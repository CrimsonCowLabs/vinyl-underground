# SPDX-License-Identifier: GPL-3.0-only
"""Tests for the lowconfidence plugin. They need beets, so run them in the image: retag.sh test."""
import unittest
from types import SimpleNamespace

from beets import config
from beets.autotag.match import Recommendation
from beets.importer import Action

from beetsplug.lowconfidence import LowConfidencePlugin


def task(rec: Recommendation, distance: float = 0.5) -> SimpleNamespace:
    best = SimpleNamespace(distance=distance, info=SimpleNamespace(artist="Radiohead", album="The Bends"))
    return SimpleNamespace(rec=rec, candidates=[best] if rec is not Recommendation.none else [], paths=[b"/music/The Bends"])


class LowConfidence(unittest.TestCase):
    def setUp(self) -> None:
        config.clear()
        config.read(user=False, defaults=True)
        self.plugin = LowConfidencePlugin()

    def choice(self, t: SimpleNamespace) -> object:
        return self.plugin.before_choice(session=None, task=t)

    def test_skips_an_album_with_no_match_or_only_a_low_confidence_one(self) -> None:
        self.assertIs(self.choice(task(Recommendation.none)), Action.SKIP)
        self.assertIs(self.choice(task(Recommendation.low)), Action.SKIP)

    def test_leaves_medium_and_strong_matches_to_the_owner(self) -> None:
        self.assertIsNone(self.choice(task(Recommendation.medium, 0.2)))
        self.assertIsNone(self.choice(task(Recommendation.strong, 0.01)))

    def test_can_be_turned_off_to_review_the_skipped_albums(self) -> None:
        config["lowconfidence"]["skip"] = False

        self.assertIsNone(self.choice(task(Recommendation.none)))


if __name__ == "__main__":
    unittest.main()
