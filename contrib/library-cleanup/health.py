#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Report how healthy the Library is, from the app's database.

Usage: health.py NAVIDROME.db                  # every number, with the key that lists it
       health.py NAVIDROME.db --list KEY       # the offenders behind one number, as TSV

The database is opened read-only, so this is safe on the live database while the app runs
as well as on a copy. Only Tracks and albums the app still has count (missing = 0):

  - Tracks and albums in total.
  - Likely Duplicates: Tracks sharing artist, title and album (ignoring case and edge
    spaces), and how many of them are copy-named Files ("Song 1", "Song (1)", "Copy of
    Song" beside "Song" in the same group).
  - Albums held twice: the same album and album artist in more than one folder, unless
    each folder holds a different disc (CD1/, CD2/).
  - Suspect discs: an album with a disc of one or two Tracks while the rest have no disc number.
  - Albums with no cover: no embedded art and no image in any of their folders.
  - Tracks with no year, a blank or unknown album or artist, no track number, or no
    MusicBrainz recording ID.

This module is pure apart from main(): it only reads the database it's given.
"""
from __future__ import annotations

import os
import sqlite3
import sys
from collections import defaultdict
from pathlib import Path
from typing import NamedTuple

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from dupes import _is_copy_name  # noqa: E402

LIVE = "missing = 0"
# A tag the app shows as "[Unknown Album]" or "[Unknown Artist]" was blank in the File.
_UNKNOWN = ("", "unknown", "[unknown album]", "[unknown artist]")


class Measure(NamedTuple):
    key: str
    label: str
    count: int
    header: tuple[str, ...] = ()
    rows: tuple[tuple[object, ...], ...] = ()
    note: str = ""


def _scalar(db: sqlite3.Connection, sql: str) -> int:
    return db.execute(sql).fetchone()[0]


def _unknown(column: str) -> str:
    values = ", ".join(f"'{v}'" for v in _UNKNOWN)
    return f"lower(trim({column})) IN ({values})"


def _tracks_where(db: sqlite3.Connection, key: str, label: str, condition: str) -> Measure:
    rows = db.execute(f"SELECT path FROM media_file WHERE {LIVE} AND {condition} ORDER BY path").fetchall()
    return Measure(key, label, len(rows), ("path",), tuple(rows))


_FOLDER_PATH = "CASE WHEN f.path IN ('', '.') THEN f.name ELSE f.path || '/' || f.name END"


def _no_cover(db: sqlite3.Connection) -> Measure:
    rows = db.execute(
        f"""SELECT a.album_artist, a.name,
                   (SELECT group_concat({_FOLDER_PATH}, ' | ') FROM json_each(a.folder_ids) j JOIN folder f ON f.id = j.value)
            FROM album a
            WHERE a.{LIVE} AND trim(a.embed_art_path) = ''
              AND NOT EXISTS (SELECT 1 FROM json_each(a.folder_ids) j JOIN folder f ON f.id = j.value
                              WHERE f.image_files NOT IN ('', '[]'))
            ORDER BY a.album_artist, a.name"""
    ).fetchall()
    return Measure("no-cover", "albums with no cover", len(rows), ("album artist", "album", "folders"), tuple(rows))


def _likely_duplicates(db: sqlite3.Connection) -> list[Measure]:
    groups: dict[tuple[str, ...], list[tuple[str, str, str, str]]] = defaultdict(list)
    for artist, album, title, path in db.execute(f"SELECT artist, album, title, path FROM media_file WHERE {LIVE}"):
        groups[tuple(v.strip().casefold() for v in (artist, album, title))].append((artist, album, title, path))
    shared = [sorted(tracks, key=lambda t: t[-1]) for _key, tracks in sorted(groups.items()) if len(tracks) > 1]
    rows = tuple(t for tracks in shared for t in tracks)
    copies = []
    for tracks in shared:
        names = {os.path.basename(t[-1]) for t in tracks}
        copies += [(t[-1],) for t in tracks if _is_copy_name(t[-1], names)]
    return [
        Measure("duplicates", "likely Duplicates: extra Tracks sharing artist, title and album", len(rows) - len(shared),
                ("artist", "album", "title", "path"), rows, f"in {len(shared)} groups"),
        Measure("copy-named", "of them copy-named Files (Song 1, Song (1), Copy of Song)", len(copies),
                ("path",), tuple(sorted(copies))),
    ]


def _albums_held_twice(db: sqlite3.Connection) -> Measure:
    albums: dict[tuple[str, str], dict[str, set[int]]] = defaultdict(lambda: defaultdict(set))
    shown: dict[tuple[str, str], tuple[str, str, str]] = {}
    for album_artist, album, disc, folder in db.execute(
        f"""SELECT m.album_artist, m.album, m.disc_number, {_FOLDER_PATH}
            FROM media_file m JOIN folder f ON f.id = m.folder_id
            WHERE m.{LIVE} AND NOT {_unknown("m.album")}"""
    ):
        key = (album_artist.strip().casefold(), album.strip().casefold())
        albums[key][folder].add(disc)
        shown[key] = min(shown.get(key, (folder, album_artist, album)), (folder, album_artist.strip(), album.strip()))
    rows = []
    for key, folders in albums.items():
        if len(folders) < 2:
            continue
        discs = [next(iter(d)) for d in folders.values() if len(d) == 1]
        one_disc_per_folder = len(discs) == len(folders) and 0 not in discs and len(set(discs)) == len(discs)
        if not one_disc_per_folder:
            _folder, album_artist, album = shown[key]
            rows.append((album_artist, album, " | ".join(sorted(folders))))
    rows.sort(key=lambda r: (r[0].casefold(), r[1].casefold()))
    return Measure("albums-twice", "albums held twice: the same album and album artist in more than one folder",
                   len(rows), ("album artist", "album", "folders"), tuple(rows))


SHORT_DISC = 2  # Tracks


def _suspect_discs(db: sqlite3.Connection) -> Measure:
    albums: dict[tuple[str, str, str], dict[int, list[str]]] = defaultdict(lambda: defaultdict(list))
    for album_id, album_artist, album, disc, path in db.execute(
        f"""SELECT a.id, a.album_artist, a.name, m.disc_number, m.path
            FROM media_file m JOIN album a ON a.id = m.album_id
            WHERE m.{LIVE} AND a.{LIVE}"""
    ):
        albums[(album_artist, album, album_id)][disc].append(path)
    rows = []
    for (album_artist, album, _id), discs in sorted(albums.items()):
        short = sorted(d for d, paths in discs.items() if d and len(paths) <= SHORT_DISC)
        if 0 in discs and short:
            summary = ", ".join(f"disc {d}: {len(p)}" if d else f"no disc: {len(p)}" for d, p in sorted(discs.items()))
            strays = " | ".join(sorted(p for d in short for p in discs[d]))
            rows.append((album_artist, album, summary, strays))
    return Measure("suspect-discs", f"suspect discs: a disc of {SHORT_DISC} Tracks or fewer beside Tracks with no disc number",
                   len(rows), ("album artist", "album", "Tracks per disc", "Tracks on the short discs"), tuple(rows))


def open_read_only(path: str) -> sqlite3.Connection:
    """The app's database, opened so that nothing can write to it (and nothing is created)."""
    return sqlite3.connect(f"{Path(path).resolve().as_uri()}?mode=ro", uri=True)


def report(db: sqlite3.Connection) -> list[Measure]:
    return [
        Measure("tracks", "Tracks", _scalar(db, f"SELECT count(*) FROM media_file WHERE {LIVE}")),
        Measure("albums", "albums", _scalar(db, f"SELECT count(*) FROM album WHERE {LIVE}")),
        *_likely_duplicates(db),
        _albums_held_twice(db),
        _suspect_discs(db),
        _no_cover(db),
        _tracks_where(db, "no-year", "Tracks with no year", "year = 0"),
        _tracks_where(db, "no-album", "Tracks with a blank or unknown album", _unknown("album")),
        _tracks_where(db, "no-artist", "Tracks with a blank or unknown artist", _unknown("artist")),
        _tracks_where(db, "no-track-number", "Tracks with no track number", "track_number = 0"),
        _tracks_where(db, "no-musicbrainz-id", "Tracks with no MusicBrainz ID", "trim(mbz_recording_id) = ''"),
    ]


def main(argv: list[str]) -> int:
    args = list(argv)
    key = None
    if "--list" in args:
        i = args.index("--list")
        key = args[i + 1] if i + 1 < len(args) else ""
        del args[i : i + 2]
    if len(args) != 1:
        print(__doc__, file=sys.stderr)
        return 2
    db = open_read_only(args[0])
    try:
        measures = report(db)
    finally:
        db.close()
    if key is None:
        for m in measures:
            label = f"{m.label} ({m.note})" if m.note else m.label
            print(f"{m.count}\t{label}\t{m.key}")
        return 0
    chosen = next((m for m in measures if m.key == key and m.header), None)
    if chosen is None:
        listable = ", ".join(m.key for m in measures if m.header)
        print(f"nothing to list for {key!r}; choose one of: {listable}", file=sys.stderr)
        return 2
    print("\t".join(chosen.header))
    for row in chosen.rows:
        print("\t".join(str(v) for v in row))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
