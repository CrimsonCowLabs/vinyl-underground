#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""List every folder and File under a music share, serially, as TSV: kind, size, relative path.

Usage: scan.py /mnt/music > scan.tsv

Skips the top-level _Quarantine folder. Serial on purpose: parallel walkers (e.g. bfs) return mangled names on
case-insensitive SMB1 shares. Errors go to stderr; the exit code is 1 if any occurred.
"""
import os
import sys


def main() -> int:
    root = sys.argv[1]
    errors = 0

    def onerror(e: OSError) -> None:
        nonlocal errors
        errors += 1
        print(f"{e.filename}\t{e.strerror}", file=sys.stderr)

    out = sys.stdout
    for here, dirs, files in os.walk(root, onerror=onerror):
        rel = os.path.relpath(here, root)
        if rel == ".":
            dirs[:] = [d for d in dirs if d != "_Quarantine"]
        for name in dirs:
            out.write(f"d\t0\t{os.path.normpath(os.path.join(rel, name))}\n")
        for name in files:
            try:
                size = os.lstat(os.path.join(here, name)).st_size
            except OSError as e:
                onerror(e)
                continue
            out.write(f"f\t{size}\t{os.path.normpath(os.path.join(rel, name))}\n")
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
