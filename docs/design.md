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
- **Regenerate** reruns a Generated Playlist's Prompt at its stored length for a fresh preview. Its owner can replace the Playlist's Tracks with it; any Listener who can see it can save the preview as a new Playlist.

### Stations ([ADR-0005](./adr/0005-stations-are-personal-not-live-broadcast.md))
- Personal, endless and skippable. The Listener starts a Station with a DJ and an optional Theme.
- Selection uses the DJ's taste and the Theme, biased toward the Listener's favorites and history.
- It avoids Tracks played in the last ~48h and avoids repeating an artist within ~5 Tracks.
- Tracks and Blurbs are pre-generated 2–3 Tracks ahead.
- A skip is a mild "less like this" signal for that Station only, fading over ~30 days.
  - How skips work (#14): a Station is a Listener's with one DJ. Leaving a Station Track before half of it played is a skip; ending the Station or starting another one isn't. The web app sends it to `POST /api/station/skip` (`{dj, track}`), which records it in `station_skip` against that DJ, only for a Track one of that Listener's Stations served in the last ~48h (404 `notServed` otherwise; served Tracks aren't kept per DJ).
  - On each refill, that Station's skips scale a Track's chance: a fresh skip takes 40% off the Track's and 20% off each of its artist's Tracks', fading evenly to nothing over 30 days, after which the skip is pruned. Repeated skips compound, but never leave a Track less than a fifth of its chance.
  - With an LLM, the same weight applies to the LLM's planned picks: when a planned Track's turn comes, it plays with a chance equal to its weight and is otherwise dropped from the plan, so the LLM's order holds for the rest. Skips are kept per DJ, not per Theme.
  - Skips never touch annotations, so ratings, favorites and play counts stay as they are.
- Station plays count in play history once ~50% of the Track has played.
- How Themes and DJ taste work with an LLM (#15):
  - **Request:** the DJ's name and description, plus "Theme: …" when the Listener gave one, are the request to the Generated Playlist candidate approach (`generatedplaylist.Selector`): the LLM turns it into filters and seed artists, the server builds candidates from the Library, leaving out what the Station served in the last ~48h, and the LLM ranks them. So different DJs and Themes get different candidates and picks.
  - **Running ahead:** a refill never waits on the LLM. Each Listener's Station keeps a plan of the LLM's picks in memory, filled in the background ~15 Tracks at a time and again once fewer than 8 are left. A refill plays the plan in the LLM's order, keeping the ~48h and ~5-Track artist rules (a Track by an artist heard too recently waits for a later refill). Whatever the plan can't cover, as on the first refill of a new Station, comes from rule-based selection.
  - **Changing Station:** a different DJ or Theme starts a new plan; picks for the old one are dropped.
  - **Failures:** when a selection fails (e.g. the LLM's host is asleep), the Station plays by rule and asks the LLM again after ~2 minutes. A restart forgets the plans, so a Station plays by rule until its next selection is ready.
  - **Theme:** up to 200 characters, trimmed. With no LLM configured the Theme field is hidden (`enableStationThemes`), and a Theme sent anyway is ignored.
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
  - The tags are `SOURCE` (the site's name, e.g. `YouTube`) and `SOURCEURL`: Vorbis comments in Opus, Ogg and FLAC, `----:com.apple.iTunes:SOURCE` / `SOURCEURL` freeform atoms in M4A, and `TXXX:SOURCE` / `TXXX:SOURCEURL` frames in MP3.
  - The scanner reads them into the Track's `source` and `source_url` for every File. Adding these columns forced one full rescan, so Files already in a Library were read again.
  - The badge shows only when `SOURCEURL` is an http(s) URL, which the ripper always writes. A File another tool tagged with `SOURCE` alone (e.g. `SOURCE=CD`) gets no badge.
- A playlist of more than 50 items asks for confirmation. More than 300 is refused.
- Each Rip is a background job, run one at a time. A job's status is queued, downloading, tagging, ready for review, or failed (showing yt-dlp's error). Jobs keep running after the tab is closed.
- A job has one item per video (a single video is a job with one item), stored in `rip_job` and `rip_item` (#30):
  - **Queuing** looks at the URL with `yt-dlp --flat-playlist`, so a playlist's items are known (and shown) straight away. Over 50 items, `POST /api/rip` answers 409 with the item count and titles, and queues nothing until it is sent again with `confirm: true`. Over 300, it answers 422.
  - **Items** are ripped one at a time, in order, each with its own timeout. An item is queued, downloading, ready (its File is in the Inbox) or failed (with its own error).
  - **Partial failure:** a failed item doesn't stop the others. The job ends ready if any item landed, with "N of M Tracks failed" as its message ("ready with failures"), and failed only if none did.
  - **Restart:** items that were downloading are queued again and ripped from scratch. Items already in the Inbox are not ripped again.
  - **Files:** a playlist's Files go into an Inbox subfolder named after the playlist or album. The Inbox is scanned once, when the job is done.
- yt-dlp lives in `/data/bin`, because the container is read-only. The app downloads it, updates it on a schedule and from an "Update yt-dlp" button, and after a failed Rip it updates yt-dlp once and retries.
  - The schedule is `ND_RIPPING_YTDLPUPDATESCHEDULE` (default `@daily`). An update resolves the latest release to its tag once, fetches the binary and `SHA2-256SUMS` from that tag, and downloads nothing when `yt-dlp --version` is already that tag. A failed update keeps the old binary, and no update replaces it while a Rip runs.
  - A failed Rip is retried only when yt-dlp itself failed and an update might help. Not retried: unsupported URLs, private, removed, age-restricted, members-only or blocked videos, playlists, the Rip's own timeout, and the app's own errors.
- Before ripping, the app matches artist and title against the Library and warns "already have: …". It never blocks the Rip.
  - Matching (#32) runs at queuing, on each item, in the libraries the admin can see (the Library and the Inbox). It uses the Track search to find candidates, then compares artist and title with case, accents, punctuation and featuring credits taken out, along with YouTube noise such as "(Official Video)", "[HD]", "(Lyrics)" and a channel's " - Topic" or "VEVO". An "Artist - Title" video title is split. "Live", "Remastered" and the like are kept, as they make another recording. A likely match is not a Duplicate: Duplicates are identical Files.
  - Matches fold into the same confirmation as a big playlist: `POST /api/rip` answers 409 with `alreadyHave` (the matching items, each with its Tracks), and one click ("Rip anyway", or "Rip all N" for a big playlist) sends it again with `confirm: true`.
  - Each item keeps its matches (`rip_item.already_have`, a JSON snapshot of the Tracks), so the job list, and review, still say "already have".

**Tagging and review**
- Tag proposals start from YouTube Music's own metadata (from auto-generated "Topic" uploads) when it exists. Otherwise they come from a cleaned-up video title.
- MusicBrainz then sets the album and year. It picks the earliest official studio album by the main artist, else the single, never a compilation, live or remix release. Uploads get the same proposal.
- How proposals work (#34):
  - **Starting point:** at rip time, yt-dlp's own fields are kept on the item (`rip_item.origin`): the video's title, uploader and length, and, for a YouTube Music "Topic" upload, its `artist`/`artists`, `track`, `album` and `release_year`. With a `track` and an `artist`, those are the artist and title. Otherwise #32's normaliser cleans up the video title ("(Official Video)", "[HD]", "(Lyrics)" or a bare trailing "Lyrics", the noise beside "Remastered" or "Live" in one bracket, a channel's " - Topic"/"VEVO") and splits "Artist - Title". When the left part isn't the uploader, the whole title with the uploader as the artist is a second reading ("Creep - Acoustic" by Radiohead), looked up only if MusicBrainz doesn't know the first.
  - **Album rule** (one MusicBrainz recording search per Track, for official album, EP or single releases, by the lead artist, e.g. "Simon & Garfunkel" or YouTube Music's first artist, with the title searched without featuring credits or a remaster note): among the recordings of the same title credited to the main artist, the releases that are official, not credited to "Various Artists", and whose release group has no secondary type (compilation, live, remix, soundtrack, DJ-mix). A release by the main artist beats one by anyone else; then a studio album beats an EP, which beats the single; then the earliest year wins. The year is that release group's earliest. The search has to ask for official albums, EPs and singles: a well-known song has more recordings than one search lists, most only on compilations. The search score decides nothing, as an album recording credited with its featured artists can score below covers and remixes.
  - **Nothing found:** the album is YouTube Music's (for a Topic upload) or the File's own, if any, else the Track's own title, as for a single.
  - **When:** each item's proposal is worked out in the background once its File has landed, beside the Rips, never in their way. It is stored on the item (`rip_item.proposal`), so the card opens at once, and a stop leaves it pending, to be worked out on the next start, as is one whose lookup failed because MusicBrainz was busy or unreachable. "Refresh proposals" (`POST /api/rip/{jobId}/proposals`) works them out again.
  - **Card:** each field with a proposal offers it under the field ("Proposed: X", click to take it), and "Use proposals" takes them all. A field holding a proposal the File doesn't have yet says so. Nothing is written until Save. The album-level proposal is shown only for a value every Track's proposal agrees on, as album-level edits go into every File.
  - **MusicBrainz etiquette:** one request per 1.1 s across the whole app, a `VinylUnderground/<version> ( <repo URL> )` User-Agent, backing off on 503 (Retry-After, else 2, 4, 8, 16 s, then giving up), and answers cached for a day. `ND_RIPPING_MUSICBRAINZ=false`, or `EnableExternalServices=false`, turns the lookups off; proposals then come from the starting point alone.
  - **Uploads and dropped Files (#35):** the proposal engine (`core/rip/propose`) takes plain fields (artist, title, album, year, length, and YouTube Music's metadata if any), not yt-dlp's.
- Review is one card per job, with editable album tags and an editable Track list. It offers **Promote all** and **Discard**, and single Tracks can also be discarded.
- Discard deletes the File permanently. Quarantine stays reserved for Duplicates.
- How review and Discard work (#33):
  - **Card:** a job's card appears once the job is ready, including ready with failures. It is driven by the job and its items, not by yt-dlp, so uploads and dropped Files (#35) can use it too. Its fields come from the Files' own tags (`GET /api/rip/{jobId}/tags`), and the album-level ones from the first Track.
  - **Saving** (`PUT /api/rip/{jobId}/tags`) sends only the fields the admin changed. The server writes only tags whose values differ, and keeps every other tag, including `SOURCE`/`SOURCEURL`. Each File is copied to a hidden temp File beside it, the copy is written and read back, and only then is it renamed over the File. A failed write never touches the original. The Inbox is then rescanned in the background.
  - **Discard** (`DELETE /api/rip/{jobId}`) deletes the job's Files and its subfolder if that is left empty, then deletes the job's `rip_job` and `rip_item` rows. There is no "discarded" status: a discarded job has nothing left to show. The rescan marks the Tracks missing, and they are then deleted, so they don't linger under Missing Files.
  - **Discarding one Track** (`DELETE /api/rip/{jobId}/items/{itemId}`) deletes its File and its item row. When the job has no Files left, the job goes too.
  - **Safety:** every path must resolve inside the Inbox. Absolute paths, `..`, symlinked Files and folders that lead out are refused before anything is written or deleted. Nothing can be edited or discarded while the job is queued or downloading (409). Only the admin can do any of this (403 for Listeners, 404 with ripping off).

**Promote** (Phase 2, after the case-name merge in #2; [ADR-0008](./adr/0008-app-writes-to-one-dedicated-library-folder.md))
- Promote moves Files to `Vinyl Underground/<Album Artist>/<Album> (<Year>)/<NN> <Title>.<ext>`, a NAS folder with its own read-write mount.
- Before creating a folder, the app looks for an existing one whose name differs only in letter case and reuses it, because the NAS is case-insensitive.
- The Track keeps its identity. Its path and library change in place, so favorites, Playlist entries and play counts carry over.
- How Promote works (#36):
  - **Wiring:** the host mounts the share read-only and, separately, just its `Vinyl Underground/` folder read-write (CIFS mounts a subfolder). `deploy/docker-compose.promote.yml` binds that second mount over `/music/Vinyl Underground`, inside the read-only `/music` bind, so promoted Files are in the Library at their final path. Docker nests a read-write bind inside a read-only one, but can't create its mountpoint there, so the folder must exist on the share first. `ND_INBOX_PROMOTEPATH` (`Inbox.PromotePath`, default empty: Promote off) names it inside the container; on start it must be strictly inside a library that isn't the Inbox, and writable, or Promote stays off (no endpoint, no button).
  - **Identity:** a Track's ID is not derived from its path (only its persistent and album IDs are, and they include the library). Promote updates the Track's row in place in one transaction: its library, path and folder change and its ID doesn't, so nothing that refers to it needs remapping: annotations (stars, ratings, play counts), Playlist entries, bookmarks, the play queue, shares, scrobbles and the scrobble buffer, Station history (`station_track`), Downloads on devices (by Track ID) and other Rips' `already_have` snapshots (whose library name is updated in the same transaction). Its `updated_at` is reset, so the next scan of its new folder re-reads the File into the same row (matched by library and path), with its new library's album and persistent IDs; the old album's stars move with it, as on any album ID change. A Track whose File isn't scanned yet has no identity to keep, and is simply scanned in its new place.
  - **Endpoint:** `POST /api/rip/{jobId}/promote`, admin only (403 for Listeners), 404 with ripping or Promote off or for an unknown job, 409 while the job is queued or downloading or when it has no Files. It answers with each File's new path or error, and the card for what is left.
  - **Moving a File** (`core/rip/promote_move.go`): the Inbox is local and the NAS is CIFS, so a rename can't cross (EXDEV). The File is copied to a hidden temp File beside its destination, fsynced, read back and compared (size and SHA-256), and put in place by a hard link, or where SMB1 has none, a rename after checking the name is still free; an existing File is never replaced. Then the Track is moved, and only then is the Inbox copy deleted. On any failure the temp (or the copy) is removed and the File stays in the Inbox.
  - **Per File:** each File is promoted on its own; one that fails keeps its error on its item and the others go. When no File is left, the job and its items are deleted and its emptied Inbox folder removed, as for Discard, and the Inbox and the promote folder are rescanned.
  - **Restarts:** the destination is recorded on the item (`rip_item.promote_dest`) before the copy starts. On start (only once the promote folder passed its check, as an unmounted folder looks empty), and before promoting that item again, the move is finished when the copy is in place and matches (or the Track already points at it), and undone otherwise, removing the temp File. A move whose Track already points at a File that can't be seen is left recorded, never undone. If a scan made a second Track for the same File meanwhile (from the copy, or from an Inbox copy that couldn't be deleted), the older Track is kept and the newer one merged into it.
  - **Names:** sanitised for SMB (see the README's Promote section); folders are matched to existing ones case-insensitively after NFC normalisation.

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
- **External metadata.** Last.fm lookups are enabled (`EnableExternalServices`, and Last.fm first in the default `Agents`, unlike upstream). They need only an API key. Scrobbling stays off: the Last.fm scrobbler and account linking are removed.
  - Generated Playlist candidates add Tracks by the LLM's seed artists' similar artists that are in the Library (a Prompt with no seed artists gets none). A preview waits at most ~4 s for those lookups, then carries on from local tags alone.
- **Duplicates.** A one-time `jdupes` recipe moves Duplicates to a Quarantine folder that carries a `.ndignore` file. The app has no dedupe code ([ADR-0002](./adr/0002-quarantine-duplicates-instead-of-in-app-dedupe.md)).

## Deployment

- A `docker compose` stack running the Vinyl Underground image. An `ollama` service is optional (a compose profile) for self-hosters who run the LLM on the same machine.
- The reference deployment runs Ollama on a separate, always-on Apple Silicon Mac on the tailnet, which reads long prompts much faster than a CPU-only host. Ollama must listen on the tailnet interface, not just localhost.
- The music share is mounted on the host and bind-mounted **read-only** into the container. The one exception is the `Vinyl Underground/` folder, which has its own read-write mount for Promote ([ADR-0008](./adr/0008-app-writes-to-one-dedicated-library-folder.md)): a second host CIFS mount of just that folder, bound over `/music/Vinyl Underground` by `docker-compose.promote.yml` (turned on with `COMPOSE_FILE` and `VU_PROMOTE_DIR` in `.env`).
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
