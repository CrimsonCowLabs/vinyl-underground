# SPDX-License-Identifier: GPL-3.0-only
import unittest

from audiodupes import Found, candidate_sets, find_same_audio, plan_audio_duplicates
from casemerge import Action, Entry

ALBUM = "iTunes_Library/Music/Radiohead/The Bends"


def scan(*paths: str) -> list[Entry]:
    return [Entry("f", 1000, p) for p in paths]


class CandidateSets(unittest.TestCase):
    def test_groups_each_copy_named_file_with_the_file_it_was_copied_from(self) -> None:
        files = scan(
            f"{ALBUM}/09 Bullet Proof.mp3",
            f"{ALBUM}/09 Bullet Proof 1.mp3",
            f"{ALBUM}/09 Bullet Proof 2.mp3",
            f"{ALBUM}/10 Black Star.mp3",
            f"{ALBUM}/cover.jpg",
        )

        self.assertEqual(
            candidate_sets(files),
            [[f"{ALBUM}/09 Bullet Proof 1.mp3", f"{ALBUM}/09 Bullet Proof 2.mp3", f"{ALBUM}/09 Bullet Proof.mp3"]],
        )

    def test_only_compares_files_in_the_same_folder_with_the_same_extension(self) -> None:
        files = scan("A/Song.mp3", "B/Song 1.mp3", "A/Song 1.m4a", "A/Song (1).mp3")

        self.assertEqual(candidate_sets(files), [["A/Song (1).mp3", "A/Song.mp3"]])

    def test_a_copy_of_a_copy_joins_the_same_set(self) -> None:
        files = scan("A/Song.mp3", "A/Song 1.mp3", "A/Song 1 1.mp3", "A/Copy of Song 1.mp3")

        self.assertEqual(candidate_sets(files), [["A/Copy of Song 1.mp3", "A/Song 1 1.mp3", "A/Song 1.mp3", "A/Song.mp3"]])

    def test_a_trailing_number_without_its_original_is_not_a_copy(self) -> None:
        self.assertEqual(candidate_sets(scan("A/Track 1.mp3", "A/Track 2.mp3", "King/6 of 6.mp3")), [])

    def test_leaves_out_files_the_scanner_skips(self) -> None:
        files = scan("trashbox/.ndignore", "trashbox/A/Song.mp3", "trashbox/A/Song 1.mp3", "_Quarantine/B/Song.mp3", "_Quarantine/B/Song 1.mp3")

        self.assertEqual(candidate_sets(files), [])


class FindSameAudio(unittest.TestCase):
    # Fake Files: path -> (audio size, audio digest); None for a format that can't be compared.
    FILES = {
        "A/Song.mp3": (5000, "aaa"),
        "A/Song 1.mp3": (5000, "aaa"),  # same audio, other tags
        "A/Song 2.mp3": (4000, "bbb"),  # a different recording that got the same name
        "B/Live.mp3": (5000, "ccc"),
        "B/Live 1.mp3": (5000, "ddd"),  # same length, different audio
        "C/Old.wma": None,
        "C/Old 1.wma": None,
    }

    def find(self, sets: list[list[str]]) -> tuple[Found, list[str]]:
        hashed: list[str] = []

        def size_of(path: str) -> int | None:
            info = self.FILES[path]
            return None if info is None else info[0]

        def digest_of(path: str) -> str | None:
            hashed.append(path)
            info = self.FILES[path]
            return None if info is None else info[1]

        return find_same_audio(sets, size_of, digest_of), hashed

    def test_groups_the_files_whose_audio_is_identical(self) -> None:
        found, _ = self.find([["A/Song 1.mp3", "A/Song 2.mp3", "A/Song.mp3"], ["B/Live 1.mp3", "B/Live.mp3"]])

        self.assertEqual(found.sets, [["A/Song 1.mp3", "A/Song.mp3"]])
        self.assertEqual(found.different, ["A/Song 2.mp3", "B/Live 1.mp3", "B/Live.mp3"])

    def test_only_reads_the_audio_of_files_whose_audio_is_the_same_size(self) -> None:
        _, hashed = self.find([["A/Song 2.mp3", "A/Song.mp3"], ["A/Song 1.mp3", "A/Song.mp3"]])

        self.assertEqual(sorted(hashed), ["A/Song 1.mp3", "A/Song.mp3"])

    def test_reports_files_it_cannot_compare_instead_of_planning_them(self) -> None:
        found, _ = self.find([["C/Old 1.wma", "C/Old.wma"]])

        self.assertEqual((found.sets, found.unsupported), ([], ["C/Old 1.wma", "C/Old.wma"]))

    def test_reports_files_it_cannot_read_and_carries_on(self) -> None:
        def size_of(path: str) -> int | None:
            if path == "A/Song 1.mp3":
                raise FileNotFoundError(path)
            return 5000

        found = find_same_audio([["A/Song 1.mp3", "A/Song.mp3"], ["B/Live 1.mp3", "B/Live.mp3"]], size_of, lambda p: "x")

        self.assertEqual(found.sets, [["B/Live 1.mp3", "B/Live.mp3"]])
        self.assertEqual([path for path, _ in found.errors], ["A/Song 1.mp3"])


class PlanAudioDuplicates(unittest.TestCase):
    def test_quarantines_all_but_the_original_with_a_step_that_rechecks_the_audio(self) -> None:
        sets = [[f"{ALBUM}/09 Bullet Proof 1.mp3", f"{ALBUM}/09 Bullet Proof 2.mp3", f"{ALBUM}/09 Bullet Proof.mp3"]]

        self.assertEqual(
            plan_audio_duplicates(sets, taken={f"_Quarantine/{ALBUM}/09 Bullet Proof 2.mp3"}),
            [
                Action("quarantine-audio", f"{ALBUM}/09 Bullet Proof 1.mp3", f"_Quarantine/{ALBUM}/09 Bullet Proof 1.mp3", f"{ALBUM}/09 Bullet Proof.mp3"),
                Action("quarantine-audio", f"{ALBUM}/09 Bullet Proof 2.mp3", f"_Quarantine/{ALBUM}/09 Bullet Proof 2 [2].mp3", f"{ALBUM}/09 Bullet Proof.mp3"),
            ],
        )


if __name__ == "__main__":
    unittest.main()
