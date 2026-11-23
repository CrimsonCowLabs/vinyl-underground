#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""Apply a casemerge.py plan to a writable mount of the music share.

Usage: apply.py ROOT PLAN.tsv [--dry-run]

Every step is checked afterwards by re-listing both folders, because a
case-insensitive server may apply a name to the wrong sibling. The first step
that fails or doesn't verify stops the run. Completed steps are recorded, whole, in
PLAN.tsv.done, so re-running the plan skips them, even after it was edited; a new plan
written to the same name doesn't inherit the old one's progress.
"decide" steps are only reported. A "quarantine" step first checks that the File
is byte-identical to the one it duplicates, and stops if not; a "quarantine-audio"
step (from audiodupes.py) instead checks that the two Files' audio is identical,
whatever their tags (see audio.py), and stops if not. A "set-aside" step
is the owner's answer to a "decide": it moves a File that is NOT byte-identical
(e.g. a lower-bitrate copy of the same recording) into Quarantine, provided the
File it gives way to still exists. A "held-twice" step (from heldtwice.py) moves
the Tracks both copies of an album hold out of the copy marked quarantine; it first
checks that every one of those Files, and the File kept for each, is still listed at
the size it had when planned, and moves nothing if not. A step stopped part-way
resumes: a File already in Quarantine at its planned size counts as moved.
_Quarantine/ gets a .ndignore file so the app's scanner skips it.
"""
import filecmp
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from audio import same_audio  # noqa: E402
from casemerge import QUARANTINE  # noqa: E402
from heldtwice import OP as HELD_TWICE, describe, parse_line  # noqa: E402


# How each quarantine step checks that SRC is a Duplicate of KEEP, and what it says if not.
DUPLICATE_CHECKS = {
    "quarantine": (lambda a, b: filecmp.cmp(a, b, shallow=False), "not byte-identical to"),
    "quarantine-audio": (same_audio, "audio not identical to"),
}


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


def _size(root: str, rel: str) -> int | None:
    """The File's size, if a listing of its folder shows it."""
    return os.path.getsize(os.path.join(root, rel)) if present(root, rel) else None


def quarantine_copy(root: str, fields: list[str]) -> str:
    """A "held-twice" step (heldtwice.py): move the Tracks both copies of an album hold out of
    the copy marked quarantine. Every File is checked before any moves: the File kept and the
    File moved must both still be listed, at the size they had when planned. A File already
    in Quarantine at that size was moved by an earlier, interrupted run."""
    try:
        kept, away, moves = parse_line(fields)
    except ValueError as e:
        raise RuntimeError(f"not a valid held-twice line: {e}") from e
    todo = []
    for m in moves:
        if _size(root, m.keep) != m.keep_size:
            raise RuntimeError(f"the File to keep is missing or changed since planning: {m.keep!r}; plan again")
        dst = f"{QUARANTINE}/{m.src}"
        size = _size(root, m.src)
        if size is None and _size(root, dst) == m.size:
            continue
        if size != m.size:
            raise RuntimeError(f"missing or changed since planning: {m.src!r}; plan again")
        if present_any_case(root, dst):
            raise RuntimeError(f"destination already exists (in some letter case): {dst}")
        todo.append((m.src, dst))
    if todo:
        ensure_quarantine(root)
    for src, dst in todo:
        relocate(root, src, dst)
    earlier = len(moves) - len(todo)
    return f"ok: {len(todo)} Files moved out of {away!r}, keeping {kept!r}" + (f" ({earlier} moved earlier)" if earlier else "")


def read_progress(log_path: str) -> set[str]:
    """The steps a run of this plan already finished, each as its line of the plan.

    Steps are recorded whole, not by number, so a new plan written to the same name, or a plan
    edited after a stop, never takes another plan's progress for its own.
    """
    if not os.path.exists(log_path):
        return set()
    with open(log_path, encoding="utf-8") as fh:
        lines = [line.rstrip("\n") for line in fh if line.strip()]
    if any(line.isdigit() for line in lines):
        raise ValueError(
            f"{log_path} records steps by number, as apply.py did before, so it can't tell which steps "
            "of this plan it means. If the plan is new, move it aside and run again; if this run was "
            "stopped part-way, check by hand which steps were done and delete them from the plan first."
        )
    return set(lines)


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2
    root, plan_path, dry = argv[0], argv[1], "--dry-run" in argv[2:]
    with open(plan_path, encoding="utf-8") as fh:
        plan = [line.rstrip("\n").split("\t") for line in fh if line.strip()]
    log_path = plan_path + ".done"
    try:
        finished = read_progress(log_path)
    except ValueError as e:
        print(e, file=sys.stderr)
        return 2
    log = None if dry else open(log_path, "a", encoding="utf-8")
    done = 0
    for i, (op, src, *rest) in enumerate(plan, 1):
        dst = rest[0] if rest else ""
        step = "\t".join([op, src, *rest])
        if step in finished:
            continue
        if op == HELD_TWICE:
            print(f"[{i}/{len(plan)}] {op}\t{describe([op, src, *rest])}", flush=True)
        else:
            print(f"[{i}/{len(plan)}] {op}\t{src}" + (f"\t-> {dst}" if dst else ""), flush=True)
        if dry or op == "decide":
            continue
        try:
            if op == "rename":
                result = relocate(root, src, dst, twin_ok=True)
            elif op == "move":
                result = relocate(root, src, dst)
            elif op in DUPLICATE_CHECKS:
                same, differs = DUPLICATE_CHECKS[op]
                keep = rest[1] if len(rest) > 1 else ""
                if not keep or keep.casefold() == src.casefold() or keep.casefold().startswith(QUARANTINE.casefold() + "/"):
                    raise RuntimeError(f"the File to keep must be another File in the Library, not {keep!r}")
                if not same(os.path.join(root, src), os.path.join(root, keep)):
                    raise RuntimeError(f"{differs} {keep!r}, so not a Duplicate; decide by hand")
                ensure_quarantine(root)
                result = relocate(root, src, dst)
            elif op == "set-aside":
                keep = rest[1] if len(rest) > 1 else ""
                if not keep or not os.path.isfile(os.path.join(root, keep)):
                    raise RuntimeError(f"the File to keep is missing: {keep!r}")
                ensure_quarantine(root)
                result = relocate(root, src, dst)
            elif op == HELD_TWICE:
                result = quarantine_copy(root, [op, src, *rest])
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
        log.write(step + "\n")
        log.flush()
        print(f"    {result}", flush=True)
    print(f"{done} steps applied" + (" (dry run)" if dry else ""), file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
