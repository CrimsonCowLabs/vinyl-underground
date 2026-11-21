# Vinyl Underground

Vinyl Underground is a modified fork of [Navidrome](https://github.com/navidrome/navidrome) v0.64.2 (© Deluan Quintão and contributors, GPL-3.0). Forked on 2026-11-20 and modified since; see git history.

A self-hosted music server for your own Library, with offline Downloads on your phone, AI-generated Playlists from a local LLM, and Stations hosted by DJ personas.

**Status: design stage.** Nothing is built yet. The plan is in [`docs/design.md`](docs/design.md), the vocabulary in [`GLOSSARY.md`](GLOSSARY.md), the key decisions in [`docs/adr/`](docs/adr/), and the work is tracked in this repo's issues.

Vinyl Underground will be a modified hard fork of [Navidrome](https://github.com/navidrome/navidrome) v0.64.2, licensed under GPL-3.0. It is intended for private networks only (e.g. a tailnet), not the public internet.

To run it on a tailnet with `docker compose` and `tailscale serve`, see [`deploy/README.md`](deploy/README.md).

## Ripping

Ripping fetches the audio of a web page (usually a YouTube or YouTube Music video) into the Inbox with [yt-dlp](https://github.com/yt-dlp/yt-dlp). It is **off by default**. To turn it on, set `ND_RIPPING_ENABLED=true` (in `deploy/.env` for the Docker stack) and restart. The Inbox must be set up too (`ND_INBOX_PATH`, `/inbox` in the image).

Then the admin gets a **Rips** page: paste the link to one video and press **Rip**. The Rip runs in the background, one at a time, and keeps going if you close the tab. When it is ready for review, it is a Track in the Inbox. Details are in [`deploy/README.md`](deploy/README.md#10-rip-a-video-into-the-inbox).

**You are responsible for what you download.** Only rip what you have the right to copy, and check the terms of the site you rip from and the law where you live. Vinyl Underground and its authors don't check what you rip and accept no responsibility for it.
