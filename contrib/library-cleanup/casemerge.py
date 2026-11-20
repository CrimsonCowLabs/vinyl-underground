# SPDX-License-Identifier: GPL-3.0-only
"""Plan merges of sibling names that differ only by letter case.

Some NAS devices (e.g. old Buffalo LinkStations over SMB1) match names
case-insensitively and cache the lookups, so for two siblings such as `ATB`
and `Atb` both paths can lead into the same real folder. Listings of either
side can't be trusted until the names are unique. So merging takes two phases:

  A. `plan_disambiguation`: rename all but one member of each outermost clash
     to a unique temporary name ("Atb [case-merge]"). Renames can't lose data,
     whichever real folder the server applies them to. Rescan, and repeat
     until no clashes are left (nested clashes surface as their parents become unique).
  B. `plan_case_merges`: with every name unique, merge each "[case-merge]"
     sibling into its partner. The side with more Files wins; same-name Files of
     the same size go to Quarantine; anything else is left for the owner to decide.

This module is pure: it never touches the filesystem.
"""
from __future__ import annotations

import os
import re
import sys
from collections import defaultdict
from typing import Iterable, Iterator, NamedTuple


class Entry(NamedTuple):
    kind: str  # "d" for a folder, "f" for a file (audio or not)
    size: int
    path: str  # relative to the share root, "/"-separated


QUARANTINE = "_Quarantine"


class Action(NamedTuple):
    # "rename" src to dst; "move" src to dst; "rmdir" an emptied folder; "quarantine" src to
    # dst if its bytes match keep (apply.py checks); "decide": src clashes with dst but
    # differs, so the owner chooses.
    op: str
    src: str
    dst: str = ""
    keep: str = ""  # for "quarantine": the File it duplicates, which stays


class _Node:
    def __init__(self, kind: str, size: int = 0):
        self.kind = kind
        self.size = size
        self.children: dict[str, _Node] = {}

    def file_count(self) -> int:
        if self.kind == "f":
            return 1
        return sum(c.file_count() for c in self.children.values())


def _build_tree(scan: Iterable[Entry]) -> _Node:
    root = _Node("d")
    for e in scan:
        node = root
        parts = e.path.split("/")
        for part in parts[:-1]:
            node = node.children.setdefault(part, _Node("d"))
        existing = node.children.get(parts[-1])
        if existing is None:
            node.children[parts[-1]] = _Node(e.kind, e.size)
        elif e.kind == "f":
            existing.kind, existing.size = "f", e.size
    return root


def _join(parent: str, name: str) -> str:
    return f"{parent}/{name}" if parent else name


# Phase A's temporary-name tag: " [case-merge]", then " [case-merge 2]", ...
_TAG = "case-merge"
_TAG_RE = re.compile(rf" \[{_TAG}(?: \d+)?\]")


def _with_tag(name: str, kind: str, n: int) -> str:
    tag = f" [{_TAG}]" if n == 1 else f" [{_TAG} {n}]"
    stem, ext = os.path.splitext(name) if kind == "f" else (name, "")
    return f"{stem}{tag}{ext}"


def _strip_tag(name: str) -> str:
    """The name without its Phase A tag ("Atb [case-merge]" -> "Atb")."""
    return _TAG_RE.sub("", name, count=1)


def _name_preference(name: str) -> tuple[bool, str]:
    """Sort key for which spelling of a name to keep: not ALL CAPS first, then alphabetical."""
    return (name.isupper(), name)


def _parents_and_names(path: str) -> Iterator[tuple[str, str]]:
    """Every (parent, name) along a path, so folders count even if the scan didn't list them."""
    parts = path.split("/")
    for i in range(len(parts)):
        yield "/".join(parts[:i]), parts[i]


def _quarantine_path(keep: str, plan: list[Action]) -> str:
    """Quarantine path named after the kept File, numbered if several copies of it go there."""
    taken = {a.dst.casefold() for a in plan if a.op == "quarantine"}
    stem, ext = os.path.splitext(_join(QUARANTINE, keep))
    path, n = stem + ext, 1
    while path.casefold() in taken:
        n += 1
        path = f"{stem} [{n}]{ext}"
    return path


def _merge_into(winner: _Node, wpath: str, loser: _Node, lpath: str, plan: list[Action]) -> None:
    for name, child in sorted(loser.children.items()):
        match = next((n for n in winner.children if n.casefold() == name.casefold()), None)
        if match is not None and child.kind == "d" and winner.children[match].kind == "d":
            _merge_into(winner.children[match], _join(wpath, match), child, _join(lpath, name), plan)
            if not child.children:
                del loser.children[name]
            continue
        if match is not None:
            _resolve_file_clash(winner.children[match], _join(wpath, match), child, _join(lpath, name), plan)
            if child.kind == "f" and plan[-1].op == "quarantine":
                del loser.children[name]
            continue
        plan.append(Action("move", _join(lpath, name), _join(wpath, name)))
        winner.children[name] = child
        del loser.children[name]
    if not loser.children:
        plan.append(Action("rmdir", lpath))


def _resolve_file_clash(keep: _Node, kpath: str, other: _Node, opath: str, plan: list[Action]) -> None:
    if keep.kind == "f" and other.kind == "f" and keep.size == other.size:
        plan.append(Action("quarantine", opath, _quarantine_path(kpath, plan), kpath))
    else:
        plan.append(Action("decide", opath, kpath))


def _merge_rank(node: _Node, final_name: str, renamed: bool) -> tuple[int, bool, bool]:
    """Lower wins: more Files, then a name that isn't ALL CAPS, then the side not renamed."""
    return (-node.file_count(), final_name.isupper(), renamed)


def _plan_dir(node: _Node, path: str, plan: list[Action]) -> None:
    groups: dict[str, list[str]] = defaultdict(list)
    for name in node.children:
        groups[_strip_tag(name).casefold()].append(name)
    for names in sorted(groups.values()):
        tagged_names = sorted(n for n in names if _TAG_RE.search(n))
        partner_name = next((n for n in names if not _TAG_RE.search(n)), None)
        if partner_name is None or not tagged_names:
            continue
        for tagged_name in tagged_names:
            partner, tagged = node.children[partner_name], node.children[tagged_name]
            partner_path, tagged_path = _join(path, partner_name), _join(path, tagged_name)
            if partner.kind == "f" or tagged.kind == "f":
                _resolve_file_clash(partner, partner_path, tagged, tagged_path, plan)
                if plan[-1].op == "quarantine":
                    del node.children[tagged_name]
                continue
            if _merge_rank(partner, partner_name, False) <= _merge_rank(tagged, _strip_tag(tagged_name), True):
                _merge_into(partner, partner_path, tagged, tagged_path, plan)
                if not tagged.children:
                    del node.children[tagged_name]
                continue
            _merge_into(tagged, tagged_path, partner, partner_path, plan)
            if partner.children:
                continue
            del node.children[partner_name]
            final = min(partner_name, _strip_tag(tagged_name), key=_name_preference)
            plan.append(Action("rename", tagged_path, _join(path, final)))
            node.children[final] = node.children.pop(tagged_name)
            partner_name = final
    for name, child in sorted(node.children.items()):
        if child.kind == "d":
            _plan_dir(child, _join(path, name), plan)


def plan_case_merges(scan: Iterable[Entry]) -> list[Action]:
    """Phase B: the actions, in execution order, that merge each "[case-merge]" sibling into its partner."""
    entries = list(scan)
    clashes = find_case_clashes(entries)
    if clashes:
        parent, names = clashes[0]
        raise ValueError(f"{len(clashes)} names still clash (e.g. {names} in {parent!r}); run Phase A again first")
    plan: list[Action] = []
    _plan_dir(_build_tree(entries), "", plan)
    return plan


def plan_disambiguation(scan: Iterable[Entry]) -> list[Action]:
    """Phase A: give all but one member of each outermost clash a unique temporary name.

    Renaming is safe even when the server resolves a name to the wrong sibling,
    because a rename never loses data. Clashes nested under a clashing folder are
    left for a later pass, since their listings can't be trusted yet.
    """
    entries = list(scan)
    kinds = {e.path: e.kind for e in entries}
    siblings: dict[str, set[str]] = defaultdict(set)
    for e in entries:
        for parent, name in _parents_and_names(e.path):
            siblings[parent].add(name.casefold())
    clashes = find_case_clashes(entries)
    clashing = {_join(parent, n).casefold() for parent, names in clashes for n in names}
    plan: list[Action] = []
    for parent, names in clashes:
        parts = parent.split("/") if parent else []
        if any("/".join(parts[: i + 1]).casefold() in clashing for i in range(len(parts))):
            continue
        _kept, *to_rename = sorted(names, key=_name_preference)
        tag_number = 1
        for name in to_rename:
            kind = kinds.get(_join(parent, name), "d")
            while (new := _with_tag(name, kind, tag_number)).casefold() in siblings[parent]:
                tag_number += 1
            siblings[parent].add(new.casefold())
            plan.append(Action("rename", _join(parent, name), _join(parent, new)))
            tag_number += 1
    return sorted(plan)


def read_scans(paths: Iterable[str]) -> list[Entry]:
    """Read and combine scan.py TSV files, so a File seen by any scan is kept."""
    entries: list[Entry] = []
    for p in paths:
        with open(p, encoding="utf-8") as fh:
            for line in fh:
                kind, size, path = line.rstrip("\n").split("\t", 2)
                entries.append(Entry(kind, int(size), path))
    return entries


def main(argv: list[str]) -> int:
    if len(argv) < 2 or argv[0] not in ("rename", "merge"):
        print("usage: casemerge.py {rename|merge} SCAN.tsv [SCAN.tsv ...] > plan.tsv", file=sys.stderr)
        return 2
    scan = read_scans(argv[1:])
    try:
        plan = plan_disambiguation(scan) if argv[0] == "rename" else plan_case_merges(scan)
    except ValueError as e:
        print(f"error: {e}", file=sys.stderr)
        return 1
    for a in plan:
        print("\t".join(a))
    counts: dict[str, int] = defaultdict(int)
    for a in plan:
        counts[a.op] += 1
    print(f"{len(find_case_clashes(scan))} clashing groups; plan: {dict(counts)}", file=sys.stderr)
    return 0


def find_case_clashes(scan: Iterable[Entry]) -> list[tuple[str, list[str]]]:
    """Return (parent, names) for every group of siblings whose names differ only by case."""
    groups: dict[str, dict[str, set[str]]] = defaultdict(lambda: defaultdict(set))
    for e in scan:
        for parent, name in _parents_and_names(e.path):
            groups[parent][name.casefold()].add(name)
    return sorted(
        (parent, sorted(names))
        for parent, by_key in groups.items()
        for names in by_key.values()
        if len(names) > 1
    )


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
