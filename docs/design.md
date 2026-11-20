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

### Streaming quality
- Phone: always Opus at ~160 kbps. Desktop: the original File.
- A per-Listener override in settings.

### Downloads ([ADR-0006](./adr/0006-offline-downloads-in-the-pwa.md))
- Playlists and albums can be downloaded to the device as Opus at ~160 kbps.
- A downloaded Playlist syncs automatically when online.
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

## Architecture

- **One codebase.** All new backend features (Generated Playlists, Stations, DJs, Blurbs) live in the Go server: new tables through Navidrome's migrations, and new endpoints next to its native `/api`, reusing its auth.
- **UI.** Navidrome's existing React 17 / react-admin 3 / MUI v4 UI is extended, not replaced ([ADR-0003](./adr/0003-extend-legacy-navidrome-ui.md)). The service worker gains audio caching.
- **LLM.** Any OpenAI-compatible endpoint, set by a URL and model name. The reference deployment uses Ollama. With no LLM configured, AI features are hidden and Stations run with rule-based selection and no Blurbs.
- **External metadata.** Last.fm lookups are enabled (`EnableExternalServices`). Scrobbling stays off.
- **Duplicates.** A one-time `jdupes` recipe moves Duplicates to a Quarantine folder that carries a `.ndignore` file. The app has no dedupe code ([ADR-0002](./adr/0002-quarantine-duplicates-instead-of-in-app-dedupe.md)).

## Deployment

- A `docker compose` stack: the Vinyl Underground image plus an `ollama` container.
- The music share is mounted on the host and bind-mounted **read-only** into the container.
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

## Build order

The goal is to finish everything in one weekend. If time runs out, items drop from the bottom.

1. Mount the NAS, run the Quarantine cleanup, fork and rename at v0.64.2, run it over Tailscale HTTPS, and create both Listener logins.
2. Downloads.
3. Generated Playlists.
4. Stations, DJs and Blurbs.
5. GHCR publishing workflow and README.

## Repo setup note

The repo (`CrimsonCowLabs/vinyl-underground`) starts with these design docs only. When forking, bring in upstream history by fetching Navidrome's `v0.64.2` tag and merging it with `--allow-unrelated-histories`. That keeps every upstream commit reachable and marks the merge commit as the fork point. All commits are GPG-signed.
