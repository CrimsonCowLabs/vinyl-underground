#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""List the paths beets should retag, from a scan of the share, for beets/retag.sh import.

Usage: retag.py SCAN.tsv [FOLDER ...] > retag.txt

Prints one path per line, relative to the share root: the fewest folders that hold every audio
File in the Library, or only those under the FOLDERs given (e.g. for a trial). beets walks
every folder it is given, so Quarantine, the NAS's recycle bin (trashbox/) and every folder
with a .ndignore are left out by giving their sibling folders instead. Audio Files loose beside
such a folder (or at the share root) are listed on stderr instead, to retag by hand: given one by
one, beets would take each for an album of one Track.
Warns on stderr about copy-named Files ("Song 1.mp3" beside "Song.mp3") still in the Library,
which beets would take for extra Tracks: run the Duplicate recipes first.

This module is pure apart from main(): it never touches the share.
"""
from __future__ import annotations

import os
import sys
from typing import Iterable

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from casemerge import QUARANTINE, Entry, read_scans  # noqa: E402
from dupes import AUDIO, _is_copy_name, ignored_folders, outside_library  # noqa: E402

# The NAS's recycle bin (Buffalo LinkStations), left out even before it has a .ndignore.
RECYCLE_BINS = ("trashbox",)


def _ancestors(path: str) -> list[str]:
    """The folders above `path`, top first ("" is the share root)."""
    parts = path.split("/")
    return ["/".join(parts[:i]) for i in range(len(parts))]


def _under(path: str, folder: str) -> bool:
    return path.casefold().startswith(folder.casefold() + "/")


def _folders(within: Iterable[str]) -> list[str]:
    return [w.strip("/") for w in within]


def _within(path: str, folders: list[str]) -> bool:
    """Whether `path` is one of `folders` or under one of them (all of them when there are none)."""
    return not folders or any(path.casefold() == w.casefold() or _under(path, w) for w in folders)


def _left_out(scan: list[Entry]) -> set[str]:
    """Folders outside the Library besides Quarantine: the recycle bin and those with a .ndignore."""
    return ignored_folders(scan) | set(RECYCLE_BINS)


def audio_files(scan: Iterable[Entry], within: Iterable[str] = ()) -> list[str]:
    """The audio Files in the Library, or only those under the folders `within`."""
    entries = list(scan)
    folders = _folders(within)
    left_out = _left_out(entries)
    if "" in left_out:  # a .ndignore at the share root: the app scans nothing
        return []
    audio = [e.path for e in entries if e.kind == "f" and AUDIO.search(e.path)]
    return [p for p in audio if not outside_library(p, left_out) and _within(p, folders)]


def _cover(scan: Iterable[Entry], within: Iterable[str]) -> tuple[list[str], list[str]]:
    """The folders to give beets whole, and the audio Files in no such folder."""
    entries = list(scan)
    folders = _folders(within)
    # Folders with something left out somewhere under them (the share root always has Quarantine).
    holds_left_out = {a.casefold() for f in _left_out(entries) | {QUARANTINE} for a in _ancestors(f)}

    def whole(folder: str) -> bool:
        return _within(folder, folders) and folder.casefold() not in holds_left_out

    paths: set[str] = set()
    loose: list[str] = []
    for p in audio_files(entries, folders):
        # The topmost folder above p that can be given whole.
        top = next((a for a in _ancestors(p)[1:] if whole(a)), None)
        if top is None:
            loose.append(p)
        else:
            paths.add(top)
    return sorted(paths), sorted(loose)


def import_paths(scan: Iterable[Entry], within: Iterable[str] = ()) -> list[str]:
    """The fewest folders that hold every audio File in the Library and nothing outside it.

    beets walks every folder it is given, so a folder is given whole only if nothing under it is
    left out; otherwise its subfolders are given instead. `within` limits this to the audio Files
    under those folders (relative to the share root), e.g. for a trial.
    """
    return _cover(scan, within)[0]


def loose_files(scan: Iterable[Entry], within: Iterable[str] = ()) -> list[str]:
    """The audio Files import_paths() can't give beets: those beside a left-out folder (or at the
    share root). Given one by one, beets would take each for an album of one Track."""
    return _cover(scan, within)[1]


def copy_named(audio: Iterable[str]) -> list[str]:
    """The copy-named Files ("Song 1.mp3" beside "Song.mp3") among `audio`."""
    files = list(audio)
    names: dict[str, set[str]] = {}
    for p in files:
        names.setdefault(os.path.dirname(p), set()).add(os.path.basename(p))
    return [p for p in files if _is_copy_name(p, names[os.path.dirname(p)])]


def main(argv: list[str]) -> int:
    if not argv or argv[0].startswith("-"):
        print(__doc__, file=sys.stderr)
        return 2
    scan = read_scans(argv[:1])
    folders = _folders(argv[1:])
    known = {e.path.casefold() for e in scan if e.kind == "d"}
    missing = [w for w in folders if w.casefold() not in known]
    for w in missing:
        print(f"not a folder in the scan: {w}", file=sys.stderr)
    if missing:
        return 1
    paths, loose = _cover(scan, folders)
    for p in paths:
        print(p)
    audio = audio_files(scan, folders)
    print(f"{len(paths)} paths to retag, holding {len(audio) - len(loose)} audio Files", file=sys.stderr)
    if loose:
        print(f"{len(loose)} audio Files beside a left-out folder, not retagged: retag them by hand", file=sys.stderr)
        for p in loose:
            print(f"  {p}", file=sys.stderr)
    copies = copy_named(audio)
    if copies:
        print(f"warning: {len(copies)} copy-named Files are still in the Library; run the Duplicate recipes first, "
              "or beets will see them as extra Tracks:", file=sys.stderr)
        for p in copies:
            print(f"  {p}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
