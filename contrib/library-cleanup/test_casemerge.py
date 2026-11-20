# SPDX-License-Identifier: GPL-3.0-only
import unittest

from casemerge import Action, Entry, find_case_clashes, plan_case_merges, plan_disambiguation


def d(path: str) -> Entry:
    return Entry("d", 0, path)


def f(path: str, size: int = 100) -> Entry:
    return Entry("f", size, path)


class FindCaseClashes(unittest.TestCase):
    def test_finds_sibling_folders_differing_only_by_case(self) -> None:
        scan = [d("Music"), d("Music/BLONDIE"), d("Music/Blondie"), d("Music/Queen")]

        self.assertEqual(find_case_clashes(scan), [("Music", ["BLONDIE", "Blondie"])])


class PlanDisambiguation(unittest.TestCase):
    def test_renames_one_side_of_each_outermost_clash_and_leaves_nested_clashes_for_later(self) -> None:
        scan = [
            f("Music/OutKast/Aquemini/01 Hold On.mp3"),
            f("Music/Outkast/STANKONIA/01 Intro.mp3"),
            f("Music/Outkast/Stankonia/02 Gasoline Dreams.mp3"),
            f("Music/Blondie/Atomic.mp3"),
            f("Music/Blondie/atomic.mp3"),
        ]

        self.assertEqual(
            plan_disambiguation(scan),
            [
                Action("rename", "Music/Blondie/atomic.mp3", "Music/Blondie/atomic [case-merge].mp3"),
                Action("rename", "Music/Outkast", "Music/Outkast [case-merge]"),
            ],
        )


class PlanCaseMerges(unittest.TestCase):
    """Phase B: after Phase A, merge each "[case-merge]" sibling into its partner."""

    def test_moves_the_smaller_sides_contents_into_the_side_with_more_files(self) -> None:
        scan = [
            d("Music"),
            d("Music/BLONDIE [case-merge]"),
            f("Music/BLONDIE [case-merge]/Call Me.mp3"),
            d("Music/Blondie"),
            f("Music/Blondie/Atomic.mp3"),
            f("Music/Blondie/Maria.mp3"),
        ]

        self.assertEqual(
            plan_case_merges(scan),
            [
                Action("move", "Music/BLONDIE [case-merge]/Call Me.mp3", "Music/Blondie/Call Me.mp3"),
                Action("rmdir", "Music/BLONDIE [case-merge]"),
            ],
        )

    def test_when_the_renamed_side_wins_it_takes_back_the_preferred_name(self) -> None:
        scan = [
            f("Music/BANGLES/Eternal Flame.mp3"),
            f("Music/Bangles [case-merge]/Manic Monday.mp3"),
        ]

        self.assertEqual(
            plan_case_merges(scan),
            [
                Action("move", "Music/BANGLES/Eternal Flame.mp3", "Music/Bangles [case-merge]/Eternal Flame.mp3"),
                Action("rmdir", "Music/BANGLES"),
                Action("rename", "Music/Bangles [case-merge]", "Music/Bangles"),
            ],
        )

    def test_merges_nested_folders_that_clash_once_their_parents_are_merged(self) -> None:
        scan = [
            f("Music/Outkast/Stankonia/01 Intro.mp3"),
            f("Music/Outkast/Stankonia/02 Gasoline Dreams.mp3"),
            f("Music/OutKast [case-merge]/STANKONIA/03 So Fresh, So Clean.mp3"),
        ]

        self.assertEqual(
            plan_case_merges(scan),
            [
                Action(
                    "move",
                    "Music/OutKast [case-merge]/STANKONIA/03 So Fresh, So Clean.mp3",
                    "Music/Outkast/Stankonia/03 So Fresh, So Clean.mp3",
                ),
                Action("rmdir", "Music/OutKast [case-merge]/STANKONIA"),
                Action("rmdir", "Music/OutKast [case-merge]"),
            ],
        )

    def test_a_same_named_file_of_the_same_size_goes_to_quarantine_named_after_the_file_it_duplicates(self) -> None:
        scan = [
            f("Music/Blondie/Atomic.mp3", 500),
            f("Music/Blondie/Maria.mp3", 400),
            f("Music/BLONDIE [case-merge]/atomic.mp3", 500),
        ]

        self.assertEqual(
            plan_case_merges(scan),
            [
                Action("quarantine", "Music/BLONDIE [case-merge]/atomic.mp3", "_Quarantine/Music/Blondie/Atomic.mp3", "Music/Blondie/Atomic.mp3"),
                Action("rmdir", "Music/BLONDIE [case-merge]"),
            ],
        )

    def test_a_same_named_file_of_a_different_size_is_left_for_the_owner_to_decide(self) -> None:
        scan = [
            f("Music/Blondie/Atomic.mp3", 500),
            f("Music/Blondie/Maria.mp3", 400),
            f("Music/BLONDIE [case-merge]/Atomic.mp3", 900),
            f("Music/BLONDIE [case-merge]/Call Me.mp3", 300),
        ]

        self.assertEqual(
            plan_case_merges(scan),
            [
                Action("decide", "Music/BLONDIE [case-merge]/Atomic.mp3", "Music/Blondie/Atomic.mp3"),
                Action("move", "Music/BLONDIE [case-merge]/Call Me.mp3", "Music/Blondie/Call Me.mp3"),
            ],
        )

    def test_a_renamed_file_pair_of_the_same_size_quarantines_the_renamed_one(self) -> None:
        scan = [f("Music/Blondie/Atomic.mp3", 500), f("Music/Blondie/atomic [case-merge].mp3", 500)]

        self.assertEqual(
            plan_case_merges(scan),
            [Action("quarantine", "Music/Blondie/atomic [case-merge].mp3", "_Quarantine/Music/Blondie/Atomic.mp3", "Music/Blondie/Atomic.mp3")],
        )

    def test_several_copies_of_one_file_get_distinct_quarantine_paths(self) -> None:
        scan = [
            f("Music/Blondie/Atomic.mp3", 500),
            f("Music/Blondie/atomic [case-merge].mp3", 500),
            f("Music/Blondie/ATOMIC [case-merge 2].mp3", 500),
        ]

        self.assertEqual(
            [a.dst for a in plan_case_merges(scan)],
            ["_Quarantine/Music/Blondie/Atomic.mp3", "_Quarantine/Music/Blondie/Atomic [2].mp3"],
        )

    def test_refuses_to_plan_while_names_still_clash(self) -> None:
        scan = [f("Music/ATB/a.mp3"), f("Music/Atb/b.mp3"), f("Music/atb [case-merge]/c.mp3")]

        with self.assertRaises(ValueError):
            plan_case_merges(scan)


if __name__ == "__main__":
    unittest.main()
