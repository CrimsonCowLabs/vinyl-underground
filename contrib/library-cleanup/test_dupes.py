# SPDX-License-Identifier: GPL-3.0-only
import unittest

from casemerge import Action, Entry
from dupes import choose_keeper, folder_impact, ignored_folders, parse_jdupes, plan_duplicates

REPORT = """\
28672 bytes each:
Live At Monterey/VIDEO_TS/VTS_02_0.BUP
Live At Monterey/VIDEO_TS/VTS_02_0.IFO

4000000 bytes each:
iTunes_Library/Music/Queen/A Night At The Opera/God Save The Queen.mp3
iTunes_Library/Music/Queen/A Night At The Opera/God Save The Queen 1.mp3

"""


class ParseJdupes(unittest.TestCase):
    def test_reads_each_set_with_or_without_the_size_line(self) -> None:
        report = REPORT + "a/Song.mp3\nb/Song.mp3\n"

        self.assertEqual(
            parse_jdupes(report),
            [
                ["Live At Monterey/VIDEO_TS/VTS_02_0.BUP", "Live At Monterey/VIDEO_TS/VTS_02_0.IFO"],
                [
                    "iTunes_Library/Music/Queen/A Night At The Opera/God Save The Queen.mp3",
                    "iTunes_Library/Music/Queen/A Night At The Opera/God Save The Queen 1.mp3",
                ],
                ["a/Song.mp3", "b/Song.mp3"],
            ],
        )


class ChooseKeeper(unittest.TestCase):
    def test_keeps_the_name_a_music_app_copied_from(self) -> None:
        self.assertEqual(choose_keeper(["A/Song 1.mp3", "A/Song.mp3", "A/Song 2.mp3"]), "A/Song.mp3")
        self.assertEqual(choose_keeper(["A/Song (1).mp3", "A/Song.mp3"]), "A/Song.mp3")
        self.assertEqual(
            choose_keeper(["A/Copy of Copy of Song.mp3", "A/Copy (2) of Song.mp3", "A/Copy of Song.mp3", "A/Song.mp3"]),
            "A/Song.mp3",
        )

    def test_a_trailing_number_alone_is_not_a_copy_marker(self) -> None:
        # "6 of 6" ends in a number but is the original; only "6 of 6 1" is the copy.
        self.assertEqual(choose_keeper(["King/6 of 6 1.mp3", "King/6 of 6.mp3"]), "King/6 of 6.mp3")

    def test_prefers_the_copy_in_the_more_organised_folder(self) -> None:
        # Byte-identical, so the tags (and the app's Artist and Album) are the same either way.
        self.assertEqual(
            choose_keeper(["Mumford And Sons/12 After the Storm.mp3", "Music/Mumford And Sons/Sigh No More/12 After the Storm.mp3"]),
            "Music/Mumford And Sons/Sigh No More/12 After the Storm.mp3",
        )

    def test_a_copy_marker_outweighs_a_deeper_folder(self) -> None:
        self.assertEqual(choose_keeper(["Deep/er/Folder/Song 1.mp3", "Top/Song.mp3"]), "Top/Song.mp3")

    def test_otherwise_keeps_the_shortest_then_first_name(self) -> None:
        self.assertEqual(choose_keeper(["A/Song 3.mp3", "A/Song 1.mp3"]), "A/Song 1.mp3")
        self.assertEqual(choose_keeper(["A/Hey Jude (Remaster).mp3", "A/Hey Jude.mp3"]), "A/Hey Jude.mp3")


class PlanDuplicates(unittest.TestCase):
    def test_quarantines_every_copy_but_the_keeper_at_its_own_relative_path(self) -> None:
        sets = [["Music/Queen/Opera/Song 1.mp3", "Music/Queen/Opera/Song.mp3", "Queen/Song.mp3"]]

        self.assertEqual(
            plan_duplicates(sets),
            [
                Action("quarantine", "Music/Queen/Opera/Song 1.mp3", "_Quarantine/Music/Queen/Opera/Song 1.mp3", "Music/Queen/Opera/Song.mp3"),
                Action("quarantine", "Queen/Song.mp3", "_Quarantine/Queen/Song.mp3", "Music/Queen/Opera/Song.mp3"),
            ],
        )

    def test_leaves_sets_that_are_not_audio_alone(self) -> None:
        # A DVD's .IFO and .BUP are identical on purpose; only audio Files can be Duplicates.
        self.assertEqual(plan_duplicates(parse_jdupes(REPORT))[0].src, "iTunes_Library/Music/Queen/A Night At The Opera/God Save The Queen 1.mp3")
        self.assertEqual(len(plan_duplicates(parse_jdupes(REPORT))), 1)

    def test_ignores_files_already_in_quarantine(self) -> None:
        self.assertEqual(plan_duplicates([["_Quarantine/A/Song.mp3", "A/Song.mp3"]]), [])

    def test_ignores_files_in_folders_the_scanner_skips(self) -> None:
        # A recycle bin with a .ndignore isn't in the Library: its copy must neither be kept nor moved.
        sets = [["trashbox/A/Song.mp3", "A/Song.mp3"], ["trashbox/B.mp3", "B/B.mp3", "C/B 1.mp3"]]

        self.assertEqual(
            plan_duplicates(sets, ignored={"trashbox"}),
            [Action("quarantine", "C/B 1.mp3", "_Quarantine/C/B 1.mp3", "B/B.mp3")],
        )

    def test_numbers_a_quarantine_path_that_is_already_taken_in_any_letter_case(self) -> None:
        plan = plan_duplicates([["A/Song 1.mp3", "A/Song.mp3"]], taken={"_quarantine/a/song 1.mp3"})

        self.assertEqual(plan[0].dst, "_Quarantine/A/Song 1 [2].mp3")


class IgnoredFolders(unittest.TestCase):
    def test_lists_every_folder_holding_a_ndignore(self) -> None:
        scan = [Entry("f", 0, "trashbox/.ndignore"), Entry("f", 0, "A/B/.ndignore"), Entry("f", 1, "C/.ndignore.mp3")]

        self.assertEqual(ignored_folders(scan), {"trashbox", "A/B"})


class FolderImpact(unittest.TestCase):
    def test_counts_audio_files_moving_out_of_each_top_level_folder(self) -> None:
        scan = [
            Entry("f", 1, "Hits/Song.mp3"),
            Entry("f", 1, "Hits/cover.jpg"),
            Entry("f", 1, "Music/A/Song.mp3"),
            Entry("f", 1, "Music/A/Other.mp3"),
        ]
        plan = [Action("quarantine", "Hits/Song.mp3", "_Quarantine/Hits/Song.mp3", "Music/A/Song.mp3")]

        self.assertEqual(folder_impact(scan, plan), [("Hits", 1, 1), ("Music", 0, 2)])


if __name__ == "__main__":
    unittest.main()
