# SPDX-License-Identifier: GPL-3.0-only
import contextlib
import io
import os
import tempfile
import unittest

from casemerge import Entry
from retag import import_paths, loose_files, main


def scan(*paths: str) -> list[Entry]:
    """A scan holding these Files and every folder above them."""
    folders = {"/".join(p.split("/")[:i]) for p in paths for i in range(1, p.count("/") + 1)}
    return [Entry("d", 0, d) for d in sorted(folders)] + [Entry("f", 1000, p) for p in paths]


class ImportPaths(unittest.TestCase):
    def test_imports_each_top_level_folder_holding_music(self) -> None:
        share = scan("Radiohead/The Bends/01 Planet Telex.mp3", "Queen/A Night At The Opera/01 Death On Two Legs.m4a",
                     "Scans/cover.jpg")

        self.assertEqual(import_paths(share), ["Queen", "Radiohead"])

    def test_leaves_out_quarantine_the_recycle_bin_and_ignored_folders(self) -> None:
        share = scan("_Quarantine/Radiohead/The Bends/01 Planet Telex 1.mp3", "trashbox/Old/01 Song.mp3",
                     "TrashBox/Older/01 Song.mp3", "Mixes/.ndignore", "Mixes/Summer/01 Song.mp3",
                     "Queen/Innuendo/01 Innuendo.mp3")

        self.assertEqual(import_paths(share), ["Queen"])

    def test_imports_around_an_ignored_folder_inside_a_folder(self) -> None:
        # beets would walk into Radiohead/Bootlegs if given Radiohead, so its siblings go instead.
        share = scan("Radiohead/The Bends/01 Planet Telex.mp3", "Radiohead/OK Computer/CD1/01 Airbag.mp3",
                     "Radiohead/Bootlegs/.ndignore", "Radiohead/Bootlegs/Live/01 Creep.mp3",
                     "Radiohead/notes.txt", "Queen/Innuendo/01 Innuendo.mp3")

        self.assertEqual(import_paths(share), ["Queen", "Radiohead/OK Computer", "Radiohead/The Bends"])

    def test_leaves_loose_files_beside_an_ignored_folder_to_the_owner(self) -> None:
        # Given one by one, beets would take each for an album of one Track.
        share = scan("Radiohead/Creep.mp3", "Radiohead/Bootlegs/.ndignore", "Radiohead/Bootlegs/01 Creep.mp3",
                     "Loose.mp3", "Queen/Innuendo/01 Innuendo.mp3")

        self.assertEqual(import_paths(share), ["Queen"])
        self.assertEqual(loose_files(share), ["Loose.mp3", "Radiohead/Creep.mp3"])

    def test_imports_nothing_when_the_share_root_has_a_ndignore(self) -> None:
        self.assertEqual(import_paths(scan(".ndignore", "Queen/Innuendo/01 Innuendo.mp3")), [])

    def test_imports_only_the_folders_asked_for_as_a_trial(self) -> None:
        share = scan("Radiohead/The Bends/01 Planet Telex.mp3", "Radiohead/OK Computer/CD1/01 Airbag.mp3",
                     "Radiohead/OK Computer/CD2/01 Lull.mp3", "Radiohead/Bootlegs/.ndignore",
                     "Radiohead/Bootlegs/Live/01 Creep.mp3", "Queen/Innuendo/01 Innuendo.mp3", "Queen Live/01 Tie Your Mother Down.mp3")

        self.assertEqual(import_paths(share, ["Radiohead/OK Computer/", "Queen", "Radiohead/Bootlegs/Live"]),
                         ["Queen", "Radiohead/OK Computer"])
        self.assertEqual(import_paths(share, ["Radiohead"]), ["Radiohead/OK Computer", "Radiohead/The Bends"])


class Main(unittest.TestCase):
    def run_main(self, share: list[Entry], *folders: str) -> tuple[int, str, str]:
        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, "scan.tsv")
            with open(path, "w", encoding="utf-8") as fh:
                fh.writelines(f"{e.kind}\t{e.size}\t{e.path}\n" for e in share)
            out, err = io.StringIO(), io.StringIO()
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
                code = main([path, *folders])
        return code, out.getvalue(), err.getvalue()

    def test_prints_one_path_per_line(self) -> None:
        code, out, err = self.run_main(scan("Radiohead/The Bends/01 Planet Telex.mp3", "Queen/Innuendo/01 Innuendo.mp3"))

        self.assertEqual((code, out), (0, "Queen\nRadiohead\n"))
        self.assertIn("2 paths to retag, holding 2 audio Files", err)

    def test_warns_about_copy_named_files_left_by_the_duplicate_passes(self) -> None:
        code, _, err = self.run_main(scan("Radiohead/The Bends/01 Planet Telex.mp3", "Radiohead/The Bends/01 Planet Telex 1.mp3"))

        self.assertEqual(code, 0)
        self.assertIn("1 copy-named Files", err)
        self.assertIn("Radiohead/The Bends/01 Planet Telex 1.mp3", err)

    def test_lists_the_loose_files_left_to_the_owner(self) -> None:
        code, out, err = self.run_main(scan("Loose.mp3", "Queen/Innuendo/01 Innuendo.mp3"))

        self.assertEqual((code, out), (0, "Queen\n"))
        self.assertIn("1 audio Files beside a left-out folder, not retagged: retag them by hand", err)
        self.assertIn("  Loose.mp3", err)

    def test_refuses_a_folder_the_scan_does_not_hold(self) -> None:
        code, out, err = self.run_main(scan("Radiohead/The Bends/01 Planet Telex.mp3"), "Radiohead/The Bands")

        self.assertEqual((code, out), (1, ""))
        self.assertIn("Radiohead/The Bands", err)


if __name__ == "__main__":
    unittest.main()
