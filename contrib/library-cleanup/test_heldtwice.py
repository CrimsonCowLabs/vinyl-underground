# SPDX-License-Identifier: GPL-3.0-only
import contextlib
import io
import os
import shutil
import sqlite3
import tempfile
import unittest

import apply
from casemerge import Entry
from heldtwice import Stray, Track, plan_albums, plan_line, read_tracks

CURATED = "Radiohead/Albums/The Bends"
ITUNES = "iTunes_Library/Music/Radiohead/The Bends"
BENDS = ["Planet Telex", "The Bends", "High and Dry", "Fake Plastic Trees", "Bones"]


def track(path: str, title: str = "", **cols: object) -> Track:
    """A well-tagged 320 kbps MP3 of The Bends, unless cols says otherwise."""
    healthy = dict(path=path, size=1000, album_artist="Radiohead", album="The Bends", artist="Radiohead",
                   title=title or path.rsplit("/", 1)[-1].rsplit(".", 1)[0], duration=200.0, suffix="mp3",
                   bit_rate=320, tags=6, cover=False)
    return Track(**{**healthy, **cols})  # type: ignore[arg-type]


def album(folder: str, titles: list[str] = BENDS, **cols: object) -> list[Track]:
    return [track(f"{folder}/{i:02} {t}.mp3", t, **cols) for i, t in enumerate(titles, 1)]


class FindAlbumsHeldTwice(unittest.TestCase):
    def test_pairs_the_tracks_of_two_copies_by_artist_and_title_not_file_name(self) -> None:
        tracks = album(CURATED) + [track(f"{ITUNES}/{t} (iTunes).mp3", t.upper() + "!", bit_rate=192) for t in BENDS]

        [line] = plan_albums(tracks).lines

        self.assertEqual((line.keep, line.other), (CURATED, ITUNES))
        self.assertEqual(
            [(k.path, o.path) for k, o in line.pairs],
            [(f"{CURATED}/{i:02} {t}.mp3", f"{ITUNES}/{t} (iTunes).mp3") for i, t in enumerate(BENDS, 1)],
        )

    def test_reports_a_stray_track_in_a_folder_of_other_albums_instead_of_planning_it(self) -> None:
        mix = "Party Mix"
        others = [track(f"{mix}/{n:02} Other.mp3", f"Other {n}", album=f"Other {n}", album_artist=f"Band {n}") for n in range(9)]
        tracks = album(ITUNES) + others + [track(f"{mix}/10 Bones.mp3", "Bones")]

        planned = plan_albums(tracks)

        self.assertEqual(planned.lines, [])
        [stray] = planned.strays
        self.assertEqual((stray.folder, stray.tracks, stray.folder_tracks), (mix, 1, 10))

    def test_reports_a_lone_track_of_the_album_as_a_stray_even_in_a_folder_of_its_own(self) -> None:
        planned = plan_albums(album(CURATED) + [track("Singles/High and Dry.mp3", "High and Dry", bit_rate=128)])

        self.assertEqual(planned.lines, [])
        self.assertEqual(planned.strays, [Stray("Radiohead - The Bends", "Singles", 1, 1)])

    def test_plans_two_multi_disc_copies_disc_by_disc(self) -> None:
        tracks = (album("X/CD1", BENDS[:3], disc=1) + album("X/CD2", BENDS[3:] + ["Sulk"], disc=2)
                  + album("Y/CD1", BENDS[:3], disc=1, bit_rate=192) + album("Y/CD2", BENDS[3:] + ["Sulk"], disc=2, bit_rate=192))

        planned = plan_albums(tracks)

        self.assertEqual([(line.keep, line.other, len(line.pairs)) for line in planned.lines], [("X/CD1", "Y/CD1", 3), ("X/CD2", "Y/CD2", 3)])
        self.assertEqual(planned.unmatched, [])

    def test_leaves_alone_folders_that_share_few_tracks(self) -> None:
        # iTunes split one copy by Track artist: each folder holds different Tracks.
        tracks = (album("Ja Rule/Pain Is Love", BENDS[:4]) + album("Ja Rule feat. Case/Pain Is Love", BENDS[3:])
                  + album("Ja Rule ft. Missy/Pain Is Love", ["Sulk", "Just"]))

        planned = plan_albums(tracks)

        self.assertEqual(planned.lines, [])
        self.assertEqual(planned.unmatched, [  # each against the biggest copy
            ("Radiohead - The Bends", "Ja Rule feat. Case/Pain Is Love", "Ja Rule/Pain Is Love", 1, 2),
            ("Radiohead - The Bends", "Ja Rule ft. Missy/Pain Is Love", "Ja Rule/Pain Is Love", 0, 2),
        ])

    def test_leaves_alone_an_album_whose_folders_each_hold_a_different_disc(self) -> None:
        tracks = album("Box Set/CD 1", disc=1) + album("Box Set/CD 2", disc=2)

        self.assertEqual(plan_albums(tracks), plan_albums([]))

    def test_only_tracks_both_copies_hold_move(self) -> None:
        tracks = album(CURATED, BENDS[:4]) + album(ITUNES, BENDS, bit_rate=192)

        [line] = plan_albums(tracks).lines

        self.assertEqual([o.title for _k, o in line.pairs], BENDS[:4])
        self.assertEqual(line.only_other, 1)

    def test_matches_titles_that_differ_only_by_a_subtitle_when_the_lengths_agree(self) -> None:
        tracks = [
            track(f"{CURATED}/01 2+2=5.mp3", "2+2=5", duration=199.4),
            track(f"{CURATED}/02 Animals.mp3", "Animals", duration=180.0),
            track(f"{ITUNES}/01 2 + 2 = 5.mp3", "2 + 2 = 5 (The Lukewarm.)", duration=199.0, bit_rate=192),
            track(f"{ITUNES}/02 Animals (Remix).mp3", "Animals (Oliver Heldens Remix)", duration=300.0, bit_rate=192),
            track(f"{CURATED}/03 Bones.mp3", "Bones"),
            track(f"{ITUNES}/03 Bones.mp3", "Bones", bit_rate=192),
        ]

        [line] = plan_albums(tracks).lines

        self.assertEqual([(k.title, o.title) for k, o in line.pairs], [("2+2=5", "2 + 2 = 5 (The Lukewarm.)"), ("Bones", "Bones")])

    def test_a_title_held_more_than_once_moves_only_if_both_copies_hold_it_as_often(self) -> None:
        twice = ["Untitled", "Untitled", "Bones"]

        [same] = plan_albums(album(CURATED, twice) + album(ITUNES, twice, bit_rate=192)).lines
        [fewer] = plan_albums(album(CURATED, twice + ["Sulk"]) + album(ITUNES, twice[1:] + ["Sulk"], bit_rate=192)).lines

        self.assertEqual([o.path for _k, o in same.pairs], [f"{ITUNES}/01 Untitled.mp3", f"{ITUNES}/02 Untitled.mp3", f"{ITUNES}/03 Bones.mp3"])
        self.assertEqual(([o.title for _k, o in fewer.pairs], fewer.only_other), (["Bones", "Sulk"], 1))

    def test_leaves_copy_named_files_to_the_audio_duplicate_pass(self) -> None:
        tracks = album(CURATED) + album(ITUNES, bit_rate=192) + [track(f"{ITUNES}/05 Bones 1.mp3", "Bones", bit_rate=192)]

        [line] = plan_albums(tracks).lines

        self.assertNotIn(f"{ITUNES}/05 Bones 1.mp3", [o.path for _k, o in line.pairs])
        self.assertEqual((len(line.pairs), line.copy_named), (5, 1))

    def test_a_third_copy_is_planned_against_the_copy_kept(self) -> None:
        tracks = album(CURATED) + album(ITUNES, bit_rate=192) + album("Radiohead/The Bends (1995)", bit_rate=128)

        lines = plan_albums(tracks).lines

        self.assertEqual([(line.keep, line.other) for line in lines], [(CURATED, "Radiohead/The Bends (1995)"), (CURATED, ITUNES)])


class ChooseTheCopyToKeep(unittest.TestCase):
    def kept(self, curated: dict[str, object], itunes: dict[str, object]) -> tuple[str, str]:
        [line] = plan_albums(album(CURATED, **curated) + album(ITUNES, **itunes)).lines
        return line.keep, line.reason

    def test_lossless_first_whatever_the_bitrate(self) -> None:
        self.assertEqual(
            self.kept(dict(bit_rate=320), dict(suffix="flac", lossless=True, bit_rate=200, tags=1)),
            (ITUNES, "lossless (FLAC) over lossy (MP3)"),
        )

    def test_then_the_higher_bitrate(self) -> None:
        self.assertEqual(self.kept(dict(bit_rate=216, cover=True), dict(bit_rate=320, tags=1)), (ITUNES, "320 kbps over 216 kbps"))

    def test_then_the_more_complete_tags(self) -> None:
        self.assertEqual(self.kept(dict(tags=4, cover=True), dict(tags=6)), (ITUNES, "more complete tags (6.0 vs 4.0 of 6 per Track)"))

    def test_then_a_cover(self) -> None:
        self.assertEqual(self.kept(dict(), dict(cover=True)), (ITUNES, "has a cover, the other copy has none"))

    def test_then_more_tracks_then_the_first_folder(self) -> None:
        [line] = plan_albums(album(CURATED, BENDS[:4]) + album(ITUNES)).lines
        self.assertEqual((line.keep, line.reason), (ITUNES, "same quality; more Tracks (5 vs 4)"))
        self.assertEqual(self.kept(dict(), dict()), (CURATED, "same quality; kept the first folder"))

    def test_judges_only_the_tracks_both_copies_hold(self) -> None:
        tracks = album(CURATED, BENDS[:3]) + album(ITUNES, BENDS[:3], bit_rate=256) + [track(f"{CURATED}/09 Sulk.flac", "Sulk", lossless=True)]

        [line] = plan_albums(tracks).lines

        self.assertEqual((line.keep, line.reason), (CURATED, "320 kbps over 256 kbps"))


# Just the columns heldtwice.py reads, with the app's defaults.
SCHEMA = """
CREATE TABLE folder (id TEXT PRIMARY KEY, image_files TEXT NOT NULL DEFAULT '[]');
CREATE TABLE media_file (id TEXT PRIMARY KEY, path TEXT NOT NULL DEFAULT '', size INTEGER NOT NULL DEFAULT 0,
    title TEXT NOT NULL DEFAULT '', album TEXT NOT NULL DEFAULT '', artist TEXT NOT NULL DEFAULT '',
    album_artist TEXT NOT NULL DEFAULT '', folder_id TEXT NOT NULL DEFAULT '', library_id INTEGER NOT NULL DEFAULT 1,
    duration REAL NOT NULL DEFAULT 0, suffix TEXT NOT NULL DEFAULT '', codec TEXT NOT NULL DEFAULT '',
    bit_rate INTEGER NOT NULL DEFAULT 0, disc_number INTEGER NOT NULL DEFAULT 0, track_number INTEGER NOT NULL DEFAULT 0,
    year INTEGER NOT NULL DEFAULT 0, genre TEXT NOT NULL DEFAULT '', mbz_recording_id TEXT NOT NULL DEFAULT '',
    has_cover_art BOOL NOT NULL DEFAULT 0, missing BOOL NOT NULL DEFAULT 0);
"""


class ReadTracks(unittest.TestCase):
    def setUp(self) -> None:
        self.db = sqlite3.connect(":memory:")
        self.db.executescript(SCHEMA)
        self.db.execute("INSERT INTO folder VALUES ('plain', '[]'), ('art', '[\"cover.jpg\"]')")
        self.n = 0

    def add(self, path: str, **cols: object) -> None:
        self.n += 1
        row = {"id": str(self.n), "path": path, "size": 1000, "folder_id": "plain", **cols}
        self.db.execute(f"INSERT INTO media_file ({', '.join(row)}) VALUES ({', '.join('?' * len(row))})", list(row.values()))

    def test_reads_what_the_keeper_choice_needs(self) -> None:
        self.add("A/01 Bones.flac", title="Bones", album="The Bends", artist="Radiohead", album_artist="Radiohead",
                 duration=189.5, suffix="flac", bit_rate=900, disc_number=1, track_number=5, year=1995, genre="Rock")
        self.add("A/02 Sulk.m4a", suffix="m4a", codec="ALAC", artist="[Unknown Artist]", mbz_recording_id="x", folder_id="art")
        self.add("A/03 Just.m4a", suffix="m4a", codec="AAC", has_cover_art=1)

        read = read_tracks(self.db, [Entry("f", 1000, p) for p in ("A/01 Bones.flac", "A/02 Sulk.m4a", "A/03 Just.m4a")])

        bones, sulk, just = read.tracks
        self.assertEqual(bones, Track("A/01 Bones.flac", 1000, "Radiohead", "The Bends", "Radiohead", "Bones", 189.5,
                                      "flac", 900, 5, False, lossless=True, disc=1, number=5))
        self.assertEqual((sulk.lossless, sulk.tags, sulk.cover), (True, 1, True))
        self.assertEqual((just.lossless, just.tags, just.cover), (False, 0, True))

    def test_only_files_the_scan_lists_at_the_size_the_app_recorded(self) -> None:
        for path in ("A/same.mp3", "A/retagged.mp3", "A/gone.mp3", "trashbox/old.mp3"):
            self.add(path)
        self.add("A/missing.mp3", missing=1)
        self.add("A/inbox.mp3", library_id=2)
        scan = [Entry("f", 1000, "A/same.mp3"), Entry("f", 1200, "A/retagged.mp3"), Entry("f", 1000, "A/missing.mp3"),
                Entry("f", 1000, "A/inbox.mp3"), Entry("f", 0, "trashbox/.ndignore"), Entry("f", 1000, "trashbox/old.mp3")]

        read = read_tracks(self.db, scan)

        self.assertEqual([t.path for t in read.tracks], ["A/same.mp3"])
        self.assertEqual((read.changed, read.gone), (["A/retagged.mp3"], 2))


class ApplyAPlanLine(unittest.TestCase):
    """A plan line moves the Tracks both copies hold out of the copy marked quarantine."""

    def setUp(self) -> None:
        self.root, self.plans = tempfile.mkdtemp(), tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.root)
        self.addCleanup(shutil.rmtree, self.plans)
        self.tracks = []
        for folder, titles, name in ((CURATED, BENDS, "{i:02} - {t}.mp3"), (ITUNES, BENDS[:3] + ["Sulk"], "{i:02} {t}.mp3")):
            os.makedirs(os.path.join(self.root, folder))
            for i, t in enumerate(titles, 1):
                path = f"{folder}/{name.format(i=i, t=t)}"
                self.put(path, b"x" * (100 + i + len(folder)))
                self.tracks.append(track(path, t, size=os.path.getsize(os.path.join(self.root, path)), bit_rate=320 if folder == CURATED else 192))
        [self.line] = plan_albums(self.tracks).lines

    def put(self, rel: str, data: bytes) -> None:
        with open(os.path.join(self.root, rel), "wb") as fh:
            fh.write(data)

    def exists(self, rel: str) -> bool:
        return os.path.exists(os.path.join(self.root, rel))

    def run_plan(self, line: str) -> int:
        self.plan = os.path.join(self.plans, "heldtwice.tsv")
        with open(self.plan, "w", encoding="utf-8") as fh:
            fh.write(line + "\n")
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            return apply.main([self.root, self.plan])

    def test_moves_the_weaker_copy_of_each_track_both_hold_into_quarantine_keeping_its_path(self) -> None:
        self.assertEqual(self.run_plan(plan_line(self.line)), 0)

        for i, t in enumerate(BENDS[:3], 1):
            self.assertTrue(self.exists(f"_Quarantine/{ITUNES}/{i:02} {t}.mp3"))
            self.assertFalse(self.exists(f"{ITUNES}/{i:02} {t}.mp3"))
        self.assertTrue(self.exists(f"{ITUNES}/04 Sulk.mp3"))  # only this copy has it
        self.assertEqual(len(os.listdir(os.path.join(self.root, CURATED))), 5)
        self.assertTrue(self.exists("_Quarantine/.ndignore"))

    def test_the_owner_keeps_the_other_copy_by_swapping_the_two_marks(self) -> None:
        fields = plan_line(self.line).split("\t")
        fields[1], fields[3] = fields[3], fields[1]

        self.assertEqual(self.run_plan("\t".join(fields)), 0)

        self.assertTrue(self.exists(f"_Quarantine/{CURATED}/01 - Planet Telex.mp3"))
        self.assertTrue(self.exists(f"{CURATED}/04 - Fake Plastic Trees.mp3"))
        self.assertEqual(len(os.listdir(os.path.join(self.root, ITUNES))), 4)

    def test_moves_nothing_if_any_file_changed_or_went_since_planning(self) -> None:
        for change in (lambda: self.put(f"{ITUNES}/02 The Bends.mp3", b"retagged"),
                       lambda: os.remove(os.path.join(self.root, f"{CURATED}/03 - High and Dry.mp3"))):
            with self.subTest(change=change):
                change()
                self.assertEqual(self.run_plan(plan_line(self.line)), 1)
                self.assertFalse(self.exists("_Quarantine"))
                self.setUp()

    def test_resumes_an_album_stopped_part_way(self) -> None:
        os.makedirs(os.path.join(self.root, f"_Quarantine/{ITUNES}"))
        os.rename(os.path.join(self.root, f"{ITUNES}/01 Planet Telex.mp3"), os.path.join(self.root, f"_Quarantine/{ITUNES}/01 Planet Telex.mp3"))

        self.assertEqual(self.run_plan(plan_line(self.line)), 0)

        self.assertEqual(sorted(os.listdir(os.path.join(self.root, ITUNES))), ["04 Sulk.mp3"])

    def test_refuses_a_line_without_exactly_one_copy_to_keep(self) -> None:
        fields = plan_line(self.line).split("\t")
        fields[1] = "quarantine"

        self.assertEqual(self.run_plan("\t".join(fields)), 1)
        self.assertFalse(self.exists("_Quarantine"))


if __name__ == "__main__":
    unittest.main()
