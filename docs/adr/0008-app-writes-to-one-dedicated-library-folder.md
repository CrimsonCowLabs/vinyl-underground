# The app may write to one dedicated Library folder, and nowhere else in the Library

ADR-0002 made the Library read-only to the app. That was enough while the Library was a fixed collection. But the Library keeps growing: new music arrives through the Inbox (Rips, uploads and Files dropped in) and has to reach the NAS. Moving it there by hand with a script keeps the Library safe, but in practice music piles up waiting. Making the whole share writable would let a bug in the app damage ~23k existing Files, and would let it create new folders whose names differ only in letter case, which is the problem #2 is fixing on this case-insensitive SMB1 NAS.

We chose to give the app one dedicated folder (`Vinyl Underground/`) in the music share, mounted read-write on its own (CIFS can mount a subfolder). The rest of the share stays mounted read-only. **Promote** moves reviewed Files from the Inbox into that folder, and the app writes nowhere else in the Library.

## Consequences

- ADR-0002 still holds for everything outside `Vinyl Underground/`. The Quarantine cleanup is still manual.
- Promote uses `Vinyl Underground/<Album Artist>/<Album> (<Year>)/<NN> <Title>.<ext>`. Before creating a folder, it looks for an existing one whose name differs only in letter case and reuses it.
- Promoting a File updates the existing Track in place (its path and library) instead of creating a new Track, so favorites, Playlist entries and play counts carry over.
- Promote ships only after the case-name merge in #2. Until then, the Inbox is where new music lives.
- The deployment needs a second mount and a second bind mount. The README must cover both, and the feature stays off for self-hosters who don't configure the folder.
