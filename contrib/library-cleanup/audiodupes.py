#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Plan moving Duplicates whose audio is identical, though their tags differ, into Quarantine.

Usage: audiodupes.py SCAN.tsv --root ROOT > audiodupes.tsv

A music app that copies a File next to itself ("Song 1.mp3" beside "Song.mp3") often
rewrites the tags of the File it writes, so `jdupes` (identical bytes) misses it. This pass compares
only the audio (see audio.py): MP3 frames without their ID3v1/ID3v2/APEv2/Lyrics3 tags,
an MP4/M4A File's media data without its metadata, and an AIFF/WAV File's format and
sample chunks without its tag chunks. Other formats are reported, never planned.

Only candidates are compared, so the share is read as little as possible: each File with
the copy-named Files beside it in the same folder (same extension), from SCAN.tsv
(scan.py). Their tag headers are read first; only Files whose audio is the same size
are read in full. Every other File, and every cross-folder Duplicate, is left to the
`jdupes` pass (dupes.py).

Each set of Files with identical audio becomes "quarantine-audio" steps for apply.py,
which checks again that the audio is identical before each move. The File kept is chosen
as in dupes.py: not the copy's name, then the deepest folder, then the shortest name.
ROOT is the share (the read-only mount will do); its _Quarantine is listed so the plan
doesn't pick a path that's already taken there.

Everything but main() is pure: the reading of Files is passed in.
"""
from __future__ import annotations

import os
import sys
from collections import defaultdict
from dataclasses import dataclass, field
from typing import Callable, Hashable, Iterable, Optional

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from audio import audio_digest, audio_size  # noqa: E402
from casemerge import Action, Entry, read_scans  # noqa: E402
from dupes import AUDIO, outside_library, quarantined, copied_from, folder_impact, ignored_folders, plan_duplicates  # noqa: E402

OP = "quarantine-audio"


def candidate_sets(scan: Iterable[Entry]) -> list[list[str]]:
    """Sets of audio Files that may be Duplicates: each File with the copy-named Files beside it."""
    entries = list(scan)
    ignored = ignored_folders(entries)
    files = {e.path for e in entries if e.kind == "f" and AUDIO.search(e.path) and not outside_library(e.path, ignored)}
    parent: dict[str, str] = {}

    def root(p: str) -> str:
        while parent.get(p, p) != p:
            p = parent[p]
        return p

    for path in files:
        folder, name = os.path.split(path)
        for base in copied_from(name):
            original = os.path.join(folder, base)
            if original in files:
                parent[root(path)] = root(original)
    sets: dict[str, list[str]] = defaultdict(list)
    for path in parent:
        sets[root(path)].append(path)
    for top, members in sets.items():
        members.append(top)
    return sorted(sorted(set(members)) for members in sets.values())


@dataclass
class Found:
    sets: list[list[str]] = field(default_factory=list)  # Files whose audio is identical, each set sorted
    different: list[str] = field(default_factory=list)  # candidates whose audio matches none of their set
    unsupported: list[str] = field(default_factory=list)  # candidates in a format whose audio can't be found
    errors: list[tuple[str, str]] = field(default_factory=list)  # (path, error) for candidates that couldn't be read


def _group(paths: Iterable[str], key: Callable[[str], Optional[Hashable]], found: Found) -> list[list[str]]:
    """Groups of 2 or more `paths` with the same key. Files without one are recorded in `found`."""
    groups: dict[Hashable, list[str]] = defaultdict(list)
    for path in paths:
        try:
            k = key(path)
        except OSError as e:
            found.errors.append((path, str(e)))
            continue
        if k is None:
            found.unsupported.append(path)
        else:
            groups[k].append(path)
    return [g for g in groups.values() if len(g) > 1]


def find_same_audio(
    candidates: Iterable[list[str]],
    size_of: Callable[[str], Optional[int]],
    digest_of: Callable[[str], Optional[str]],
) -> Found:
    """Split each candidate set into the Files whose audio is identical.

    `size_of` gives a File's audio size from its tag headers alone; only Files whose audio
    is the same size have their audio read in full, by `digest_of`.
    """
    found = Found()
    for files in candidates:
        unsupported, errors = len(found.unsupported), len(found.errors)
        same = [g for sized in _group(files, size_of, found) for g in _group(sized, digest_of, found)]
        found.sets.extend(sorted(g) for g in same)
        settled = {p for g in same for p in g} | set(found.unsupported[unsupported:]) | {p for p, _ in found.errors[errors:]}
        found.different.extend(p for p in files if p not in settled)
    found.different.sort()
    return found


def plan_audio_duplicates(sets: Iterable[list[str]], taken: Iterable[str] = ()) -> list[Action]:
    """quarantine-audio steps for all but one File of each set of Files with identical audio."""
    return plan_duplicates(sets, taken, op=OP)


def main(argv: list[str]) -> int:
    args = list(argv)
    if len(args) != 3 or "--root" not in args[:2]:
        print(__doc__, file=sys.stderr)
        return 2
    i = args.index("--root")
    root = args[i + 1]
    del args[i : i + 2]
    scan = read_scans(args)
    candidates = candidate_sets(scan)
    print(f"{len(candidates)} candidate sets, {sum(map(len, candidates))} Files; comparing their audio...", file=sys.stderr)

    hashed = 0

    def digest(rel: str) -> Optional[str]:
        nonlocal hashed
        hashed += 1
        if hashed % 200 == 0:
            print(f"  {hashed} Files read in full", file=sys.stderr, flush=True)
        return audio_digest(os.path.join(root, rel))

    found = find_same_audio(candidates, lambda rel: audio_size(os.path.join(root, rel)), digest)
    plan = plan_audio_duplicates(found.sets, quarantined(root))
    for a in plan:
        print("\t".join(a))
    print(f"{len(found.sets)} sets with identical audio; {len(plan)} Duplicates to quarantine", file=sys.stderr)
    print(f"{len(found.different)} candidate Files whose audio differs from the rest of their set, left alone", file=sys.stderr)
    for path in found.unsupported:
        print(f"not compared (format not supported): {path}", file=sys.stderr)
    for path, error in found.errors:
        print(f"not compared (unreadable): {path}\t{error}", file=sys.stderr)
    print("folder\tmoving out\taudio Files now", file=sys.stderr)
    for top, out, now in folder_impact(scan, plan):
        if out:
            note = "  <- emptied" if out == now else ""
            print(f"{top}\t{out}\t{now}{note}", file=sys.stderr)
    return 1 if found.errors else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
