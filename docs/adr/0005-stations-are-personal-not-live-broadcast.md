# Stations are personal and skippable, not a shared live broadcast

"Radio" could have meant a shared live stream on a server clock (everyone hears the same moment, no skipping, Icecast-style). We chose personal Stations instead: each Listener's Station is an endless, skippable queue built for them, refilled by the server as it plays. The radio feel comes from the DJ persona and on-screen Blurbs, not from a shared timeline or spoken voice.

## Consequences

- There is no Icecast or server-side audio mixing. Stations reuse normal Track streaming and the player queue.
- Skips are a mild negative signal, limited to that Station and fading over about 30 days. They never change ratings or favorites.
- Station plays count in normal play history once about 50% of the Track has played.
- Stations need the server, so they are online only.
