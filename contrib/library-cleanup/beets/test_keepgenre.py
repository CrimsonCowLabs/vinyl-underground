# SPDX-License-Identifier: GPL-3.0-only
"""Tests for the keepgenre plugin. They need beets, so run them in the image: retag.sh test."""
import os
import tempfile
import unittest
from types import SimpleNamespace

from beets import config
from beets.library import Item, Library

from beetsplug.keepgenre import KeepGenrePlugin


class KeepGenre(unittest.TestCase):
    def setUp(self) -> None:
        config.clear()
        config.read(user=False, defaults=True)
        self.plugin = KeepGenrePlugin()
        folder = tempfile.TemporaryDirectory()
        self.addCleanup(folder.cleanup)
        self.lib = Library(os.path.join(folder.name, "library.db"))

    def files(self, *genres: list[str]) -> list[Item]:
        return [Item(path=f"/music/Album/{n:02} Track.mp3".encode(), title=f"Track {n}", album="Album",
                     genres=g) for n, g in enumerate(genres, 1)]

    def imported(self, items: list[Item], album_genres: list[str], item_genres: list[str]) -> SimpleNamespace:
        """Reads `items` as the import does, then stores them as one album after lastgenre ran."""
        task = SimpleNamespace(items=items, is_album=True)
        self.plugin.remember(session=None, task=task)
        for item in items:
            item.genres = item_genres
        task.album = self.lib.add_album(items)
        task.album.genres = album_genres
        task.album.store(inherit=False)
        task.imported_items = lambda: list(task.album.items())
        self.plugin.restore(session=None, task=task)
        return task

    def test_puts_each_files_genre_back_when_lastgenre_found_none(self) -> None:
        task = self.imported(self.files(["Rock"], ["Rock"], ["Pop"]), album_genres=[], item_genres=[])

        self.assertEqual([i.genres for i in task.album.items()], [["Rock"], ["Rock"], ["Pop"]])
        self.assertEqual(task.album.genres, ["Rock"])

    def test_leaves_the_genre_lastgenre_found(self) -> None:
        task = self.imported(self.files(["Rock"], ["Rock"]),
                             album_genres=["Alternative Rock"], item_genres=["Alternative Rock"])

        self.assertEqual([i.genres for i in task.album.items()], [["Alternative Rock"]] * 2)
        self.assertEqual(task.album.genres, ["Alternative Rock"])

    def test_a_file_that_had_no_genre_stays_without_one(self) -> None:
        task = self.imported(self.files([], []), album_genres=[], item_genres=[])

        self.assertEqual([i.genres for i in task.album.items()], [[], []])
        self.assertEqual(task.album.genres, [])


if __name__ == "__main__":
    unittest.main()
