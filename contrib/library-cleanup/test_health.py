# SPDX-License-Identifier: GPL-3.0-only
import contextlib
import io
import os
import sqlite3
import tempfile
import unittest

from health import main, open_read_only, report

# Just the columns the report reads, with the app's defaults.
SCHEMA = """
CREATE TABLE folder (id TEXT PRIMARY KEY, path TEXT NOT NULL DEFAULT '', name TEXT NOT NULL DEFAULT '',
    image_files TEXT NOT NULL DEFAULT '[]', missing BOOL NOT NULL DEFAULT 0);
CREATE TABLE album (id TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', album_artist TEXT NOT NULL DEFAULT '',
    embed_art_path TEXT NOT NULL DEFAULT '', folder_ids TEXT NOT NULL DEFAULT '[]', missing BOOL NOT NULL DEFAULT 0);
CREATE TABLE media_file (id TEXT PRIMARY KEY, path TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '',
    album TEXT NOT NULL DEFAULT '', artist TEXT NOT NULL DEFAULT '', album_artist TEXT NOT NULL DEFAULT '',
    album_id TEXT NOT NULL DEFAULT '', folder_id TEXT NOT NULL DEFAULT '', disc_number INTEGER NOT NULL DEFAULT 0,
    track_number INTEGER NOT NULL DEFAULT 0, year INTEGER NOT NULL DEFAULT 0,
    mbz_recording_id TEXT NOT NULL DEFAULT '', missing BOOL NOT NULL DEFAULT 0);
"""


class Library:
    """A small fixture database in the app's schema."""

    def __init__(self) -> None:
        self.db = sqlite3.connect(":memory:")
        self.db.executescript(SCHEMA)
        self._ids = 0

    def _id(self) -> str:
        self._ids += 1
        return str(self._ids)

    def folder(self, path: str, images: str = "[]", **cols: object) -> str:
        # The app stores a folder as its parent's path ("." at the top) and its own name.
        parent, _, name = path.rpartition("/")
        return self._insert("folder", path=parent or ".", name=name, image_files=images, **cols)

    def album(self, name: str, album_artist: str = "Radiohead", folders: tuple[str, ...] = (), **cols: object) -> str:
        ids = "[" + ",".join(f'"{f}"' for f in folders) + "]"
        return self._insert("album", name=name, album_artist=album_artist, folder_ids=ids, **cols)

    def track(self, path: str, **cols: object) -> str:
        """A Track tagged well enough to be healthy, unless cols says otherwise."""
        healthy = dict(title=path.rsplit("/", 1)[-1].rsplit(".", 1)[0], album="OK Computer", artist="Radiohead",
                       album_artist="Radiohead", track_number=1, year=1997, mbz_recording_id="mbid")
        return self._insert("media_file", **{**healthy, "path": path, **cols})

    def _insert(self, table: str, **cols: object) -> str:
        cols = {"id": self._id(), **cols}
        names = ", ".join(cols)
        self.db.execute(f"INSERT INTO {table} ({names}) VALUES ({', '.join('?' * len(cols))})", list(cols.values()))
        return str(cols["id"])


def counts(db: sqlite3.Connection) -> dict[str, int]:
    return {m.key: m.count for m in report(db)}


class Totals(unittest.TestCase):
    def test_counts_tracks_and_albums_the_app_still_has(self) -> None:
        lib = Library()
        lib.album("OK Computer")
        lib.album("Pablo Honey", missing=1)
        lib.track("Radiohead/OK Computer/Airbag.mp3")
        lib.track("Radiohead/OK Computer/Lucky.mp3")
        lib.track("Radiohead/Gone.mp3", missing=1)

        self.assertEqual(counts(lib.db)["tracks"], 2)
        self.assertEqual(counts(lib.db)["albums"], 1)


def offenders(db: sqlite3.Connection, key: str) -> list[tuple[object, ...]]:
    return list(next(m for m in report(db) if m.key == key).rows)


class MissingTags(unittest.TestCase):
    def setUp(self) -> None:
        self.lib = Library()
        self.lib.track("Radiohead/OK Computer/Airbag.mp3")
        self.lib.track("Radiohead/Gone.mp3", year=0, track_number=0, album="", artist="", mbz_recording_id="", missing=1)

    def test_counts_tracks_with_no_year_or_track_number(self) -> None:
        self.lib.track("a/No Year.mp3", year=0)
        self.lib.track("a/No Number.mp3", track_number=0)

        self.assertEqual(offenders(self.lib.db, "no-year"), [("a/No Year.mp3",)])
        self.assertEqual(offenders(self.lib.db, "no-track-number"), [("a/No Number.mp3",)])

    def test_a_blank_or_unknown_album_or_artist_counts_as_missing(self) -> None:
        # The app shows a missing tag as "[Unknown Album]" / "[Unknown Artist]".
        self.lib.track("a/1.mp3", album="")
        self.lib.track("a/2.mp3", album="[Unknown Album]")
        self.lib.track("a/3.mp3", album=" unknown ")
        self.lib.track("a/4.mp3", album="Superunknown")
        self.lib.track("b/1.mp3", artist="  ")
        self.lib.track("b/2.mp3", artist="[Unknown Artist]")
        self.lib.track("b/3.mp3", artist="Unknown")

        self.assertEqual(offenders(self.lib.db, "no-album"), [("a/1.mp3",), ("a/2.mp3",), ("a/3.mp3",)])
        self.assertEqual(offenders(self.lib.db, "no-artist"), [("b/1.mp3",), ("b/2.mp3",), ("b/3.mp3",)])

    def test_counts_tracks_with_no_musicbrainz_id(self) -> None:
        self.lib.track("a/Untagged.mp3", mbz_recording_id="")

        self.assertEqual(counts(self.lib.db)["no-musicbrainz-id"], 1)
        self.assertEqual(offenders(self.lib.db, "no-musicbrainz-id"), [("a/Untagged.mp3",)])


class NoCover(unittest.TestCase):
    def test_an_album_needs_embedded_art_or_an_image_in_one_of_its_folders(self) -> None:
        lib = Library()
        bare = lib.folder("Radiohead/Kid A")
        cover = lib.folder("Radiohead/Amnesiac", images='["cover.jpg"]')
        lib.album("Embedded", folders=(bare,), embed_art_path="Radiohead/Kid A/01.mp3")
        lib.album("Folder image", folders=(bare, cover))
        lib.album("Kid A", folders=(bare,))
        lib.album("Gone", folders=(bare,), missing=1)

        self.assertEqual(offenders(lib.db, "no-cover"), [("Radiohead", "Kid A", "Radiohead/Kid A")])


class LikelyDuplicates(unittest.TestCase):
    def setUp(self) -> None:
        self.lib = Library()
        self.lib.track("iTunes/Radiohead/OK Computer/01 Airbag.mp3", title="Airbag")
        self.lib.track("iTunes/Radiohead/OK Computer/01 Airbag 1.mp3", title="Airbag")
        self.lib.track("iTunes/Radiohead/OK Computer/01 Airbag (1).mp3", title="airbag ")
        self.lib.track("Radiohead/OK Computer/Copy of 01 Airbag.mp3", title="Airbag", artist="radiohead")
        self.lib.track("Radiohead/OK Computer/Airbag.flac", title="Airbag")
        self.lib.track("Radiohead/Gone/01 Airbag 2.mp3", title="Airbag", missing=1)

    def test_counts_the_extra_tracks_sharing_artist_title_and_album(self) -> None:
        # A live album's Airbag is a different Track, not a Duplicate.
        self.lib.track("Radiohead/Live/Airbag.mp3", title="Airbag", album="I Might Be Wrong")
        self.lib.track("Radiohead/OK Computer/Lucky.mp3", title="Lucky")

        dupes = next(m for m in report(self.lib.db) if m.key == "duplicates")

        self.assertEqual(dupes.count, 4)
        self.assertEqual(dupes.note, "in 1 groups")
        self.assertEqual(
            [row[-1] for row in dupes.rows],
            [
                "Radiohead/OK Computer/Airbag.flac",
                "Radiohead/OK Computer/Copy of 01 Airbag.mp3",
                "iTunes/Radiohead/OK Computer/01 Airbag (1).mp3",
                "iTunes/Radiohead/OK Computer/01 Airbag 1.mp3",
                "iTunes/Radiohead/OK Computer/01 Airbag.mp3",
            ],
        )

    def test_counts_the_copy_named_files_among_them(self) -> None:
        # Only a copy of a name that is in the group: "Airbag.flac" is no copy of "01 Airbag.mp3".
        self.assertEqual(
            offenders(self.lib.db, "copy-named"),
            [
                ("Radiohead/OK Computer/Copy of 01 Airbag.mp3",),
                ("iTunes/Radiohead/OK Computer/01 Airbag (1).mp3",),
                ("iTunes/Radiohead/OK Computer/01 Airbag 1.mp3",),
            ],
        )


class AlbumsHeldTwice(unittest.TestCase):
    def setUp(self) -> None:
        self.lib = Library()

    def tracks_in(self, folder: str, album: str, *discs: int, **cols: object) -> None:
        folder_id = self.lib.folder(folder)
        for n, disc in enumerate(discs or (0,), 1):
            self.lib.track(f"{folder}/{n}.mp3", album=album, folder_id=folder_id, disc_number=disc, **cols)

    def test_finds_the_same_album_and_album_artist_in_more_than_one_folder(self) -> None:
        self.tracks_in("Grateful Dead/American Beauty", "American Beauty", album_artist="Grateful Dead")
        self.tracks_in("iTunes/Grateful Dead/American Beauty", "American Beauty ", album_artist="grateful dead")
        self.tracks_in("Radiohead/OK Computer", "OK Computer")

        self.assertEqual(
            offenders(self.lib.db, "albums-twice"),
            [("Grateful Dead", "American Beauty", "Grateful Dead/American Beauty | iTunes/Grateful Dead/American Beauty")],
        )

    def test_the_same_album_name_by_another_album_artist_is_another_album(self) -> None:
        self.tracks_in("Radiohead/Greatest Hits", "Greatest Hits")
        self.tracks_in("Queen/Greatest Hits", "Greatest Hits", album_artist="Queen")

        self.assertEqual(counts(self.lib.db)["albums-twice"], 0)

    def test_a_multi_disc_album_with_one_folder_per_disc_is_held_once(self) -> None:
        self.tracks_in("Misfits/Box Set/CD1", "Box Set", 1, 1)
        self.tracks_in("Misfits/Box Set/CD2", "Box Set", 2, 2)

        self.assertEqual(counts(self.lib.db)["albums-twice"], 0)

    def test_ignores_tracks_with_no_album_and_tracks_the_app_lost(self) -> None:
        self.tracks_in("a", "[Unknown Album]")
        self.tracks_in("b", "[Unknown Album]")
        self.tracks_in("Radiohead/Kid A", "Kid A")
        self.tracks_in("Radiohead/Kid A (old)", "Kid A", missing=1)

        self.assertEqual(counts(self.lib.db)["albums-twice"], 0)


class SuspectDiscs(unittest.TestCase):
    def setUp(self) -> None:
        self.lib = Library()

    def album(self, name: str, discs: dict[int, int], **cols: object) -> None:
        album_id = self.lib.album(name)
        for disc, tracks in discs.items():
            for n in range(1, tracks + 1):
                self.lib.track(f"Radiohead/{name}/{disc}-{n}.mp3", album=name, album_id=album_id, disc_number=disc, **cols)

    def test_finds_a_disc_of_one_or_two_tracks_beside_tracks_with_no_disc_number(self) -> None:
        self.album("The Bends", {0: 11, 1: 1})
        self.album("Kid A", {0: 8, 2: 2})
        self.album("Amnesiac", {0: 8, 1: 3})

        self.assertEqual(
            offenders(self.lib.db, "suspect-discs"),
            [
                ("Radiohead", "Kid A", "no disc: 8, disc 2: 2", "Radiohead/Kid A/2-1.mp3 | Radiohead/Kid A/2-2.mp3"),
                ("Radiohead", "The Bends", "no disc: 11, disc 1: 1", "Radiohead/The Bends/1-1.mp3"),
            ],
        )

    def test_numbered_discs_alone_are_fine_however_short(self) -> None:
        self.album("OK Computer", {1: 12, 2: 1})
        self.album("Pablo Honey", {0: 12})
        self.album("Hail to the Thief", {0: 12, 1: 1}, missing=1)

        self.assertEqual(counts(self.lib.db)["suspect-discs"], 0)


class Command(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.path = os.path.join(tmp.name, "navidrome.db")
        lib = Library()
        lib.track("Radiohead/OK Computer/Airbag.mp3", year=0)
        lib.track("Radiohead/OK Computer/Lucky.mp3", year=0)
        disk = sqlite3.connect(self.path)
        lib.db.commit()
        lib.db.backup(disk)
        disk.close()

    def run_main(self, *args: str) -> tuple[int, str]:
        out = io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(io.StringIO()):
            code = main([self.path, *args])
        return code, out.getvalue()

    def test_never_writes_to_the_database(self) -> None:
        db = open_read_only(self.path)
        self.addCleanup(db.close)

        with self.assertRaises(sqlite3.OperationalError):
            db.execute("DELETE FROM media_file")

    def test_refuses_a_database_that_is_not_there_rather_than_creating_one(self) -> None:
        with self.assertRaises(sqlite3.OperationalError):
            open_read_only(self.path + ".typo")
        self.assertFalse(os.path.exists(self.path + ".typo"))

    def test_prints_every_number_with_the_key_that_lists_it(self) -> None:
        code, out = self.run_main()

        self.assertEqual(code, 0)
        self.assertIn("2\tTracks\ttracks\n", out)
        self.assertIn("2\tTracks with no year\tno-year\n", out)

    def test_lists_the_offenders_behind_a_number_as_tsv(self) -> None:
        code, out = self.run_main("--list", "no-year")

        self.assertEqual(code, 0)
        self.assertEqual(out, "path\nRadiohead/OK Computer/Airbag.mp3\nRadiohead/OK Computer/Lucky.mp3\n")

    def test_rejects_an_unknown_key(self) -> None:
        self.assertEqual(self.run_main("--list", "no-such-number")[0], 2)


if __name__ == "__main__":
    unittest.main()
