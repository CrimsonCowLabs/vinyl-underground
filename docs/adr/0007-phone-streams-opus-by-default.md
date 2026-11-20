# The web app on a phone streams Opus by default, detected from the User-Agent

Phones should stream Opus at ~160 kbps to save data, and desktops the original File, with no setup. Navidrome already lets an admin assign a Transcoding to each player, but that needs a manual step per device. We chose to compute a default instead (`core/streamquality`): when a request comes from the web app (Subsonic client `NavidromeUI`) and its player has no Transcoding of its own, an Android or iOS User-Agent gets the Opus Transcoding at 160 kbps, and anything else gets none. The result goes into the request context exactly where a player's own Transcoding would, so the existing transcode decision code does the rest. A per-Listener setting (Automatic, Always original, Always Opus 160 kbps) overrides the device default and is stored in user props.

We considered detecting phones from screen size or a flag sent by the UI, but the User-Agent is already parsed for players and needs no UI change. We also considered applying the default to every Subsonic client, but native clients have their own quality settings, their User-Agent rarely names the OS, and some (on iOS) cannot play Opus at all.

## Consequences

- Precedence, highest first: a Transcoding set on the player, the Listener's setting, the device default.
- Native Subsonic and Jellyfin clients keep Navidrome's behaviour. A Listener's setting does not affect them.
- iPadOS Safari reports itself as macOS, so iPads count as desktops.
- Safari cannot stream transcoded Ogg Opus, so an iPhone gets MP3 at 160 kbps instead. The default caps the in-context player's bitrate to make that happen; a Transcoding set on the player does not, as upstream.
- If an admin deletes the Opus Transcoding, phones fall back to the original File.
