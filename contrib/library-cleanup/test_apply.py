# SPDX-License-Identifier: GPL-3.0-only
import contextlib
import io
import os
import shutil
import tempfile
import unittest

import apply
from test_audio import FRAMES, OTHER_FRAMES, id3v1, id3v2

ALBUM = "Radiohead/The Bends"


class QuarantineAudio(unittest.TestCase):
    def setUp(self) -> None:
        self.root, self.plans = tempfile.mkdtemp(), tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.root)
        self.addCleanup(shutil.rmtree, self.plans)
        os.makedirs(os.path.join(self.root, ALBUM))

    def put(self, rel: str, data: bytes) -> None:
        with open(os.path.join(self.root, rel), "wb") as fh:
            fh.write(data)

    def exists(self, rel: str) -> bool:
        return os.path.exists(os.path.join(self.root, rel))

    def run_plan(self, *lines: str) -> int:
        plan = os.path.join(self.plans, f"plan-{len(os.listdir(self.plans))}.tsv")
        with open(plan, "w", encoding="utf-8") as fh:
            fh.write("".join(line + "\n" for line in lines))
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            return apply.main([self.root, plan])

    def step(self, op: str, src: str, keep: str) -> str:
        return "\t".join([op, src, f"_Quarantine/{src}", keep])

    def test_moves_a_file_whose_audio_matches_though_its_tags_differ(self) -> None:
        self.put(f"{ALBUM}/09 Bullet Proof.mp3", id3v2("Bullet Proof") + FRAMES + id3v1("Bullet Proof"))
        self.put(f"{ALBUM}/09 Bullet Proof 1.mp3", id3v2("Bullet Proof..I Wish I Was", padding=500) + FRAMES)

        code = self.run_plan(self.step("quarantine-audio", f"{ALBUM}/09 Bullet Proof 1.mp3", f"{ALBUM}/09 Bullet Proof.mp3"))

        self.assertEqual(code, 0)
        self.assertTrue(self.exists(f"_Quarantine/{ALBUM}/09 Bullet Proof 1.mp3"))
        self.assertTrue(self.exists("_Quarantine/.ndignore"))
        self.assertFalse(self.exists(f"{ALBUM}/09 Bullet Proof 1.mp3"))
        self.assertTrue(self.exists(f"{ALBUM}/09 Bullet Proof.mp3"))

    def test_stops_without_moving_when_the_audio_differs(self) -> None:
        self.put(f"{ALBUM}/Song.mp3", id3v2("Song") + FRAMES)
        self.put(f"{ALBUM}/Song 1.mp3", id3v2("Song") + OTHER_FRAMES)

        code = self.run_plan(self.step("quarantine-audio", f"{ALBUM}/Song 1.mp3", f"{ALBUM}/Song.mp3"))

        self.assertEqual(code, 1)
        self.assertTrue(self.exists(f"{ALBUM}/Song 1.mp3"))

    def test_stops_when_the_file_to_keep_is_gone_or_cannot_be_compared(self) -> None:
        self.put(f"{ALBUM}/Song 1.mp3", FRAMES)
        self.put(f"{ALBUM}/Song 1.ogg", b"OggS")
        self.put(f"{ALBUM}/Song.ogg", b"OggS")

        self.assertEqual(self.run_plan(self.step("quarantine-audio", f"{ALBUM}/Song 1.mp3", f"{ALBUM}/Song.mp3")), 1)
        self.assertEqual(self.run_plan(self.step("quarantine-audio", f"{ALBUM}/Song 1.ogg", f"{ALBUM}/Song.ogg")), 1)
        self.assertTrue(self.exists(f"{ALBUM}/Song 1.mp3"))
        self.assertTrue(self.exists(f"{ALBUM}/Song 1.ogg"))

    def test_never_moves_the_file_to_keep_itself(self) -> None:
        # An edited line could name the same File twice (in any letter case, on a
        # case-insensitive server), or keep a File that's already in Quarantine.
        self.put(f"{ALBUM}/Song.mp3", FRAMES)
        os.makedirs(os.path.join(self.root, "_Quarantine", ALBUM))
        self.put(f"_Quarantine/{ALBUM}/Song 1.mp3", FRAMES)

        for keep in (f"{ALBUM}/Song.mp3", f"{ALBUM}/Song.mp3".upper(), f"_Quarantine/{ALBUM}/Song 1.mp3"):
            for op in ("quarantine", "quarantine-audio"):
                self.assertEqual(self.run_plan(self.step(op, f"{ALBUM}/Song.mp3", keep)), 1, (op, keep))
        self.assertTrue(self.exists(f"{ALBUM}/Song.mp3"))

    def test_a_jdupes_quarantine_step_still_needs_identical_bytes(self) -> None:
        self.put(f"{ALBUM}/Song.mp3", id3v2("Song") + FRAMES)
        self.put(f"{ALBUM}/Song 1.mp3", id3v2("Song 1") + FRAMES)

        self.assertEqual(self.run_plan(self.step("quarantine", f"{ALBUM}/Song 1.mp3", f"{ALBUM}/Song.mp3")), 1)
        self.assertTrue(self.exists(f"{ALBUM}/Song 1.mp3"))


class Progress(unittest.TestCase):
    def setUp(self) -> None:
        self.root, self.plans = tempfile.mkdtemp(), tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.root)
        self.addCleanup(shutil.rmtree, self.plans)
        self.plan = os.path.join(self.plans, "audiodupes.tsv")
        os.makedirs(os.path.join(self.root, ALBUM))
        for name in ("Song.mp3", "Song 1.mp3", "Other.mp3", "Other 1.mp3"):
            with open(os.path.join(self.root, ALBUM, name), "wb") as fh:
                fh.write(id3v2(name) + FRAMES)

    def exists(self, rel: str) -> bool:
        return os.path.exists(os.path.join(self.root, rel))

    def step(self, name: str, keep: str) -> str:
        return "\t".join(["quarantine-audio", f"{ALBUM}/{name}", f"_Quarantine/{ALBUM}/{name}", f"{ALBUM}/{keep}"])

    def run_plan(self, *lines: str) -> int:
        with open(self.plan, "w", encoding="utf-8") as fh:
            fh.write("".join(line + "\n" for line in lines))
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            return apply.main([self.root, self.plan])

    def test_a_new_plan_with_the_same_name_does_not_take_the_old_ones_progress(self) -> None:
        self.assertEqual(self.run_plan(self.step("Song 1.mp3", "Song.mp3")), 0)

        self.assertEqual(self.run_plan(self.step("Other 1.mp3", "Other.mp3")), 0)

        self.assertTrue(self.exists(f"_Quarantine/{ALBUM}/Other 1.mp3"))
        self.assertFalse(self.exists(f"{ALBUM}/Other 1.mp3"))

    def test_a_plan_run_again_skips_the_steps_it_finished_even_once_edited(self) -> None:
        self.assertEqual(self.run_plan(self.step("Song 1.mp3", "Song.mp3")), 0)

        # Song 1.mp3 is gone now, so its step would stop the run if it weren't skipped.
        self.assertEqual(self.run_plan(self.step("Other 1.mp3", "Other.mp3"), self.step("Song 1.mp3", "Song.mp3")), 0)

        self.assertTrue(self.exists(f"_Quarantine/{ALBUM}/Other 1.mp3"))

    def test_refuses_progress_recorded_by_step_number_and_moves_nothing(self) -> None:
        with open(self.plan + ".done", "w", encoding="utf-8") as fh:
            fh.write("1\n2\n")

        self.assertEqual(self.run_plan(self.step("Song 1.mp3", "Song.mp3")), 2)

        self.assertTrue(self.exists(f"{ALBUM}/Song 1.mp3"))


if __name__ == "__main__":
    unittest.main()
