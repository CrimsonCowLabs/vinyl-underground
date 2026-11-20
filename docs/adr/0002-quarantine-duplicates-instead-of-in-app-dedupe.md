# Duplicates are quarantined on the drive, not hidden in the app

Navidrome has no dedupe: every File becomes its own Track, so byte-identical copies such as `Song 1.mp3` show up twice. We considered grouping Duplicates under one Track inside the app (by content hash). Instead, because Duplicates are byte-identical by definition, we move them out of the Library once, with a `jdupes` recipe, into a Quarantine folder that the scanner ignores (via `.ndignore`). The app keeps the simple rule that one Track is one File and carries no dedupe code.

## Consequences

- The Library is mounted read-only for the app. The cleanup is a separate, manual step run through a temporary writable mount, and is re-run if new Duplicates appear.
- Self-hosters get the same recipe in the README. A built-in `dedupe` command is possible later but is out of scope for now.
