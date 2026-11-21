# Vinyl Underground

Vinyl Underground is a modified fork of [Navidrome](https://github.com/navidrome/navidrome) v0.64.2 (© Deluan Quintão and contributors, GPL-3.0). Forked on 2026-11-20 and modified since; see git history.

A self-hosted music server for your own Library, with offline Downloads on your phone, AI-generated Playlists from a local LLM, and Stations hosted by DJ personas.

**Status: design stage.** Nothing is built yet. The plan is in [`docs/design.md`](docs/design.md), the vocabulary in [`GLOSSARY.md`](GLOSSARY.md), the key decisions in [`docs/adr/`](docs/adr/), and the work is tracked in this repo's issues.

Vinyl Underground will be a modified hard fork of [Navidrome](https://github.com/navidrome/navidrome) v0.64.2, licensed under GPL-3.0. It is intended for private networks only (e.g. a tailnet), not the public internet.

To run it on a tailnet with `docker compose` and `tailscale serve`, see [`deploy/README.md`](deploy/README.md).

## Last.fm and other outside lookups

Outside metadata lookups are on, as in Navidrome: the app fetches artist info, album info and similar artists from Last.fm (with `ND_LASTFM_APIKEY` set), Deezer and ListenBrainz. **This sends artist and album names from your Library to Last.fm** (and to those services). Generated Playlists use the similar artists to find more Tracks in the Library when its genre tags are sparse; if Last.fm can't be reached, they are built from the Library's own tags alone. Set `ND_ENABLEEXTERNALSERVICES=false` to turn all outside lookups off.

Scrobbling stays off: the app never sends what you play to Last.fm, and it doesn't offer to link a Last.fm account. Details are in [`deploy/README.md`](deploy/README.md#similar-artists-from-lastfm).

## Ripping

Ripping fetches the audio of a web page (usually a YouTube or YouTube Music video) into the Inbox with [yt-dlp](https://github.com/yt-dlp/yt-dlp). It is **off by default**. To turn it on, set `ND_RIPPING_ENABLED=true` (in `deploy/.env` for the Docker stack) and restart. The Inbox must be set up too (`ND_INBOX_PATH`, `/inbox` in the image).

Then the admin gets a **Rips** page: paste the link to a video, a playlist or an album and press **Rip**. The Rip runs in the background, one at a time, and keeps going if you close the tab. When it is ready for review, its Tracks are in the Inbox. Details are in [`deploy/README.md`](deploy/README.md#10-rip-a-video-a-playlist-or-an-album-into-the-inbox).

Uploads and Files dropped straight into the Inbox folder get the same review cards, whether ripping is on or off: the **Rips** page shows whenever the Inbox is set up, with an **Upload Files** button (mp3, m4a, opus, ogg, flac, aiff or wav, up to 500 MB each). Details are in [`deploy/README.md`](deploy/README.md#review-cards-for-uploaded-and-dropped-files).

**You are responsible for what you download.** Only rip what you have the right to copy, and check the terms of the site you rip from and the law where you live. Vinyl Underground and its authors don't check what you rip and accept no responsibility for it.
