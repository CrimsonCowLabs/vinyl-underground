#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Apply a casemerge.py plan to a writable mount of the music share.

Usage: apply.py ROOT PLAN.tsv [--dry-run]

Every step is checked afterwards by re-listing both folders, because a
case-insensitive server may apply a name to the wrong sibling. The first step
that fails or doesn't verify stops the run. Completed steps are recorded in
PLAN.tsv.done, so re-running the same plan resumes after the last one.
"decide" steps are only reported. A "quarantine" step first checks that the File
is byte-identical to the one it duplicates, and stops if not. _Quarantine/ gets
a .ndignore file so the app's scanner skips it.
"""
import filecmp
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from casemerge import QUARANTINE  # noqa: E402


def listing(root: str, rel: str) -> set[str]:
    try:
        return set(os.listdir(os.path.join(root, rel)))
    except FileNotFoundError:
        return set()


def present(root: str, rel: str) -> bool:
    parent, name = os.path.split(rel)
    return name in listing(root, parent)


def present_any_case(root: str, rel: str) -> bool:
    parent, name = os.path.split(rel)
    return name.casefold() in {n.casefold() for n in listing(root, parent)}


def relocate(root: str, src: str, dst: str, twin_ok: bool = False) -> str:
    # Compare case-insensitively: on these servers, "Atb" would land on an existing "ATB".
    if present_any_case(root, dst):
        raise RuntimeError(f"destination already exists (in some letter case): {dst}")
    parent, name = os.path.split(src)
    twins = {n for n in listing(root, parent) if n.casefold() == name.casefold()}
    os.makedirs(os.path.join(root, os.path.dirname(dst)), exist_ok=True)
    os.rename(os.path.join(root, src), os.path.join(root, dst))
    if not present(root, dst):
        raise RuntimeError(f"rename did not verify, destination missing: {dst}")
    gone = twins - listing(root, parent)
    if gone == {name}:
        return "ok"
    if twin_ok and len(gone) == 1:
        # The server applied the name to a case-twin instead. Harmless for a
        # Phase A rename: the names are unique now either way.
        return f"ok (server renamed its twin {gone.pop()!r} instead)"
    raise RuntimeError(f"rename did not verify: {src} -> {dst}, gone: {sorted(gone)}")


def ensure_quarantine(root: str) -> None:
    os.makedirs(os.path.join(root, QUARANTINE), exist_ok=True)
    marker = os.path.join(root, QUARANTINE, ".ndignore")
    if not os.path.exists(marker):
        open(marker, "w").close()


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2
    root, plan_path, dry = argv[0], argv[1], "--dry-run" in argv[2:]
    with open(plan_path, encoding="utf-8") as fh:
        plan = [line.rstrip("\n").split("\t") for line in fh if line.strip()]
    log_path = plan_path + ".done"
    finished = set()
    if os.path.exists(log_path):
        with open(log_path, encoding="utf-8") as fh:
            finished = {int(n) for n in fh.read().split()}
    log = None if dry else open(log_path, "a", encoding="utf-8")
    done = 0
    for i, (op, src, *rest) in enumerate(plan, 1):
        dst = rest[0] if rest else ""
        if i in finished:
            continue
        print(f"[{i}/{len(plan)}] {op}\t{src}" + (f"\t-> {dst}" if dst else ""), flush=True)
        if dry or op == "decide":
            continue
        try:
            if op == "rename":
                result = relocate(root, src, dst, twin_ok=True)
            elif op == "move":
                result = relocate(root, src, dst)
            elif op == "quarantine":
                keep = rest[1] if len(rest) > 1 else ""
                if not keep or not filecmp.cmp(os.path.join(root, src), os.path.join(root, keep), shallow=False):
                    raise RuntimeError(f"not byte-identical to {keep!r}, so not a Duplicate; decide by hand")
                ensure_quarantine(root)
                result = relocate(root, src, dst)
            elif op == "rmdir":
                os.rmdir(os.path.join(root, src))  # fails unless empty
                if present(root, src):
                    raise RuntimeError(f"rmdir did not verify: {src}")
                result = "ok"
            else:
                raise RuntimeError(f"unknown op {op!r}")
        except (OSError, RuntimeError) as e:
            print(f"STOPPED at step {i}: {e}", file=sys.stderr)
            return 1
        done += 1
        assert log is not None
        log.write(f"{i}\n")
        log.flush()
        print(f"    {result}", flush=True)
    print(f"{done} steps applied" + (" (dry run)" if dry else ""), file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
