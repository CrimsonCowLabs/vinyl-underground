# Vinyl Underground: design

The design agreed in the initial planning session (2026-11-20). Vocabulary is defined in [`GLOSSARY.md`](../GLOSSARY.md). The reasoning behind key decisions is in [`docs/adr/`](./adr/).

## What it is

A self-hosted music server for a small household. Listeners stream their own Library from any device on their private network (e.g. a tailnet), keep Downloads for offline playback on their phones, get Generated Playlists from a local LLM, and listen to Stations hosted by DJ personas.

It is a public, GPL-3.0, hard fork of Navidrome **v0.64.2**, renamed and never rebased ([ADR-0001](./adr/0001-hard-fork-navidrome.md)). It is intended for private networks only and is not hardened for the public internet.

## Features

### Inherited from Navidrome (kept as-is)
- Library scanning, browsing and search.
- Listeners and logins. Per-Listener favorites, ratings, play counts and history.
- Streaming and transcoding (ffmpeg).
- Playlists: owned by one Listener, private by default, and read-only when shared. A Listener can copy a shared Playlist to make their own.

### Streaming quality ([ADR-0007](./adr/0007-phone-streams-opus-by-default.md))
- Phone: always Opus at ~160 kbps. Desktop: the original File.
- A per-Listener override in settings.
- Applies to the web app only. A phone is an Android or iOS User-Agent. Safari on an iPhone gets MP3 at ~160 kbps, as it cannot stream transcoded Opus.

### Downloads ([ADR-0006](./adr/0006-offline-downloads-in-the-pwa.md))
- Playlists and albums can be downloaded to the device as Opus at ~160 kbps.
- A downloaded Playlist is brought up to date automatically when online: when the app starts, comes back online or is brought back to the screen. It is not refreshed while the app is closed.
- The app requests persistent storage and shows a storage meter. There is no hard cap.
- Offline: play Downloads and shuffle them. Stations are unavailable.
- Lock-screen and background playback use the Media Session API.

### Generated Playlists ([ADR-0004](./adr/0004-llm-ranks-library-candidates-never-invents-tracks.md))
- The Listener enters a Prompt and a length (default 25, range 10–100).
- The LLM turns the Prompt into filters and seed artists. The server builds candidates from the Library, and the LLM ranks or trims them.
- The result is shown as a preview with a **Save** button. Once saved, it's an ordinary Playlist that remembers its Prompt, so the Listener can regenerate it.

### Stations ([ADR-0005](./adr/0005-stations-are-personal-not-live-broadcast.md))
- Personal, endless and skippable. The Listener starts a Station with a DJ and an optional Theme.
- Selection uses the DJ's taste and the Theme, biased toward the Listener's favorites and history.
- It avoids Tracks played in the last ~48h and avoids repeating an artist within ~5 Tracks.
- Tracks and Blurbs are pre-generated 2–3 Tracks ahead.
- A skip is a mild "less like this" signal for that Station only, fading over ~30 days.
- Station plays count in play history once ~50% of the Track has played.
- Later (not now): start a Station from a Track, artist or Playlist.

### DJs
- Built-in DJs: **Night Owl** (low-key, late-night, short Blurbs), **The Crate Digger** (deep cuts and trivia), **Sunny** (upbeat, morning-show).
- Listeners can create their own DJs, which are private to them, from a name plus a personality description.
- DJs never speak aloud. Their character comes through Track selection and on-screen Blurbs.

### Inbox and Rips (designed 2026-11-20)
The Library keeps growing, and all new music reaches it through one path: the **Inbox**.

**Scope and access**
- Ripping is off by default (`ND_RIPPING_ENABLED=false`).
- The README states that users are responsible for what they download.
- Only the admin can rip, upload, review and Promote.

**Getting music into the Inbox**
- The Inbox is a host folder, set by `VU_INBOX_DIR` (default `./inbox`), mounted read-write at `/inbox`. It is kept separate from `/data`.
- The app scans it as a second library, and only the admin has access to it. Inbox Tracks can be played, favorited and added to Playlists straight away.
- Music arrives in three ways:
  - **Rip:** paste a URL for one video, a playlist or an album. Any site yt-dlp supports is accepted, but only YouTube and YouTube Music are tested. The UI suggests `music.youtube.com` album links.
  - **Upload:** several Files at once through the web app. Accepted formats are mp3, m4a, opus, ogg, flac, aiff and wav, up to 500 MB per File.
  - **Drop:** Files copied straight into the Inbox folder are picked up by the scan.

**Ripping**
- Rips keep the best original audio stream, remuxed to `.opus` or `.m4a` and never re-encoded.
- Each Rip gets a `SOURCE` tag naming the site and a tag holding the source URL. The UI shows a "Ripped" badge, driven by those tags, so it survives promotion and a database rebuild.
- A playlist of more than 50 items asks for confirmation. More than 300 is refused.
- Each Rip is a background job, run one at a time. A job's status is queued, downloading, tagging, ready for review, or failed (showing yt-dlp's error). Jobs keep running after the tab is closed.
- yt-dlp lives in `/data/bin`, because the container is read-only. The app downloads it, updates it on a schedule and from an "Update yt-dlp" button, and after a failed Rip it updates yt-dlp once and retries.
- Before ripping, the app matches artist and title against the Library and warns "already have: …". It never blocks the Rip.

**Tagging and review**
- Tag proposals start from YouTube Music's own metadata (from auto-generated "Topic" uploads) when it exists. Otherwise they come from a cleaned-up video title.
- MusicBrainz then sets the album and year. It picks the earliest official studio album by the main artist, else the single, never a compilation, live or remix release. Uploads get the same proposal.
- Review is one card per job, with editable album tags and an editable Track list. It offers **Promote all** and **Discard**, and single Tracks can also be discarded.
- Discard deletes the File permanently. Quarantine stays reserved for Duplicates.

**Promote** (Phase 2, after the case-name merge in #2; [ADR-0008](./adr/0008-app-writes-to-one-dedicated-library-folder.md))
- Promote moves Files to `Vinyl Underground/<Album Artist>/<Album> (<Year>)/<NN> <Title>.<ext>`, a NAS folder with its own read-write mount.
- Before creating a folder, the app looks for an existing one whose name differs only in letter case and reuses it, because the NAS is case-insensitive.
- The Track keeps its identity. Its path and library change in place, so favorites, Playlist entries and play counts carry over.

**Phases**
1. The Inbox, Rips, uploads, the drop folder, tagging and review.
2. Promote and the dedicated read-write folder.

**First use:** two batches go into the Inbox as soon as Phase 1 works:
- The 68 Files already tagged for the 2026-11 intake.
- The 73 YouTube-quality Files excluded then, marked as Rips.

## Architecture

- **One codebase.** All new backend features (Generated Playlists, Stations, DJs, Blurbs) live in the Go server: new tables through Navidrome's migrations, and new endpoints next to its native `/api`, reusing its auth.
- **UI.** Navidrome's existing React 17 / react-admin 3 / MUI v4 UI is extended, not replaced ([ADR-0003](./adr/0003-extend-legacy-navidrome-ui.md)). The service worker gains audio caching.
- **LLM.** Any OpenAI-compatible endpoint, set by a URL and model name. With no LLM configured or reachable, AI features are hidden and Stations run with rule-based selection and no Blurbs.
  - **Model:** a ~4B instruct model (e.g. Qwen3.5 4B or Gemma 4 E4B), chosen by timing a real Prompt on the actual Library.
  - **Output:** always constrained by a JSON schema (Ollama's `format` option). Prompting for JSON alone is not enough at this model size. Use a GGUF build of the model (e.g. `qwen3.5:4b-q4_K_M`); Ollama's MLX builds return "structured output is unavailable" when given a schema (seen on Ollama 0.40.16).
  - **Measured** (Qwen3.5 4B q4_K_M on an 8 GB A18 Pro Mac): ranking 150 candidates takes ~33 s, at ~145 words/s prompt reading and ~16 words/s output.
  - **Thinking:** turn off any "thinking" mode.
  - **Prompt size:** keep candidate lists to ~100–150 Tracks, one compact line each (artist, title, year, genre), so prompts stay short.
  - **Timeouts:** allow for slow LLM calls.
- **External metadata.** Last.fm lookups are enabled (`EnableExternalServices`). Scrobbling stays off.
- **Duplicates.** A one-time `jdupes` recipe moves Duplicates to a Quarantine folder that carries a `.ndignore` file. The app has no dedupe code ([ADR-0002](./adr/0002-quarantine-duplicates-instead-of-in-app-dedupe.md)).

## Deployment

- A `docker compose` stack running the Vinyl Underground image. An `ollama` service is optional (a compose profile) for self-hosters who run the LLM on the same machine.
- The reference deployment runs Ollama on a separate, always-on Apple Silicon Mac on the tailnet, which reads long prompts much faster than a CPU-only host. Ollama must listen on the tailnet interface, not just localhost.
- The music share is mounted on the host and bind-mounted **read-only** into the container. The one exception is the `Vinyl Underground/` folder, which has its own read-write mount for Promote ([ADR-0008](./adr/0008-app-writes-to-one-dedicated-library-folder.md)).
- The Inbox (`VU_INBOX_DIR`) is a separate read-write bind mount at `/inbox`.
- HTTPS through `tailscale serve`, which the PWA needs for its service worker.
- Personal details (hostnames, IPs, credentials) live in a gitignored `.env`. The repo ships `.env.example` and an example `docker-compose.yml`.
- Development needs Go 1.27 (cgo enabled, for sqlite), Node 24 and ffmpeg.

## Open source obligations

- Keep upstream's `LICENSE` (GPL-3.0) and every existing copyright notice. New code is GPL-3.0.
- Add a README notice: *"Vinyl Underground is a modified fork of Navidrome v0.64.2 (© Deluan Quintão and contributors, GPL-3.0). Forked on <fork date> and modified since; see git history."*
- Replace Navidrome's name, logo and PWA manifest branding. Never imply endorsement.
- Keep upstream git history up to v0.64.2.
- Publish as a standalone public GitHub repo `vinyl-underground` rather than a GitHub "fork". Delete upstream's CI and add a workflow that publishes a multi-arch (amd64 + arm64) image to GHCR on each tag.

## README must cover

- A private-network-only warning, with no upstream security fixes (ADR-0001).
- Mounting a music share read-only, including the SMB1 (`vers=1.0`) note for older NAS devices.
- The `jdupes` Quarantine recipe, dry run first.
- Configuring an LLM endpoint, and what happens without one.
- Tailscale HTTPS setup.
- The Navidrome credit and license notice.
- Enabling ripping, and that users are responsible for what they download.
- Mounting the read-write `Vinyl Underground/` subfolder for Promote.

## Build order

The goal is to finish everything in one weekend. If time runs out, items drop from the bottom.

1. Mount the NAS, run the Quarantine cleanup, fork and rename at v0.64.2, run it over Tailscale HTTPS, and create both Listener logins.
2. Downloads.
3. Generated Playlists.
4. Stations, DJs and Blurbs.
5. GHCR publishing workflow and README.

## Repo setup note

The repo (`CrimsonCowLabs/vinyl-underground`) starts with these design docs only. When forking, bring in upstream history by fetching Navidrome's `v0.64.2` tag and merging it with `--allow-unrelated-histories`. That keeps every upstream commit reachable and marks the merge commit as the fork point. All commits are GPG-signed.
