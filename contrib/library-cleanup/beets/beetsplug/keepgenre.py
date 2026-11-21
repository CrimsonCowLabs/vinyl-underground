# SPDX-License-Identifier: GPL-3.0-only
"""Keep a File's own genre when lastgenre finds none for its album.

lastgenre replaces the genre each File had with Last.fm's, and clears it for an album Last.fm
knows nothing about. Its keep_existing setting can't help: it would put the File's own genre
first on every album, so iTunes' broad "Rock" would always win. This plugin remembers each
File's genres when the import reads it, and puts them back on any File the import left without
a genre. List it after lastgenre in `plugins`, so its stage runs after lastgenre's.
"""
from __future__ import annotations

from collections import Counter

from beets.plugins import BeetsPlugin


class KeepGenrePlugin(BeetsPlugin):
    def __init__(self) -> None:
        super().__init__()
        self.original: dict[bytes, list[str]] = {}  # File path -> its genres before the import
        self.register_listener("import_task_created", self.remember)
        self.import_stages = [self.restore]

    def remember(self, session, task) -> None:
        for item in task.items:
            if genres := list(item.get("genres", with_album=False) or []):
                self.original[item.path] = genres

    def restore(self, session, task) -> None:
        restored = []
        for item in task.imported_items():
            genres = self.original.pop(item.path, None)
            if genres and not item.get("genres", with_album=False):
                item.genres = genres
                item.store()
                restored.append(genres)
        album = task.album if task.is_album else None
        if album is not None and restored and not album.genres:
            # The album shows the genre most of its Files had; each File keeps its own.
            album.genres = list(Counter(map(tuple, restored)).most_common(1)[0][0])
            album.store(inherit=False)
