# Gapless playback and crossfade are deferred

The UI refresh asked whether to add gapless playback and crossfade alongside the equalizer and visuals. Both need two audio elements: the next Track is loaded into a second element ahead of time, and playback hands over from one to the other (at the exact end of the Track for gapless, overlapping and fading for crossfade). The web player is `navidrome-music-player` 4.25.4, Navidrome's fork of react-jinke-music-player, and it is built around one audio element: it owns that element's source, the queue, the play modes and the mobile and desktop panels. It cannot hand over to a second element.

Doing either means replacing the player engine with our own (or another package) and moving the queue, Stations' self-refilling queue, Downloads playback, the Media Session and ReplayGain onto it. That is the kind of rewrite ADR-0003 avoids: we extend the inherited UI and restyle what is poor, rather than rebuild what already works. We chose to defer both, and to build the rest of the UI refresh (Now Playing, the equalizer, visuals, the colour theme, a sleep timer and playback speed) on the existing one-element player.

## Consequences

- There is a short gap between Tracks, as today, and no crossfade. Live and continuous albums show the gap most.
- The shared Web Audio chain (ADR-0010) is built on the one element. A two-element engine would need a source per element, mixed into the same chain. Keeping element handling inside the chain's module keeps that change in one place.
- Revisit this only together with a decision to replace the player engine; that decision would revise ADR-0003 for the player.
