# SPDX-License-Identifier: GPL-3.0-only
"""Tests for the fingerprints plugin. They need beets, so run them in the image: retag.sh test."""
import importlib
import unittest

from beets import config
from beets.autotag import match
from beets.autotag.hooks import TrackInfo
from beets.library import Item
from beetsplug import chroma

from beetsplug.fingerprints import FingerprintsPlugin

# The module, which beets.autotag hides behind its distance() function.
DISTANCE = importlib.import_module("beets.autotag.distance")


class Fingerprints(unittest.TestCase):
    def setUp(self) -> None:
        config.clear()
        config.read(user=False, defaults=True)
        FingerprintsPlugin()
        chroma._matches.clear()
        self.addCleanup(chroma._matches.clear)

    def test_pairs_untitled_files_with_tracks_by_their_fingerprints(self) -> None:
        # Two Files with no tags, whose lengths alone would pair each with the other's Track.
        airbag = Item(path=b"/music/OK Computer/Airbag.mp3", title="", length=200.0)
        walls = Item(path=b"/music/OK Computer/Climbing up the Walls.mp3", title="", length=300.0)
        tracks = [TrackInfo(title="Airbag", track_id="rec-airbag", index=1, length=300.0),
                  TrackInfo(title="Climbing Up the Walls", track_id="rec-walls", index=2, length=200.0)]
        chroma._matches[airbag.path] = (["rec-airbag"], [])
        chroma._matches[walls.path] = (["rec-walls", "rec-walls-live"], [])

        mapping, extra_items, extra_tracks = match.assign_items([airbag, walls], tracks)

        self.assertEqual({i.path: t.title for i, t in mapping},
                         {airbag.path: "Airbag", walls.path: "Climbing Up the Walls"})
        self.assertEqual((extra_items, extra_tracks), ([], []))

    def test_counts_a_fingerprint_mismatch_in_the_album_distance(self) -> None:
        item = Item(path=b"/music/OK Computer/Airbag.mp3", title="Airbag", length=284.0)
        track = TrackInfo(title="Airbag", track_id="rec-other", index=1, length=284.0)
        chroma._matches[item.path] = (["rec-airbag"], [])

        self.assertGreater(float(DISTANCE.track_distance(item, track)), 0)


if __name__ == "__main__":
    unittest.main()
