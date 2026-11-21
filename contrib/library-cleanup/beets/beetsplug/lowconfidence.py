# SPDX-License-Identifier: GPL-3.0-only
"""Skip albums beets can't match with confidence, before the owner is asked.

beets asks about every album in timid mode, including those with no good candidate. This
plugin skips those instead (beets logs them as "skip" in the import log, for a later review),
so the owner is only asked to approve matches worth approving.

    lowconfidence:
        skip: yes      # no to ask about every album, e.g. when reviewing the skipped ones
"""
from __future__ import annotations

from beets import ui
from beets.autotag.match import Recommendation
from beets.importer import Action
from beets.plugins import BeetsPlugin

# beets' recommendation below which a match counts as low-confidence: "medium" means the best
# candidate is less than 75% similar (match.medium_rec_thresh), or there is none.
ASK_FROM = Recommendation.medium


class LowConfidencePlugin(BeetsPlugin):
    def __init__(self) -> None:
        super().__init__()
        self.config.add({"skip": True})
        self.register_listener("import_task_before_choice", self.before_choice)

    def before_choice(self, session, task) -> Action | None:
        if not self.config["skip"].get(bool) or task.rec >= ASK_FROM:
            return None
        if task.candidates:
            best = task.candidates[0]
            similarity = (1 - float(best.distance)) * 100
            ui.print_(f"Low confidence: best match {best.info.artist} - {best.info.album} ({similarity:.0f}%). Skipped for review.")
        else:
            ui.print_("No match found. Skipped for review.")
        return Action.SKIP
