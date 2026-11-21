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

A folder holds a copy of the album only if it holds two or more of its Tracks and the album
is most of the folder. A lone Track, or a few in a mix or a folder of loose Tracks, is a
stray: it is reported, never planned. Two copies are the album held twice if most Tracks of
the smaller one are in the other. Copies that share most Tracks form a group (two copies of
disc 1, apart from two copies of disc 2), and each group keeps its best copy.
Tracks match by artist and title, ignoring case, spaces and punctuation, then by title
without a bracketed subtitle if the two are the same length; never by File name. Copy-named
Files ("Song 1.mp3" beside "Song.mp3") are left to the audio Duplicate pass (audiodupes.py).

The copy kept is judged on the Tracks both hold, in order: more lossless Files, a higher
average bitrate, more complete tags (see TAGS), a cover; then more Tracks, then the first
folder. Each line of the plan is one copy to quarantine, usually one per album:

  held-twice  keep  FOLDER  quarantine  FOLDER  ALBUM  WHY  TRACK...

where each TRACK is NAME/SIZE/NAME/SIZE: the Track's File in the first folder and in the
second. To keep the other copy, swap the words keep and quarantine; delete a line to
keep both. A group of three copies gets two lines with the same copy kept: change the
copy kept on both, or plan again after the first run. apply.py moves only the TRACK Files of the folder marked quarantine, to the same
path under _Quarantine/, so a Track only that copy has stays. --library picks the app's
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
from health import LIVE, UNKNOWN, one_disc_per_folder, open_read_only  # noqa: E402

# The tags counted for completeness (title and album are always there: Tracks are matched by them).
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
class HeldTwice:
    """One copy of an album held twice to quarantine: the copy to keep, the other copy, and the Tracks both hold."""

    album: str  # "Album artist - Album", for people
    keep: str  # folder
    other: str  # folder
    reason: str
    pairs: list[tuple[Track, Track]]  # (Track in keep, Track in other), in track order
    only_other: int = 0  # Tracks only the other copy has: they stay
    copy_named: int = 0  # copy-named Files in either copy, left to the audio Duplicate pass


class Stray(NamedTuple):
    album: str
    folder: str
    tracks: int  # Tracks of the album in the folder
    folder_tracks: int  # all Tracks in the folder


class Unmatched(NamedTuple):
    """A copy of an album that shares no more than half its Tracks with any other copy: not planned."""

    album: str
    folder: str
    other: str  # the copy it shares most with
    in_common: int  # Tracks both hold
    smaller: int  # Tracks in the smaller of the two


@dataclass
class Planned:
    lines: list[HeldTwice] = field(default_factory=list)
    strays: list[Stray] = field(default_factory=list)
    unmatched: list[Unmatched] = field(default_factory=list)


LOSSLESS = {"flac", "wav", "aif", "aiff", "ape", "wv", "alac"}  # suffixes and codecs (an M4A may hold ALAC)


@dataclass
class Read:
    tracks: list[Track]
    changed: list[str]  # Files whose size differs from the app's: retagged or replaced since its last scan
    gone: int  # Tracks whose File the scan does not list in the Library (e.g. moved to Quarantine since)


def _known(value: str) -> bool:
    return value.strip().casefold() not in UNKNOWN


def read_tracks(db: sqlite3.Connection, scan: Iterable[Entry], library: int = 1) -> Read:
    """The Tracks of one library in the app's database whose Files the scan lists at the size the app
    recorded. Files the app's scanner skips (_Quarantine/, a folder with a .ndignore) are left out."""
    entries = list(scan)
    ignored = ignored_folders(entries)
    sizes = {e.path: e.size for e in entries if e.kind == "f" and not outside_library(e.path, ignored)}
    read = Read([], [], 0)
    for (path, size, album_artist, album, artist, title, duration, suffix, codec, bit_rate, disc, number,
         year, genre, mbid, embedded, images) in db.execute(
        f"""SELECT m.path, m.size, m.album_artist, m.album, m.artist, m.title, m.duration, m.suffix, m.codec,
                  m.bit_rate, m.disc_number, m.track_number, m.year, m.genre, m.mbz_recording_id,
                  m.has_cover_art, coalesce(f.image_files, '[]')
           FROM media_file m LEFT JOIN folder f ON f.id = m.folder_id
           WHERE m.{LIVE} AND m.library_id = ? ORDER BY m.path""",
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


def _artist_title(t: Track) -> tuple[str, str]:
    return (_norm(t.artist), _norm(t.title))


def _artist_title_without_subtitle(t: Track) -> tuple[str, str]:
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


def match_tracks(a: list[Track], b: list[Track]) -> list[tuple[Track, Track]]:
    """(Track in a, Track in b) for each Track both copies hold, in a's track order.

    Tracks match by artist and title, ignoring case, spaces and punctuation. What's left
    matches by title without its bracketed subtitle ("2 + 2 = 5 (The Lukewarm.)" and
    "2+2=5"), but only if the two are the same length, so a remix doesn't match the original.
    """
    pairs = _pair_by(_artist_title, a, b, close=False)
    paired = {id(t) for p in pairs for t in p}
    pairs += _pair_by(_artist_title_without_subtitle, [t for t in a if id(t) not in paired], [t for t in b if id(t) not in paired], close=True)
    return sorted(pairs, key=lambda p: _order(p[0]))


def _formats(tracks: list[Track]) -> str:
    return "/".join(sorted({t.suffix.upper() for t in tracks}))


def _choose(a: list[Track], b: list[Track], tracks_a: int, tracks_b: int) -> tuple[bool, str]:
    """Whether copy a is kept over copy b, judged on the Tracks both hold (a[i] and b[i] are
    the same Track), and why. In order: lossless, bitrate, tags, cover, then more Tracks."""
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
    if tracks_a != tracks_b:
        return tracks_a > tracks_b, f"same quality; more Tracks ({max(tracks_a, tracks_b)} vs {min(tracks_a, tracks_b)})"
    return True, "same quality; kept the first folder"


def plan_albums(tracks: Iterable[Track]) -> Planned:
    """Albums held twice: each copy of an album that shares most Tracks with the copy kept, and the strays.

    Copies that share most Tracks form a group (two copies of one disc, say, apart from two
    copies of another), and each group keeps one copy: the best of the group.
    """
    tracks = list(tracks)
    folder_tracks = Counter(os.path.dirname(t.path) for t in tracks)
    names: dict[str, set[str]] = defaultdict(set)
    for t in tracks:
        names[os.path.dirname(t.path)].add(os.path.basename(t.path))
    albums: dict[tuple[str, str], dict[str, list[Track]]] = defaultdict(lambda: defaultdict(list))
    for t in tracks:
        if t.album.strip().casefold() in UNKNOWN:
            continue
        albums[(t.album_artist.strip().casefold(), t.album.strip().casefold())][os.path.dirname(t.path)].append(t)
    planned = Planned()
    for _key, folders in sorted(albums.items()):
        if len(folders) < 2 or one_disc_per_folder({t.disc for t in ts} for ts in folders.values()):
            continue
        first = min((t for ts in folders.values() for t in ts), key=lambda t: t.path)
        label = f"{first.album_artist.strip()} - {first.album.strip()}"
        copies: dict[str, list[Track]] = {}
        copy_named: dict[str, int] = {}
        for folder, ts in sorted(folders.items()):
            if len(ts) < 2 or 2 * len(ts) <= folder_tracks[folder]:
                planned.strays.append(Stray(label, folder, len(ts), folder_tracks[folder]))
                continue
            copies[folder] = [t for t in ts if not _is_copy_name(t.path, names[folder])]
            copy_named[folder] = len(ts) - len(copies[folder])
        if len(copies) < 2:
            continue
        _plan_copies(label, copies, copy_named, planned)
    return planned


def _plan_copies(label: str, copies: dict[str, list[Track]], copy_named: dict[str, int], planned: Planned) -> None:
    """Plan (or report) the copies of one album, held in more than one folder."""
    shared: dict[tuple[str, str], list[tuple[Track, Track]]] = {}
    for fa in copies:
        for fb in copies:
            if fa != fb:
                shared[(fa, fb)] = match_tracks(copies[fa], copies[fb])

    def most(fa: str, fb: str) -> bool:
        return 2 * len(shared[(fa, fb)]) > min(len(copies[fa]), len(copies[fb]))

    def choose(fa: str, fb: str) -> tuple[bool, str]:
        pairs = shared[(fa, fb)]
        return _choose([p[0] for p in pairs], [p[1] for p in pairs], len(copies[fa]), len(copies[fb]))

    # Biggest first, so the group's first copy is its biggest, and so is the copy a loner is reported against.
    order = sorted(copies, key=lambda f: (-len(copies[f]), f))
    groups: list[list[str]] = []
    for folder in order:
        joined = [g for g in groups if any(most(folder, other) for other in g)]
        for g in joined[1:]:
            joined[0] += g
            groups.remove(g)
        if joined:
            joined[0].append(folder)
        else:
            groups.append([folder])
    lines = []
    for group in groups:
        if len(group) == 1:
            folder = group[0]
            if folder == order[0]:
                continue
            other = max((f for f in order if f != folder), key=lambda f: (len(shared[(folder, f)]), len(copies[f])))
            smaller = min(len(copies[folder]), len(copies[other]))
            planned.unmatched.append(Unmatched(label, folder, other, len(shared[(folder, other)]), smaller))
            continue
        keep = group[0]
        for folder in group[1:]:
            if most(keep, folder) and not choose(keep, folder)[0]:
                keep = folder
        for folder in group:
            if folder == keep:
                continue
            if not most(keep, folder):
                planned.unmatched.append(Unmatched(label, folder, keep, len(shared[(keep, folder)]),
                                                   min(len(copies[keep]), len(copies[folder]))))
                continue
            pairs = shared[(keep, folder)]
            lines.append(HeldTwice(label, keep, folder, choose(keep, folder)[1], pairs, len(copies[folder]) - len(pairs),
                                   copy_named[keep] + copy_named[folder]))
    planned.lines += sorted(lines, key=lambda line: line.other)


OP = "held-twice"
KEEP, AWAY = "keep", "quarantine"


def _clean(text: str) -> str:
    return " ".join(text.split())  # no tabs or newlines in a plan field


def plan_line(line: HeldTwice) -> str:
    """The plan line for apply.py: op, "keep", the folder kept, "quarantine", the other folder,
    album, reason, then one field per Track both hold: its name and size in the first folder,
    then in the second, joined by "/" (which no File name contains)."""
    tracks = [f"{os.path.basename(k.path)}/{k.size}/{os.path.basename(o.path)}/{o.size}" for k, o in line.pairs]
    return "\t".join([OP, KEEP, line.keep, AWAY, line.other, _clean(line.album), _clean(line.reason), *tracks])


class Move(NamedTuple):
    src: str  # the File to quarantine
    size: int  # its size when planned
    keep: str  # the same Track in the copy kept
    keep_size: int


def parse_line(fields: list[str]) -> tuple[str, str, list[Move]]:
    """(folder kept, folder quarantined, moves) from a plan line, whichever folder the owner marked keep."""
    if len(fields) < 8 or fields[0] != OP:
        raise ValueError("a held-twice line needs op, two marked folders, album, reason and at least one TRACK")
    if sorted((fields[1], fields[3])) != [KEEP, AWAY]:
        raise ValueError(f"mark one folder {KEEP!r} and the other {AWAY!r}, not {fields[1]!r} and {fields[3]!r}")
    first, second = fields[2], fields[4]
    if not first or not second or first.casefold() == second.casefold():
        raise ValueError("the two copies must be two different folders")
    if any(f.casefold().split("/")[0] == QUARANTINE.casefold() for f in (first, second)):
        raise ValueError("both copies must be in the Library, not in Quarantine")
    keep_first = fields[1] == KEEP
    moves = []
    for track in fields[7:]:
        parts = track.split("/")
        if len(parts) != 4 or not all(parts) or not parts[1].isdigit() or not parts[3].isdigit():
            raise ValueError(f"a TRACK must be NAME/SIZE/NAME/SIZE, not {track!r}")
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
    return f"{fields[5]}: {len(moves)} Tracks from {away}\t-> {QUARANTINE}/, keeping {kept}"


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
    for u in planned.unmatched:
        print(f"not planned, few Tracks in common ({u.in_common} of {u.smaller}): {u.album}\t{u.folder} | {u.other}", file=err)
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
