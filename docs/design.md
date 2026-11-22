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
  - Without an LLM, a DJ with genres (Night Owl, Sunny, or a Listener's DJ that has some) plays only Tracks in them once the Library has 50 or more such Tracks (#103), matched within each Track's genre tags; Tracks with no genre are left out. Below 50, the genres are only a boost, so a small Library never runs dry. DJs without genres (The Crate Digger) draw on the whole Library.
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
  - **Genres (#108):** when the DJ has genres (Night Owl, Sunny, or a Listener's DJ that has some), the Stations side resolves them to the Library's genre names the way rule-based selection does (a built-in DJ's words within the names, so "dance" finds "Electronica & Dance"; a Listener DJ's as whole names) and passes those to the Selector. Every candidate offered to the LLM then has one of them in its genre tags, through every fallback search; Tracks with no genre tag are never candidates. The LLM chooses its filters' genres from that list alone, so a Theme can put some of the DJ's genres first but never reach outside them. A DJ without genres (The Crate Digger), or one whose genres name none of the Library's, selects from the whole Library as before. The plan remembers the genres too, so editing a DJ's genres (or a Library change to the genres they name) starts a new plan. Generated Playlists pass no genres and are unchanged.
  - **Running ahead:** a refill never waits on the LLM. Each Listener's Station keeps a plan of the LLM's picks in memory, filled in the background ~15 Tracks at a time and again once fewer than 8 are left. A refill plays the plan in the LLM's order, keeping the ~48h and ~5-Track artist rules (a Track by an artist heard too recently waits for a later refill). Whatever the plan can't cover comes from rule-based selection.
  - **Starting (#109):** only a Station's start waits. The web app starts one with `POST /api/station/next` and `"start": true`; when that Listener and DJ have no picks planned, the request waits up to 60 s for the LLM's, while the web app shows the DJ's name and "Picking your first songs…" with a spinner (on the DJ's card and with the player; no "start anyway"). Past 60 s it answers with 5 rule-based Tracks (on the DJ's genres, as without an LLM) and the picks join later refills when they land. It doesn't wait when that Listener's last selection failed in the last ~2 minutes, nor without an LLM. Built-in and Listener DJs alike, so a built-in DJ's first start, and any DJ's first after a restart, wait; a Listener DJ's setup step (#110) plans its first picks before its first start. If a selection for another of the Listener's Stations is under way, the start waits for it to end and then has its own made, within the same 60 s. The server sets no write timeout, so the long request needs nothing else; a reverse proxy in front must allow it 60 s.
  - **Changing Station:** a different DJ or Theme starts a new plan; picks for the old one are dropped.
  - **Failures:** when a selection fails (e.g. the LLM's host is asleep), that Listener's Stations play by rule and ask the LLM again after ~2 minutes. A restart forgets the plans, so a Station's next start waits for a new one.
  - **Theme:** up to 200 characters, trimmed. With no LLM configured the Theme field is hidden (`enableStationThemes`), and a Theme sent anyway is ignored.
- How Blurbs work (#16):
  - **Writing ahead:** each refill queues a Blurb for every Track change among the Tracks it served, the first following on from the previous refill's last Track. They are written in the background, one at a time per Listener and always for the Station they listen to now, in play order. The web app refills once fewer than 3 Tracks are queued after the one playing, so each Blurb is queued at least 2–3 Tracks before its Track change. Nothing waits on them: a refill returns at once.
  - **Request:** the DJ's name and description are its persona; the LLM gets the Track that just played and the next one (title, artist, album, year, genre) and the Theme, if any, and writes 1–3 short sentences mentioning both. A reply that doesn't name both Tracks (each by artist, or by title without a note like "(Remastered)", as whole words, so "Air" isn't found in "fair"), is empty, or runs over 400 characters is dropped.
  - **Showing:** when a Station Track starts after another Track of the same Station, the web app asks `GET /api/station/blurb?dj=…&track=…&after=…`, an in-memory lookup that never waits on the LLM, and shows the Blurb with the DJ's name at the top of the screen for ~15 s. 204 means it isn't written (or failed): the Track plays without one. Only a Blurb written for that exact change (`after` → `track`) is shown, so after jumping around in the queue there is none. Asking for a Track also drops the Blurbs not yet written up to it, so a slow LLM catches up with the Listener.
  - **Memory:** Blurbs are kept in memory only, for each Listener's last Station (its ~30 newest), and a different DJ or Theme drops them, written or not; a restart forgets them. Starting the same DJ and Theme again carries on the same Blurbs: its first Track change may then wait behind Blurbs left from before, and one Blurb (into its first Track) is written for nothing. With no LLM configured (`enableStationBlurbs` false), the web app never asks. Blurbs are text only: nothing is spoken.
- Later (not now): start a Station from a Track, artist or Playlist.

### DJs
- Built-in DJs: **Night Owl** (low-key, late-night, short Blurbs), **The Crate Digger** (deep cuts and trivia), **Sunny** (upbeat, morning-show).
- Listeners can create their own DJs, which are private to them, from a name plus a personality description.
  - How Listener DJs work (#17): the Stations page lists the built-in DJs, then the Listener's own by name. "New DJ" opens an editor for a name (up to 60 characters) and a personality (up to 1000); their own DJs also get Edit and Delete there. Built-in DJs are read-only. The `dj` table keeps them per Listener, and every endpoint (`GET`/`POST /api/station/dj`, `GET`/`PUT`/`DELETE /api/station/dj/{id}`) only reaches the logged-in Listener's own, so another Listener's DJ is `unknownDJ` (404), even to an admin; editing or deleting a built-in DJ is `builtInDJ` (403).
  - **Selection:** with an LLM, a Listener DJ's name and personality are the request, as for the built-in ones, its genres limit the candidates (#108), and editing any of them starts a new plan. Without one, their DJ plays its genres the way a built-in DJ does (strict at 50+ Tracks, a boost below), and leans on the Listener's favorites, most played and best-loved artists, with some room for unplayed Tracks. A Listener DJ's genres are matched as whole genre names: "Techno" doesn't bring in "Hardcore Techno". A DJ with no genres plays only by favorites and history.
  - **Genres (#106):** a Listener DJ keeps a list of the Library's genre tags (`dj.genres`, JSON), with `dj.genre_source` saying where they came from: `llm` (mapped by the LLM, #107), `words` (matched from the name and personality), `listener` (picked by hand) or empty (a DJ from before #106). Matching is case-insensitive on words: a genre is matched when the text names it whole ("deep house" → Deep House) or has one of its telling words ("techno" → Techno, Hardcore Techno, Euro-Techno); generic words such as "music", "new" or "songs" don't count on their own. The editor shows the genres as removable tags plus an "Add a genre" picker over the Library's genres (`GET /api/station/genres`), refreshed from `GET /api/station/genres/match?name=…&description=…` when the name or personality loses focus (and from `GET /api/station/dj/{id}` on opening a DJ, which maps its genres first when they still need it, see below), with "Suggesting genres…" meanwhile, with a tip to name genres or artists, and a warning when matching found none and no LLM is configured. Words common in prose ("love", "night", "best") don't count on their own either. Once the Listener adds or removes a genre, the editor sends `genres` with the DJ and they stay as picked: later edits to the name or personality leave them alone. Picked genres the Library doesn't have are dropped. A "Re-suggest from description" button reruns the mapping for the name and personality in the form and shows the result; saving then sends `"resuggest": true`, which replaces genres picked by hand and clears that flag. That button is the only way picked genres are ever replaced.
  - **LLM-mapped genres (#107):** with an LLM, the genres are what the LLM maps the DJ's name and personality to, from the Library's 150 most common genres listed in a short prompt (structured output, at most 12, best first), e.g. "Pump the house music" → Electronica & Dance, Dance, Electronica, House, Club-House. Only genres the Library has are kept (case-insensitive, in the Library's spelling); an answer with none of them, or an LLM that fails or takes over 30 s, falls back to word matching (`words`), so saving always succeeds. The match preview, saving, and opening a DJ all map the same way, and each Listener's answers are kept in memory for an hour (their last 20 names and personalities), so saving what the form just previewed doesn't ask again and a DJ the LLM found nothing for isn't asked about at every play. Each Listener has one mapping running at a time, and after one fails, their DJs are word-matched without asking for ~2 minutes (as for selection). While the genres aren't picked by hand, editing the name or personality maps them again; saving them unchanged keeps the LLM's. **Retry and backfill:** a DJ whose genres the LLM hasn't mapped (`words`, or empty from before #106) is mapped on its next use: opening it in the form waits for the mapping (up to 30 s) and stores it; playing it (`POST /api/station/next`, start or refill) never waits: a DJ from before #106 is word-matched on the spot and plays those, and the LLM's mapping runs in the background after the refill, once that Listener's Station selection under way, if any, has ended, and is stored unless the DJ was edited meanwhile. Without an LLM, a DJ from before #106 is word-matched the first time it plays or is opened.
  - **Setup step (#110):** with an LLM, creating or editing a Listener DJ sets it up, so its Station sounds like it from the first Track. The step gets 60 s from the moment the save begins. Saving (`POST`/`PUT /api/station/dj`) maps the genres first, up to 30 s as before (#107), while the editor shows "Reading your Library's genres…". Once the DJ is stored, the server starts the DJ's first selection of 15 LLM picks from those genres (#108) in the background, into that Listener's plan for the DJ with no Theme (#104), and gets whatever the mapping left of the 60 s (at least 30 s). The editor asks `GET /api/station/dj/{id}/setup` and shows "Picking the first hour…" meanwhile. That request blocks until the setup step ends, the way a Station start does (#109), and answers `{"status": …}`: `ready` when the plan holds the LLM's picks, so the editor closes and starting the Station right after plays them without waiting; `settingUp` when the 60 s ran out or the LLM can't be reached; `none` when there is no setup (no LLM, or one that gave up). The setup runs on the server whether or not the editor asks, and closing the editor during it leaves the DJ saved.
    - **Starter and retries:** if the plan isn't ready in time, or the LLM is down for that Listener (a selection or genre mapping of theirs failed in the last ~2 minutes, which includes a mapping that just timed out during the save), the DJ keeps whatever genres it has and its plan gets a starter of 5 rule-based Tracks on its genres (strict at 50+, as without an LLM). The editor then says "Your DJ is saved. It's still being set up and will sound better shortly." with a Done button. In the background, the server tries again every 2 minutes for up to 30 minutes after the save, each try 2 minutes after the last one ended and never before that Listener's ~2-minute back-off from their latest failure (any of their selections or genre mappings) is over. Each try first maps the genres with the LLM if it hasn't yet (after any selection of that Listener's under way), carrying the starter into the plan for the new genres, then has the LLM select into the plan, its picks going after the starter. A selection still running when the 60 s ran out isn't cut off: the server waits for it first, its picks land after the starter, and the DJ counts as ready as soon as they do. Each try goes through the same per-Listener selection as a Station's, so a Listener never has two selections at once: a try waits for one under way, and none is made while their last one failed in the last ~2 minutes. After 30 minutes it stops and drops whatever is left of the starter, so the Station-start wait (#109) takes over the next time the DJ is played. Setups live in memory only. Editing the DJ again starts a new setup in place of the old one.
    - Without an LLM there is no setup step: saving closes the editor as before.
  - **Deleting:** Station history only holds a DJ's id, never a reference to its row, so deleting a DJ leaves play counts, served Tracks (still kept out for ~48h) and other Stations as they are. Its skips are left to fade and be pruned. If its Station is on air, it stops refilling and what's queued plays out. Deleting it also stops its setup and forgets its plan.
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
  - **Uploads and dropped Files (#35):** the proposal engine (`core/rip/propose`) takes plain fields (artist, title, album, year, length, and YouTube Music's metadata if any), not yt-dlp's. A File's own tags are marked as such (`Tagged`), so a title with a dash is read whole first ("Creep - Acoustic" by Radiohead), and as "Artist - Title" second; YouTube noise is still taken out.
- Review is one card per job, with editable album tags and an editable Track list. It offers **Promote all** and **Discard**, and single Tracks can also be discarded.
- Discard deletes the File permanently. Quarantine stays reserved for Duplicates.
- How review and Discard work (#33):
  - **Card:** a job's card appears once the job is ready, including ready with failures. It is driven by the job and its items, not by yt-dlp, so uploads and dropped Files (#35) can use it too. Its fields come from the Files' own tags (`GET /api/rip/{jobId}/tags`), and the album-level ones from the first Track.
  - **Saving** (`PUT /api/rip/{jobId}/tags`) sends only the fields the admin changed. The server writes only tags whose values differ, and keeps every other tag, including `SOURCE`/`SOURCEURL`. Each File is copied to a hidden temp File beside it, the copy is written and read back, and only then is it renamed over the File. A failed write never touches the original. The Inbox is then rescanned in the background.
  - **Discard** (`DELETE /api/rip/{jobId}`) deletes the job's Files and its subfolder if that is left empty, then deletes the job's `rip_job` and `rip_item` rows. There is no "discarded" status: a discarded job has nothing left to show. The rescan marks the Tracks missing, and they are then deleted, so they don't linger under Missing Files.
  - **Discarding one Track** (`DELETE /api/rip/{jobId}/items/{itemId}`) deletes its File and its item row. When the job has no Files left, the job goes too.
  - **Safety:** every path must resolve inside the Inbox. Absolute paths, `..`, symlinked Files and folders that lead out are refused before anything is written or deleted. Nothing can be edited or discarded while the job is queued or downloading (409). Only the admin can do any of this (403 for Listeners, 404 with neither ripping nor an Inbox, see "Uploads and dropped Files" below).

**Uploads and dropped Files** (#35)
- **Jobs and kinds:** an upload or a drop is a job too, with the same review card, Discard and Promote. `rip_job.kind` says which: `rip`, `upload` or `drop` (migration `20261121110000`; every earlier job is a `rip`). Upload and drop jobs and their items are ready from the start. The ripping worker only picks up Rips (`NextQueued` and the restart re-queue filter on `kind = 'rip'`), so they never reach yt-dlp. The card is labelled "Uploaded", "Dropped into the Inbox", or with the Rip's URL.
- **Gating:** uploads and drops aren't Rips, so they work whenever the Inbox is set up (`ND_INBOX_PATH`), independent of `ND_RIPPING_ENABLED`. The Rip API is mounted when either is on: the job list, review, Discard, Promote and `POST /api/rip/upload` need the Inbox; starting a Rip and yt-dlp's routes need ripping on (404 otherwise). The UI shows the Rips page to the admin when either is on, with the Rip form and yt-dlp only with ripping on, and **Upload Files** only with the Inbox. The proposal and drop workers (`Ripper.RunInbox`) run whenever the Inbox is; the ripping worker (`Ripper.Run`) and yt-dlp's schedule only with ripping on. An Inbox folder that can't be set up as a library turns the Inbox off for the run.
- **Upload** (`POST /api/rip/upload[?job=<id>]`, multipart "file" parts; `core/rip/upload.go`): one job per batch, in its own Inbox folder (`Upload <date> <time>`, with ` (2)` if taken). The web app sends a batch one File per request, each with its own progress bar, passing the first answer's job ID on, so the batch is one card. Each File must have an accepted extension (mp3, m4a, opus, ogg, flac, aiff/aif, wav), its first bytes must be that type's (an MPEG frame or ID3 tag, an MP4 `ftyp`, an Ogg page with an Opus head for opus, `fLaC`, an AIFF `FORM`, a WAVE `RIFF`), and TagLib must then read it as audio. It is streamed to a hidden temp File in `.uploads/` inside the Inbox, never into memory, and refused the moment it passes 500 MB (the request then stops being read). Only then is it moved into the job's folder by a hard link, never over an existing File (` (2)` instead). A refused File gets a per-File message and leaves nothing behind. The route has no whole-request deadline: each read gets 2 minutes, and a request carries at most 20 Files. The server sets no read or write timeout anywhere else either; a reverse proxy in front must allow 500 MB bodies.
- **Drops** (`core/rip/drops.go`): a reconciler runs after every scan this process runs (`scanner.AfterScan`), and every 5 minutes (every 30 s while Files are settling). It looks at the Inbox's Tracks (not missing) whose path no `rip_item` knows, skipping any path with a hidden part (`.rips`, `.uploads`, tag-save temps, rsync's partial Files). Files are grouped by top-level folder: one drop job per folder (named after it), and one per File for Files loose in the Inbox root, like a single video's Rip. A File is adopted only once its size and modification time have stayed the same for a minute (`Options.DropSettle`), seen across two passes, and its Track's scanned size matches the File (otherwise its folder is rescanned in full once, as a quick scan skips a File whose copy kept an older time, and it is adopted anyway if the Track still differs a minute later); a folder is adopted only when all its Files are, and its own time is a minute old (nothing added or renamed in it). A File later copied into a folder that has a drop job joins it. Rips and uploads hold a lock (`Ripper.landing`) from the moment their File lands until its item is saved, and the reconciler saves what it adopts under the same lock (re-reading the known paths first) and the review lock, so it never takes their Files and never races a Discard or Promote of the same card. The Files' tags are read before the locks are taken. A rescan adopts nothing twice, as adopted paths are then known.
- **Proposals and matching:** both start from the File's own artist, title, album and year tags and its length (TagLib); a File with no title tag is read from its name ("Artist - Title.mp3", without a leading track number like "01 - "). The item's title and artist are that starting point, and the proposal is worked out in the background by the same worker as a Rip's, one MusicBrainz request about every 1.1 s, so a dropped folder of 68 Files gets its proposals over a minute or two. "Already have" runs when the Files join the job, and leaves out the File's own Inbox Track.
- **Tags:** an uploaded or dropped File is not a Rip: nothing writes `SOURCE`/`SOURCEURL` into it, and the "Ripped" badge shows only if it already carries an http(s) `SOURCEURL`.
- **Discard** deletes the Files from disk, as for a Rip, and the job's folder with the empty folders inside it.

**Promote** (Phase 2, after the case-name merge in #2; [ADR-0008](./adr/0008-app-writes-to-one-dedicated-library-folder.md))
- Promote moves Files to `Vinyl Underground/<Album Artist>/<Album> (<Year>)/<NN> <Title>.<ext>`, a NAS folder with its own read-write mount.
- Before creating a folder, the app looks for an existing one whose name differs only in letter case and reuses it, because the NAS is case-insensitive.
- The Track keeps its identity. Its path and library change in place, so favorites, Playlist entries and play counts carry over.
- How Promote works (#36):
  - **Wiring:** the host mounts the share read-only and, separately, just its `Vinyl Underground/` folder read-write (CIFS mounts a subfolder). `deploy/docker-compose.promote.yml` binds that second mount over `/music/Vinyl Underground`, inside the read-only `/music` bind, so promoted Files are in the Library at their final path. Docker nests a read-write bind inside a read-only one, but can't create its mountpoint there, so the folder must exist on the share first. `ND_INBOX_PROMOTEPATH` (`Inbox.PromotePath`, default empty: Promote off) names it inside the container; on start it must be strictly inside a library that isn't the Inbox, and writable, or Promote stays off (no endpoint, no button).
  - **Identity:** a Track's ID is not derived from its path (only its persistent and album IDs are, and they include the library). Promote updates the Track's row in place in one transaction: its library, path and folder change and its ID doesn't, so nothing that refers to it needs remapping: annotations (stars, ratings, play counts), Playlist entries, bookmarks, the play queue, shares, scrobbles and the scrobble buffer, Station history (`station_track`), Downloads on devices (by Track ID) and other Rips' `already_have` snapshots (whose library name is updated in the same transaction). Its `updated_at` is reset, so the next scan of its new folder re-reads the File into the same row (matched by library and path), with its new library's album and persistent IDs; the old album's stars move with it, as on any album ID change. A Track whose File isn't scanned yet has no identity to keep, and is simply scanned in its new place.
  - **Endpoint:** `POST /api/rip/{jobId}/promote`, admin only (403 for Listeners), 404 with no Inbox or Promote off or for an unknown job, 409 while the job is queued or downloading or when it has no Files. It answers with each File's new path or error, and the card for what is left.
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

### UI refresh (designed 2026-11-22)
A better player and a look of our own, built into the existing web UI ([ADR-0003](./adr/0003-extend-legacy-navidrome-ui.md) still holds).

**Direction**
- Improve our own web UI rather than adopt a Subsonic client. Vibrdrome (an MIT Subsonic client) is inspiration only: a Subsonic client never shows Stations, Blurbs, Generated Playlists or our Downloads.
- The player stays `navidrome-music-player` 4.25.4 (Navidrome's fork of react-jinke-music-player), which is built around one audio element. Our views sit on top of it; it keeps doing playback and the queue.
- Order of work: Now Playing, then the equalizer, then visuals, then the look.
- A prototype (#88, in `prototypes/ui-refresh/`) settled the look and the Now Playing layout on a real phone and desktop; its decisions are below.
- Gapless playback and crossfade are deferred: both need two audio elements ([ADR-0009](./adr/0009-defer-gapless-playback-and-crossfade.md)).

**Now Playing** (#90, #91, #92)
- The inherited panel showing what other Listeners are playing is renamed **Who's Listening** (#89), freeing the name.
- Phone: tapping the mini player opens Now Playing full screen, in place of the player package's own mobile panel. A down gesture, a button, Esc or back closes it; playback carries on.
- Desktop: the cover in the player bar (or an expand button) opens a side panel docked on the right, fixed at about 400px and not resizable. It pushes the page content over rather than covering it, stays open as the Listener navigates, and whether it was open is remembered per device. Below about 1000px wide it falls back to full screen.
- Content: large album art; title, artist and album, each a link; seek bar with elapsed and remaining time; previous, play/pause and next; star and rating. The Star toggle is drawn as a star (filled when starred, outlined when not), not the heart the inherited `LoveButton` draws.
- Art: on Art, the cover sits on a record that slides partly out of its sleeve.
- Toolbar: Lyrics, Queue, EQ, Visuals, Sleep timer and Speed. A slot whose feature isn't built yet is hidden.
- The art area has **Art · Lyrics · Queue** tabs (Art becomes Art/Visuals once visuals land). The selected tab is remembered per device. On desktop, the player package's own queue pop-up is hidden.
- Lyrics: synced lyrics highlight the current line, and tapping a line seeks to it. Unsynced lyrics are shown as plain scrollable text instead of being dropped.
- Queue: tap to play, drag to reorder, remove.
- On a Station: the DJ's name and the latest Blurb (when Blurbs are enabled) in a card under the title and rating, updating as Tracks change.
- A Download plays and shows the same way offline.

**Sleep timer and playback speed** (#93)
- Sleep timer: 15, 30, 45 or 60 minutes, or "End of this Track". Playback fades out briefly and pauses; the remaining time shows in Now Playing. Not kept across reloads.
- Speed: 0.75×, 1×, 1.25×, 1.5×, 2×, keeping pitch where the browser supports it.

**Equalizer** (#94, #95; [ADR-0010](./adr/0010-one-shared-web-audio-chain.md))
- 10 bands (about 31 Hz to 16 kHz, ±12 dB), a preamp, an on/off toggle, built-in presets (Flat, Bass boost, Treble boost, Vocal, Loudness, Late night) and named custom presets.
- One shared Web Audio chain: source → equalizer → analyser for visuals → ReplayGain gain → output. ReplayGain already builds AudioContext → MediaElementSource → GainNode, lazily and only when it is on (`ui/src/audioplayer/Player.jsx`), and `createMediaElementSource` can be called only once per element, so every feature plugs into that one chain.
- All Library audio is same-origin, including Downloads (answered by the service worker from Cache Storage), so it all runs through the chain. Building the chain sets `crossOrigin='anonymous'` on the element (for Internet Radio in Firefox), so a radio stream plays through it only when its host allows CORS.
- iPhone: the equalizer and visuals sit behind an **Audio effects** switch in Settings, off by default, with a warning that playback may stop when the screen is locked. Android and desktop always have them, with no switch.

**Visuals** (#96, #97)
- Lightweight: spectrum bars, a waveform, and a glow coloured from the album art. Shown in the Art/Visuals tab, with a Full screen button.
- Milkdrop mode through butterchurn (MIT), loaded only when chosen, with a preset picker.
- A photosensitivity warning the first time visuals are turned on, and a setting that turns them off entirely. They stop drawing when hidden or in the background.

**The look** (#98, #99, #100, #101)
- A new **Vinyl Underground** colour theme (not a Station's Theme), dark and light: a cool Slate dark and a warm cream light, a faint grain, and one strong accent (Teal by default). It is the default, and "Auto" maps to it, following the system's light or dark mode. The 27 inherited colour themes are kept, and a Listener who picked one keeps it.
- Accents: about 8 fixed colours plus "From album art", which colours Now Playing and the mini player from the current cover, adjusted to stay readable. Only offered with the Vinyl Underground theme.
- The mini player is restyled: cover, title and artist, play/pause and next, a thin progress line, and a hint of the DJ's name on a Station. Tapping it opens Now Playing.
- Phones get a bottom tab bar in place of the side drawer: **Library · Playlists · Stations · Downloads · More**, with the mini player just above it. "More" holds Generated Playlists, the Inbox and Rips, settings and the admin pages. Desktop keeps its sidebar.

**Palette** (from the prototype, #88)
- Values are CSS custom properties in the prototype, each mapped to an MUI v4 theme key; its README has the full tables and contrast ratios. Body and secondary text and every accent pass WCAG AA (4.5:1); disabled text is for large text and marks only (3:1).

| Role | MUI v4 key | Dark (Slate) | Light (cream) |
|---|---|---|---|
| Background | `palette.background.default`, theme-color | `#0F1216` | `#F3EDE4` |
| Paper (tab bar, player bar, sheets) | `palette.background.paper` | `#161A1F` | `#FBF8F3` |
| Surface 2 (mini player, chips, DJ card) | custom | `#1F242B` | `#E9E1D5` |
| Surface 3 (selected tab pill, slider track) | custom | `#2A3038` | `#FFFFFF` |
| Divider | `palette.divider` | `#2E343C` | `#D6CBBC` |
| Text | `palette.text.primary` | `#E8ECF1` | `#1E1915` |
| Secondary text | `palette.text.secondary` | `#9AA4B0` | `#5E5247` |
| Disabled text | `palette.text.disabled` | `#6B7480` | `#7E7163` |

- Dark is Slate, a cool blue-grey, chosen in review over the prototype's first warm brown (also tried: neutral Graphite, true-black Ink). Light stays warm cream.
- Accents (`palette.primary.main`), dark / light: **Teal `#3EC1B0` / `#086A5F` (default)**, Vermilion `#FF5B2E` / `#B0300F`, Amber `#FF9A1F` / `#934E00`, Mustard `#E2B53A` / `#765A00`, Olive `#9DBB4E` / `#4A6510`, Denim `#6FA3F0` / `#2F5DB0`, Plum `#C08BF0` / `#7A3FB0`, Rose `#FF6F91` / `#B42851`. Text on an accent is `#0F1216` in dark and `#FFFFFF` in light.
- "From album art" keeps the cover colour's hue, raises its saturation to at least 45%, and steps its lightness until it reaches 4.5:1 against the background and Surface 2. It fades over 0.7 s between Tracks, and a grey cover falls back to the default accent.
- Grain: a 180px SVG noise tile at 7% (dark) and 8% (light) on the page background.
- Shape: 12px radius for cards, sheets and the mini player; 6px for covers; fully round for chips, tabs and the play button. Type: the system font stack, then Roboto.

**Settings are per device**
- Like the colour theme and the existing player settings, the new ones are kept per device in `localStorage`: the panel's open state, the selected tab, speed, the equalizer and its custom presets, the chosen visual, the accent and the Audio effects switch. Nothing new is stored on the server.

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
- Each version tag publishes a multi-arch (amd64 + arm64) image to `ghcr.io/crimsoncowlabs/vinyl-underground` ([releasing](./releasing.md)).
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
