# Duplicates are quarantined on the drive, not hidden in the app

Navidrome has no dedupe: every File becomes its own Track, so byte-identical copies such as `Song 1.mp3` show up twice. We considered grouping Duplicates under one Track inside the app (by content hash). Instead, because Duplicates are byte-identical by definition, we move them out of the Library once, with a `jdupes` recipe, into a Quarantine folder that the scanner ignores (via `.ndignore`). The app keeps the simple rule that one Track is one File and carries no dedupe code.

## Consequences

- The Library is mounted read-only for the app. The cleanup is a separate, manual step run through a temporary writable mount, and is re-run if new Duplicates appear.
- Self-hosters get the same recipe in the README. A built-in `dedupe` command is possible later but is out of scope for now.
- Changed (#53): a Duplicate is a File whose *audio* is identical to another File's, not only its bytes. A music app copying a File often rewrites the new File's tags (about 2,100 iTunes `… 1.mp3` Files survived the `jdupes` pass that way), and the app showed each such Track twice. A second pass compares the audio data alone, ignoring tags and embedded art, and quarantines through the same reviewed plan; the decision itself is unchanged: Duplicates leave the drive rather than being grouped in the app.
- Extended (#54): an album held twice (two copies in different folders whose audio differs) is not a Duplicate, but the songs the weaker copy shares with the other leave the Library the same way, through a reviewed plan into Quarantine, so the app still needs no code to group or hide Tracks.
