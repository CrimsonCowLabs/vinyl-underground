#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Plan moving the weaker copy of each album held twice into Quarantine.

Usage: heldtwice.py NAVIDROME.db SCAN.tsv [--library ID] > heldtwice.tsv

An album is held twice when the same album and album artist have Tracks in more than one
folder (as health.py's albums-twice counts it, so one disc per folder doesn't count). The
album, artist, title, format, bitrate, tags and cover come from the app's database, opened
read-only; SCAN.tsv (scan.py) says which Files are on the share now. A Track counts only if
the scan lists its File at the size the app recorded, so a File moved, retagged or replaced
since the app's last scan is left out (and listed).

A folder holds a copy of the album only if the album is most of its Tracks. One Track of
the album in a mix or a folder of loose songs is a stray: it is reported, never planned.
Two copies are the album held twice if most songs of the smaller one are in the other.
Songs match by artist and title, ignoring case, spaces and punctuation, then by title
without a bracketed subtitle if the two are the same length; never by File name. Copy-named
Files ("Song 1.mp3" beside "Song.mp3") are left to the audio Duplicate pass (audiodupes.py).

The copy kept is judged on the songs both hold, in order: more lossless Files, a higher
average bitrate, more complete tags (see TAGS), a cover; then more songs, then the first
folder. Each line of the plan is one copy to quarantine, usually one per album:

  held-twice  keep  FOLDER  quarantine  FOLDER  ALBUM  WHY  SONG...

where each SONG is NAME/SIZE/NAME/SIZE: the song's File in the first folder and in the
second. To keep the other copy, swap the words keep and quarantine; delete a line to
keep both. apply.py moves only the SONG Files of the folder marked quarantine, to the same
path under _Quarantine/, so a song only that copy has stays. --library picks the app's
library holding the share (1, the first, by default; the Inbox is another).

Apart from main(), this module only reads the database connection it's given.
"""
from __future__ import annotations

import os
import re
import sqlite3
import sys
from collections import Counter, defaultdict
from dataclasses import dataclass, field
from typing import Callable, Iterable, NamedTuple

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from casemerge import QUARANTINE, Entry, read_scans  # noqa: E402
from dupes import _is_copy_name, ignored_folders, outside_library  # noqa: E402
from health import open_read_only  # noqa: E402

# A tag the app shows as "[Unknown Album]" or "[Unknown Artist]" was blank in the File.
_UNKNOWN = ("", "unknown", "[unknown album]", "[unknown artist]")


# The tags counted for completeness (title and album are always there: songs are matched by them).
TAGS = ("artist", "album artist", "year", "track number", "genre", "MusicBrainz recording ID")


class Track(NamedTuple):
    path: str  # relative to the share root
    size: int
    album_artist: str
    album: str
    artist: str
    title: str
    duration: float  # seconds
    suffix: str
    bit_rate: int  # kbps
    tags: int  # how many of TAGS are filled in
    cover: bool  # embedded art, or an image in its folder
    lossless: bool = False
    disc: int = 0
    number: int = 0  # track number


@dataclass
class Line:
    """One album held twice: the copy to keep, the other copy, and the songs both hold."""

    album: str  # "Album artist - Album", for people
    keep: str  # folder
    other: str  # folder
    reason: str
    pairs: list[tuple[Track, Track]]  # (Track in keep, Track in other), in track order
    only_other: int = 0  # songs only the other copy has: they stay
    copy_named: int = 0  # copy-named Files in either copy, left to the audio Duplicate pass


class Stray(NamedTuple):
    album: str
    folder: str
    tracks: int  # Tracks of the album in the folder
    folder_tracks: int  # all Tracks in the folder


@dataclass
class Planned:
    lines: list[Line] = field(default_factory=list)
    strays: list[Stray] = field(default_factory=list)
    unmatched: list[tuple[str, str, str, int, int]] = field(default_factory=list)  # (album, folder, folder, in common, smaller copy)


LOSSLESS = {"flac", "wav", "aif", "aiff", "ape", "wv", "alac"}  # suffixes and codecs (an M4A may hold ALAC)


@dataclass
class Read:
    tracks: list[Track]
    changed: list[str]  # Files whose size differs from the app's: retagged or replaced since its last scan
    gone: int  # Tracks whose File the scan does not list in the Library (e.g. moved to Quarantine since)


def _known(value: str) -> bool:
    return value.strip().casefold() not in _UNKNOWN


def read_tracks(db: sqlite3.Connection, scan: Iterable[Entry], library: int = 1) -> Read:
    """The Tracks of one library in the app's database whose Files the scan lists at the size the app
    recorded. Files the app's scanner skips (_Quarantine/, a folder with a .ndignore) are left out."""
    entries = list(scan)
    ignored = ignored_folders(entries)
    sizes = {e.path: e.size for e in entries if e.kind == "f" and not outside_library(e.path, ignored)}
    read = Read([], [], 0)
    for (path, size, album_artist, album, artist, title, duration, suffix, codec, bit_rate, disc, number,
         year, genre, mbid, embedded, images) in db.execute(
        """SELECT m.path, m.size, m.album_artist, m.album, m.artist, m.title, m.duration, m.suffix, m.codec,
                  m.bit_rate, m.disc_number, m.track_number, m.year, m.genre, m.mbz_recording_id,
                  m.has_cover_art, coalesce(f.image_files, '[]')
           FROM media_file m LEFT JOIN folder f ON f.id = m.folder_id
           WHERE m.missing = 0 AND m.library_id = ? ORDER BY m.path""",
        (library,),
    ):
        if path not in sizes:
            read.gone += 1
            continue
        if sizes[path] != size:
            read.changed.append(path)
            continue
        tags = sum((_known(artist), _known(album_artist), year > 0, number > 0, bool(genre.strip()), bool(mbid.strip())))
        read.tracks.append(Track(path, size, album_artist, album, artist, title, duration, suffix, bit_rate, tags,
                                 bool(embedded) or images not in ("", "[]"),
                                 suffix.casefold() in LOSSLESS or codec.casefold() in LOSSLESS, disc, number))
    return read


def _norm(text: str) -> str:
    return "".join(c for c in text.casefold() if c.isalnum())


_BRACKETED = re.compile(r"\([^)]*\)|\[[^\]]*\]")
SAME_LENGTH = 3.0  # seconds: two rips of one recording differ by less


def _song(t: Track) -> tuple[str, str]:
    return (_norm(t.artist), _norm(t.title))


def _song_without_subtitle(t: Track) -> tuple[str, str]:
    return (_norm(t.artist), _norm(_BRACKETED.sub("", t.title)))


def _order(t: Track) -> tuple[int, int, str]:
    return (t.disc, t.number, t.path)


def _pair_by(key: Callable[[Track], tuple[str, str]], a: list[Track], b: list[Track], close: bool) -> list[tuple[Track, Track]]:
    """Pairs of Tracks with the same key. A key held a different number of times in a and b is ambiguous, so unpaired."""
    by_a: dict[tuple[str, str], list[Track]] = defaultdict(list)
    by_b: dict[tuple[str, str], list[Track]] = defaultdict(list)
    for t in a:
        by_a[key(t)].append(t)
    for t in b:
        by_b[key(t)].append(t)
    pairs: list[tuple[Track, Track]] = []
    for k, ta in by_a.items():
        tb = by_b.get(k, [])
        if not k[1] or len(ta) != len(tb):
            continue
        matched = list(zip(sorted(ta, key=_order), sorted(tb, key=_order)))
        if not close or all(abs(x.duration - y.duration) <= SAME_LENGTH for x, y in matched):
            pairs += matched
    return pairs


def match_songs(a: list[Track], b: list[Track]) -> list[tuple[Track, Track]]:
    """(Track in a, Track in b) for each song both copies hold, in a's track order.

    Songs match by artist and title, ignoring case, spaces and punctuation. What's left
    matches by title without its bracketed subtitle ("2 + 2 = 5 (The Lukewarm.)" and
    "2+2=5"), but only if the two are the same length, so a remix doesn't match the original.
    """
    pairs = _pair_by(_song, a, b, close=False)
    paired = {id(t) for p in pairs for t in p}
    pairs += _pair_by(_song_without_subtitle, [t for t in a if id(t) not in paired], [t for t in b if id(t) not in paired], close=True)
    return sorted(pairs, key=lambda p: _order(p[0]))


def _formats(tracks: list[Track]) -> str:
    return "/".join(sorted({t.suffix.upper() for t in tracks}))


def _choose(a: list[Track], b: list[Track], songs_a: int, songs_b: int) -> tuple[bool, str]:
    """Whether copy a is kept over copy b, judged on the songs both hold (a[i] and b[i] are
    the same song), and why. In order: lossless, bitrate, tags, cover, then more songs."""
    lossless_a, lossless_b = (sum(t.lossless for t in c) for c in (a, b))
    if lossless_a != lossless_b:
        win, lose = (a, b) if lossless_a > lossless_b else (b, a)
        if all(t.lossless for t in win) and not any(t.lossless for t in lose):
            return lossless_a > lossless_b, f"lossless ({_formats(win)}) over lossy ({_formats(lose)})"
        return lossless_a > lossless_b, f"more lossless Tracks ({max(lossless_a, lossless_b)} vs {min(lossless_a, lossless_b)})"
    rate_a, rate_b = (round(sum(t.bit_rate for t in c) / len(c)) for c in (a, b))
    if rate_a != rate_b:
        return rate_a > rate_b, f"{max(rate_a, rate_b)} kbps over {min(rate_a, rate_b)} kbps"
    tags_a, tags_b = (round(sum(t.tags for t in c) / len(c), 1) for c in (a, b))
    if tags_a != tags_b:
        return tags_a > tags_b, f"more complete tags ({max(tags_a, tags_b)} vs {min(tags_a, tags_b)} of {len(TAGS)} per Track)"
    cover_a, cover_b = (any(t.cover for t in c) for c in (a, b))
    if cover_a != cover_b:
        return cover_a, "has a cover, the other copy has none"
    if songs_a != songs_b:
        return songs_a > songs_b, f"same quality; more songs ({max(songs_a, songs_b)} vs {min(songs_a, songs_b)})"
    return True, "same quality; kept the first folder"


def _is_one_disc_per_folder(folders: dict[str, list[Track]]) -> bool:
    """CD1/ and CD2/ of one album, as health.py counts it: not held twice."""
    discs = [next(iter(d)) for d in ({t.disc for t in ts} for ts in folders.values()) if len(d) == 1]
    return len(discs) == len(folders) and 0 not in discs and len(set(discs)) == len(discs)


def plan_albums(tracks: Iterable[Track]) -> Planned:
    """Albums held twice: each copy of an album that shares most songs with the copy kept, and the strays."""
    tracks = list(tracks)
    folder_tracks = Counter(os.path.dirname(t.path) for t in tracks)
    names: dict[str, set[str]] = defaultdict(set)
    for t in tracks:
        names[os.path.dirname(t.path)].add(os.path.basename(t.path))
    albums: dict[tuple[str, str], dict[str, list[Track]]] = defaultdict(lambda: defaultdict(list))
    for t in tracks:
        if t.album.strip().casefold() in _UNKNOWN:
            continue
        albums[(t.album_artist.strip().casefold(), t.album.strip().casefold())][os.path.dirname(t.path)].append(t)
    planned = Planned()
    for _key, folders in sorted(albums.items()):
        if len(folders) < 2 or _is_one_disc_per_folder(folders):
            continue
        first = min((t for ts in folders.values() for t in ts), key=lambda t: t.path)
        label = f"{first.album_artist.strip()} - {first.album.strip()}"
        copies: dict[str, list[Track]] = {}
        copy_named: dict[str, int] = {}
        for folder, ts in sorted(folders.items()):
            if 2 * len(ts) <= folder_tracks[folder]:
                planned.strays.append(Stray(label, folder, len(ts), folder_tracks[folder]))
                continue
            copies[folder] = [t for t in ts if not _is_copy_name(t.path, names[folder])]
            copy_named[folder] = len(ts) - len(copies[folder])
        if len(copies) < 2:
            continue

        def shared(fa: str, fb: str) -> tuple[list[tuple[Track, Track]], bool]:
            pairs = match_songs(copies[fa], copies[fb])
            return pairs, 2 * len(pairs) > min(len(copies[fa]), len(copies[fb]))

        def wins(fa: str, fb: str, pairs: list[tuple[Track, Track]]) -> tuple[bool, str]:
            return _choose([p[0] for p in pairs], [p[1] for p in pairs], len(copies[fa]), len(copies[fb]))

        # Start from the biggest copy, so copies that share little are reported against it.
        keep = min(copies, key=lambda f: (-len(copies[f]), f))
        for folder in copies:
            if folder == keep:
                continue
            pairs, most = shared(keep, folder)
            if most and not wins(keep, folder, pairs)[0]:
                keep = folder
        for folder in copies:
            if folder == keep:
                continue
            pairs, most = shared(keep, folder)
            if not most:
                a, b = sorted((keep, folder))
                planned.unmatched.append((label, a, b, len(pairs), min(len(copies[keep]), len(copies[folder]))))
                continue
            _won, reason = wins(keep, folder, pairs)
            planned.lines.append(Line(label, keep, folder, reason, pairs, len(copies[folder]) - len(pairs),
                                      copy_named[keep] + copy_named[folder]))
    return planned


OP = "held-twice"
KEEP, AWAY = "keep", "quarantine"


def _clean(text: str) -> str:
    return " ".join(text.split())  # no tabs or newlines in a plan field


def plan_line(line: Line) -> str:
    """The plan line for apply.py: op, "keep", the folder kept, "quarantine", the other folder,
    album, reason, then one field per song both hold: its name and size in the first folder,
    then in the second, joined by "/" (which no File name contains)."""
    songs = [f"{os.path.basename(k.path)}/{k.size}/{os.path.basename(o.path)}/{o.size}" for k, o in line.pairs]
    return "\t".join([OP, KEEP, line.keep, AWAY, line.other, _clean(line.album), _clean(line.reason), *songs])


class Move(NamedTuple):
    src: str  # the File to quarantine
    size: int  # its size when planned
    keep: str  # the same song in the copy kept
    keep_size: int


def parse_line(fields: list[str]) -> tuple[str, str, list[Move]]:
    """(folder kept, folder quarantined, moves) from a plan line, whichever folder the owner marked keep."""
    if len(fields) < 8 or fields[0] != OP:
        raise ValueError("a held-twice line needs op, two marked folders, album, reason and at least one song")
    if sorted((fields[1], fields[3])) != [KEEP, AWAY]:
        raise ValueError(f"mark one folder {KEEP!r} and the other {AWAY!r}, not {fields[1]!r} and {fields[3]!r}")
    first, second = fields[2], fields[4]
    if not first or not second or first.casefold() == second.casefold():
        raise ValueError("the two copies must be two different folders")
    if any(f.casefold().split("/")[0] == QUARANTINE.casefold() for f in (first, second)):
        raise ValueError("both copies must be in the Library, not in Quarantine")
    keep_first = fields[1] == KEEP
    moves = []
    for song in fields[7:]:
        parts = song.split("/")
        if len(parts) != 4 or not all(parts) or not parts[1].isdigit() or not parts[3].isdigit():
            raise ValueError(f"a song must be NAME/SIZE/NAME/SIZE, not {song!r}")
        a, b = (f"{first}/{parts[0]}", int(parts[1])), (f"{second}/{parts[2]}", int(parts[3]))
        (src, size), (keep, keep_size) = (b, a) if keep_first else (a, b)
        moves.append(Move(src, size, keep, keep_size))
    kept, away = (first, second) if keep_first else (second, first)
    return kept, away, moves


def describe(fields: list[str]) -> str:
    """What a plan line does, for apply.py's progress output."""
    try:
        kept, away, moves = parse_line(fields)
    except ValueError as e:
        return f"(not a valid line: {e})"
    return f"{fields[5]}: {len(moves)} songs from {away}\t-> {QUARANTINE}/, keeping {kept}"


def main(argv: list[str]) -> int:
    args = list(argv)
    library = 1
    if "--library" in args:
        i = args.index("--library")
        value = args[i + 1] if i + 1 < len(args) else ""
        if not value.isdigit():
            print(__doc__, file=sys.stderr)
            return 2
        library = int(value)
        del args[i : i + 2]
    if len(args) != 2:
        print(__doc__, file=sys.stderr)
        return 2
    db = open_read_only(args[0])
    try:
        read = read_tracks(db, read_scans(args[1:]), library)
    finally:
        db.close()
    planned = plan_albums(read.tracks)
    for line in planned.lines:
        print(plan_line(line))
    err = sys.stderr
    moving = sum(len(line.pairs) for line in planned.lines)
    print(f"{len(planned.lines)} copies of albums held twice to quarantine; {moving} Files move", file=err)
    print("album\tkeep\tquarantine\tmoving\tstaying\twhy", file=err)
    for line in planned.lines:
        note = f" ({line.copy_named} copy-named Files left to the audio Duplicate pass)" if line.copy_named else ""
        print(f"{line.album}\t{line.keep}\t{line.other}\t{len(line.pairs)}\t{line.only_other}\t{line.reason}{note}", file=err)
    for album, a, b, common, smaller in planned.unmatched:
        print(f"not planned, few songs in common ({common} of {smaller}): {album}\t{a} | {b}", file=err)
    for s in planned.strays:
        print(f"stray, left to you: {s.album}\t{s.tracks} Track{'' if s.tracks == 1 else 's'} in {s.folder}, "
              f"which holds {s.folder_tracks}", file=err)
    if read.changed:
        print(f"{len(read.changed)} Files changed since the app's last scan, left out (run a Full Scan, then plan again):", file=err)
        for path in read.changed:
            print(f"  {path}", file=err)
    if read.gone:
        print(f"{read.gone} Tracks whose File the scan doesn't list in the Library, left out", file=err)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
