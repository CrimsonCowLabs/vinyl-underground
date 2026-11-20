#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Plan moving Duplicates into Quarantine from a `jdupes` report.

Usage: dupes.py JDUPES.txt [SCAN.tsv] [--root ROOT] > dupes.tsv

JDUPES.txt is the output of a read-only `jdupes -r -S` run from the share root, so its
paths are relative to it. For each set of byte-identical audio Files, one is kept and
every other one becomes a "quarantine" step for apply.py, which moves it to the same
relative path under _Quarantine/ after checking again that its bytes still match the
kept File. Sets with no audio (e.g. a DVD's matching .IFO and .BUP) are left alone.

Which File is kept, in order:
  1. Not a copy's name: "Song 1", "Song (1)", "Copy of Song" when "Song" is in the set.
  2. The deepest folder, e.g. Artist/Album/Song over a loose Song. The bytes, and so the
     tags the app shows, are identical, so this only decides where the File lives.
  3. The shortest file name, then the first in alphabetical order.

SCAN.tsv (from scan.py) adds a per-folder summary to stderr, so the owner can see
which folders the plan empties. --root lists ROOT/_Quarantine so the plan doesn't pick
a path that's already taken there.

This module is pure apart from main(): it never touches the share.
"""
from __future__ import annotations

import os
import re
import sys
from collections import Counter
from typing import Iterable

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from casemerge import QUARANTINE, Action, Entry, read_scans  # noqa: E402

AUDIO = re.compile(r"\.(mp3|m4a|aac|aiff?|wav|flac|ogg|opus|wma|alac)$", re.IGNORECASE)
_SIZE_LINE = re.compile(r"^\d+ bytes? each:$")
# What music apps and file managers add when they copy a File next to itself.
_COPY_MARKERS = (
    re.compile(r"^(?P<base>.+) \d+$"),
    re.compile(r"^(?P<base>.+) \(\d+\)$"),
    re.compile(r"^Copy of (?P<base>.+)$"),
    re.compile(r"^Copy \(\d+\) of (?P<base>.+)$"),
)


def parse_jdupes(report: str) -> list[list[str]]:
    """The sets of matching Files in a jdupes report, in report order."""
    sets: list[list[str]] = []
    current: list[str] = []
    for line in report.splitlines():
        if not line.strip():
            if current:
                sets.append(current)
            current = []
        elif not _SIZE_LINE.match(line):
            current.append(line)
    if current:
        sets.append(current)
    return sets


def _is_copy_name(path: str, names: set[str]) -> bool:
    stem, ext = os.path.splitext(os.path.basename(path))
    return any((m := marker.match(stem)) and m["base"] + ext in names for marker in _COPY_MARKERS)


def choose_keeper(paths: list[str]) -> str:
    """The File to keep out of a set of byte-identical Files."""
    names = {os.path.basename(p) for p in paths}

    def rank(p: str) -> tuple[bool, int, int, str]:
        return (_is_copy_name(p, names), -p.count("/"), len(os.path.basename(p)), p)

    return min(paths, key=rank)


def _in_quarantine(path: str) -> bool:
    return path.split("/", 1)[0].casefold() == QUARANTINE.casefold()


def _quarantine_path(src: str, taken: set[str]) -> str:
    stem, ext = os.path.splitext(f"{QUARANTINE}/{src}")
    path, n = stem + ext, 1
    while path.casefold() in taken:
        n += 1
        path = f"{stem} [{n}]{ext}"
    return path


def plan_duplicates(sets: Iterable[list[str]], taken: Iterable[str] = ()) -> list[Action]:
    """quarantine steps for every Duplicate: all but one File of each set of audio Files."""
    used = {t.casefold() for t in taken}
    plan: list[Action] = []
    for files in sets:
        audio = [f for f in files if AUDIO.search(f) and not _in_quarantine(f)]
        if len(audio) < 2:
            continue
        keep = choose_keeper(audio)
        for src in audio:
            if src == keep:
                continue
            dst = _quarantine_path(src, used)
            used.add(dst.casefold())
            plan.append(Action("quarantine", src, dst, keep))
    return plan


def _top(path: str) -> str:
    return path.split("/", 1)[0] if "/" in path else "(top level)"


def folder_impact(scan: Iterable[Entry], plan: Iterable[Action]) -> list[tuple[str, int, int]]:
    """(top-level folder, audio Files moving out, audio Files in it now), for every folder."""
    total = Counter(_top(e.path) for e in scan if e.kind == "f" and AUDIO.search(e.path))
    moving = Counter(_top(a.src) for a in plan if a.op == "quarantine")
    return sorted((top, moving[top], n) for top, n in total.items())


def _quarantined(root: str) -> set[str]:
    found = set()
    for here, _dirs, files in os.walk(os.path.join(root, QUARANTINE)):
        rel = os.path.relpath(here, root)
        found.update(f"{rel}/{name}".replace(os.sep, "/") for name in files)
    return found


def main(argv: list[str]) -> int:
    args = list(argv)
    root = None
    if "--root" in args:
        i = args.index("--root")
        root = args[i + 1]
        del args[i : i + 2]
    if not 1 <= len(args) <= 2:
        print(__doc__, file=sys.stderr)
        return 2
    with open(args[0], encoding="utf-8") as fh:
        sets = parse_jdupes(fh.read())
    plan = plan_duplicates(sets, _quarantined(root) if root else ())
    for a in plan:
        print("\t".join(a))
    audio_sets = len({a.keep for a in plan})
    print(f"{len(sets)} sets in the report, {audio_sets} of them audio; {len(plan)} Duplicates to quarantine", file=sys.stderr)
    if len(args) == 2:
        print("folder\tmoving out\taudio Files now", file=sys.stderr)
        for top, out, now in folder_impact(read_scans(args[1:]), plan):
            if out:
                note = "  <- emptied" if out == now else ""
                print(f"{top}\t{out}\t{now}{note}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
