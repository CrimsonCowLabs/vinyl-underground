# SPDX-License-Identifier: GPL-3.0-only
"""Let the chroma plugin's audio fingerprints count when beets pairs Files with Tracks.

chroma looks up each File's fingerprint on AcoustID and offers the releases it finds, and its
track_distance() penalizes pairing a File with a Track whose recording the fingerprint didn't
match. beets 2.14.1 never calls a plugin's track_distance(), though, so for Files with no titles
it pairs Files with Tracks by length alone, and swaps Tracks of similar length (on OK Computer,
"Airbag" and "Climbing Up the Walls"). This plugin adds chroma's penalty to beets' own
track_distance(), which pairs Files with Tracks and scores album matches. Drop it once beets
calls plugin track distances again (check with `retag.sh test` after changing the pinned beets).
"""
from __future__ import annotations

import importlib

from beets.autotag import match
from beets.plugins import BeetsPlugin
from beetsplug import chroma

# The module, which beets.autotag hides behind its distance() function.
_distance = importlib.import_module("beets.autotag.distance")
_core_track_distance = _distance.track_distance


def track_distance(item, track_info, incl_artist: bool = False):
    """beets' track distance plus chroma's: a File paired with a recording its fingerprint didn't match is penalized."""
    dist = _core_track_distance(item, track_info, incl_artist)
    if item.path in chroma._matches and track_info.track_id:
        recording_ids, _ = chroma._matches[item.path]
        dist.add_expr("track_id", track_info.track_id not in recording_ids)
    return dist


class FingerprintsPlugin(BeetsPlugin):
    def __init__(self) -> None:
        super().__init__()
        # match imported the function by name, so both references are replaced.
        _distance.track_distance = track_distance
        match.track_distance = track_distance
