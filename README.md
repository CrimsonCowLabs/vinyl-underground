# Vinyl Underground

Vinyl Underground is a modified fork of [Navidrome](https://github.com/navidrome/navidrome) v0.64.2 (© Deluan Quintão and contributors, GPL-3.0). Forked on 2026-11-20 and modified since; see git history.

A self-hosted music server for a small household's own Library, with offline Downloads on your phone, AI-generated Playlists from a local LLM, and Stations hosted by DJ personas.

> [!WARNING]
> **Private networks only.** Vinyl Underground is a hard fork that never takes changes from Navidrome, **including security fixes** ([ADR-0001](docs/adr/0001-hard-fork-navidrome.md)). It is not hardened for the public internet. Run it only where your own devices can reach it, such as a [Tailscale](https://tailscale.com) tailnet. Never forward a port to it, put it behind a public reverse proxy, or use `tailscale funnel`.

## What it does

- Streams your Library to a browser or the installed web app (PWA). Phones get Opus at about 160 kbps (MP3 in Safari on an iPhone); desktops get the original File.
- Each Listener has their own login, favorites, ratings, play counts, history and Playlists. Playlists are private by default and read-only when shared.
- Downloads: albums and Playlists saved on the phone for offline playback, kept up to date when the app is online.
- Generated Playlists: type a Prompt ("rainy Sunday 90s indie") and a local LLM picks real Tracks from your Library. Optional.
- Stations: an endless queue hosted by a DJ persona, shaped by your skips and an optional Theme.
- The Inbox: new music, uploaded, copied in or ripped from a web page, reviewed by the admin and then promoted into the Library. Optional.

The plan is in [`docs/design.md`](docs/design.md), the vocabulary in [`GLOSSARY.md`](GLOSSARY.md), the key decisions in [`docs/adr/`](docs/adr/), and the work is tracked in this repo's issues.

## Self-hosting

You need:

- A Linux host on your tailnet, with Docker (and its Compose plugin), Tailscale, and `cifs-utils` for a network share.
- Your music on a share the host can mount, or a local folder.
- Optional: a machine that can run an LLM (see [LLM](#llm-generated-playlists-and-station-themes)).

Clone this repo on the host (`git clone https://github.com/CrimsonCowLabs/vinyl-underground.git`); paths below are relative to the clone. The steps set up the host once. [`deploy/README.md`](deploy/README.md) is the full runbook for the stack itself: the settings, the first-scan checks, Listeners, phones, the Inbox and Promote. All its commands run from `deploy/`.

### 1. Mount the music share read-only

The app never writes to the Library ([ADR-0002](docs/adr/0002-quarantine-duplicates-instead-of-in-app-dedupe.md)); the one exception is an optional folder for Promote ([ADR-0008](docs/adr/0008-app-writes-to-one-dedicated-library-folder.md), [below](#inbox-uploads-ripping-and-promote)). Mount the share on the host read-only, and the container binds it read-only again at `/music`.

A credentials file, readable only by root, keeps the password out of `/etc/fstab`:

```bash
sudo install -d -m 700 /etc/samba-creds
sudo install -m 600 /dev/null /etc/samba-creds/nas
sudoedit /etc/samba-creds/nas    # username=<nas-user> and password=<nas-password>, one per line
sudo install -d -m 555 /mnt/music
```

Then one line in `/etc/fstab` (`nas-host` and `music` are your NAS and share):

```
//nas-host/music  /mnt/music  cifs  ro,vers=3.0,credentials=/etc/samba-creds/nas,uid=1000,gid=1000,iocharset=utf8,noserverino,_netdev,nofail  0  0
```

- **Older NAS devices** (such as old Buffalo LinkStations) speak only SMB1: use `vers=1.0` instead of `vers=3.0`. The host's kernel must still allow SMB1; `grep CONFIG_CIFS_ALLOW_INSECURE_LEGACY /boot/config-$(uname -r)` must show `=y`. SMB1 is old and insecure, which is one more reason to keep the NAS on a private network.
- `uid`/`gid` give the Files to the user the app runs as (`VU_UID`/`VU_GID`, `1000` by default), so it can read them.
- `iocharset=utf8` keeps accented names intact. `noserverino` avoids the inode-number problems of older SMB servers.
- `_netdev` waits for the network at boot. `nofail` lets the host boot when the NAS is away.

Mount it and check that it is read-only:

```bash
sudo mount /mnt/music
findmnt -no OPTIONS /mnt/music | tr , '\n' | grep -x ro    # prints "ro"
touch /mnt/music/.vu-check                                 # must fail: "Read-only file system"
```

Start the app only once the share is mounted. If the app starts without it, it sees an empty Library and marks its Tracks missing; they come back, with their favorites and play counts, at the next scan once the share is mounted again.

### 2. Move Duplicates into Quarantine

The app has no dedupe code ([ADR-0002](docs/adr/0002-quarantine-duplicates-instead-of-in-app-dedupe.md)): every File becomes a Track, so a Duplicate such as `Song 1.mp3` shows up twice. Before the first scan, move byte-identical Duplicates into a `_Quarantine/` folder on the share, which the scanner skips. You need `jdupes` and Python 3.10+ on the host, and a temporary writable mount of the share beside the read-only one (same options, with `rw` instead of `ro`). On a case-insensitive NAS (old SMB1 devices), first [merge names that differ only by letter case](contrib/library-cleanup/README.md#recipe-merge-names-that-differ-only-by-case), as `jdupes` can't be trusted on them.

```bash
sudo install -d /mnt/music-rw
sudo mount -t cifs //nas-host/music /mnt/music-rw \
  -o rw,vers=3.0,credentials=/etc/samba-creds/nas,uid=$(id -u),gid=$(id -g),iocharset=utf8,noserverino
cd contrib/library-cleanup
PLANS=~/library-plans; mkdir -p "$PLANS"    # any folder outside the share
```

1. **List the Duplicate sets.** `jdupes` with no action flag changes nothing, so it runs on the read-only mount. Leave out `_Quarantine/` and the NAS's recycle bin, if it has one on the share (`trashbox/` on a LinkStation):

   ```bash
   (cd /mnt/music && find . -mindepth 1 -maxdepth 1 -type d ! -name _Quarantine ! -name trashbox -printf '%P\0' \
     | xargs -0 jdupes -r -S) > "$PLANS/jdupes.txt"
   python3 scan.py /mnt/music > "$PLANS/scan.tsv"
   python3 dupes.py "$PLANS/jdupes.txt" "$PLANS/scan.tsv" --root /mnt/music > "$PLANS/dupes.tsv"
   ```

2. **Review the plan.** Each line of `dupes.tsv` moves one File to the same path under `_Quarantine/` and names the identical File that stays. Delete a line to keep both.

3. **Dry run first, then apply:**

   ```bash
   python3 apply.py /mnt/music-rw "$PLANS/dupes.tsv" --dry-run
   python3 apply.py /mnt/music-rw "$PLANS/dupes.tsv"
   ```

   `apply.py` checks each File is still byte-identical to the one kept before it moves it, and gives `_Quarantine/` a `.ndignore` so the scanner skips it. It can be stopped and re-run.

4. **Check:** repeat step 1; `dupes.py` must report `0 Duplicates to quarantine`. Give the recycle bin a `.ndignore` too (`touch /mnt/music-rw/trashbox/.ndignore`), or empty it, as Files removed over SMB land there. Then `sudo umount /mnt/music-rw`.

This is the short form of the [full recipe](contrib/library-cleanup/README.md#recipe-quarantine-duplicates), which explains the plan's format and which File is kept. The passes after it there, for Duplicates whose tags differ and for albums held twice, are worth running too. Re-run them whenever new Duplicates may have arrived.

### 3. Get the app

Run the rest from `deploy/` (from the clone's root: `cd deploy`). [`deploy/docker-compose.yml`](deploy/docker-compose.yml) and [`deploy/.env.example`](deploy/.env.example) hold no personal values: every setting has a default, and your own values go in a gitignored `deploy/.env`.

- **Build from source** (the default): leave `VU_IMAGE` unset, and add `--build` to `docker compose up` in the next step. The first build takes several minutes.

- **Published image** (amd64 and arm64), once a release is published ([the image's versions](https://github.com/CrimsonCowLabs/vinyl-underground/pkgs/container/vinyl-underground)); until then, build from source. Set the version in `.env` and pull it:

  ```bash
  echo 'VU_IMAGE=ghcr.io/crimsoncowlabs/vinyl-underground:<version>' >> .env
  docker compose pull vinyl-underground
  ```

  Tags are the full version (`1.2.3`, without the `v`), the minor line (`1.2`), and `latest` for the newest stable release. How releases are cut is in [`docs/releasing.md`](docs/releasing.md).

The defaults expect the Library at `/mnt/music` and keep the database in `deploy/data` and the Inbox in `deploy/inbox`. To change those or any app setting, see [Configure](deploy/README.md#2-configure).

### 4. Start it and check the first scan

```bash
docker compose up -d            # or: docker compose up -d --build
docker compose logs -f vinyl-underground
```

The first scan starts on its own; wait for `Scan completed`. Then check it picked up no Quarantine Files and no Duplicates, as in [the runbook](deploy/README.md#4-start-the-app-and-run-the-first-scan).

### 5. Serve it over HTTPS with Tailscale

The web app needs HTTPS: without it the browser won't register its service worker, so it can't be installed and Downloads can't work ([ADR-0006](docs/adr/0006-offline-downloads-in-the-pwa.md)). Docker publishes the app on `127.0.0.1:4533` only, and `tailscale serve` adds HTTPS on the tailnet:

1. In the [Tailscale admin console](https://login.tailscale.com/admin/dns), under **DNS**, turn on **MagicDNS** and **HTTPS Certificates**. Each machine's name is published in the public Certificate Transparency logs when it gets a certificate, so pick a host name you don't mind being public.
2. On the host:

   ```bash
   sudo tailscale serve --bg 4533    # or your VU_PORT
   tailscale serve status            # https://<host>.<your-tailnet>.ts.net proxying to http://127.0.0.1:4533
   ```

3. Open `https://<host>.<your-tailnet>.ts.net` from any device on the tailnet and create the admin.

`--bg` keeps the setting across reboots. Use `serve`, never `funnel`. No base URL setting is needed. The checks, the second Listener and installing the app on a phone are in [the runbook](deploy/README.md#5-serve-it-over-https-with-tailscale-serve).

### Updating

```bash
git pull
# published image: change VU_IMAGE in .env, then
docker compose pull vinyl-underground && docker compose up -d
# built from source:
docker compose up -d --build
```

The database and settings live in `deploy/data`, so they survive updates. Backups are in [the runbook](deploy/README.md#updating).

## Optional features

### LLM: Generated Playlists and Station Themes

The app talks to any OpenAI-compatible endpoint: Ollama, llama.cpp, LM Studio or a hosted API. Set both lines in `deploy/.env`, then `docker compose up -d`:

```bash
ND_LLM_BASEURL=http://llm-host.example-tailnet.ts.net:11434   # one with no path gets /v1 added
ND_LLM_MODEL=qwen3.5:4b-q4_K_M
```

- **The model has no default.** Both settings are empty out of the box, and with either one empty the LLM features stay hidden.
- **A machine on the tailnet needs its full MagicDNS name** (`llm-host.example-tailnet.ts.net`, or its tailnet IP). Docker's DNS inside the container doesn't add the tailnet's search domain, so a short name like `llm-host` that works on the host fails in the app with "bad address".
- `ND_LLM_APIKEY` is for endpoints that need a key (Ollama doesn't). `ND_LLM_TIMEOUT` (default `3m`) bounds one call, and `ND_LLM_CONNECTTIMEOUT` (default `5s`) how long to wait before reporting the LLM unavailable.
- With Ollama, use a GGUF build of the model; its MLX builds refuse the JSON-schema replies the app asks for.

**Without an LLM** everything else works: streaming, Downloads, Playlists made by hand, the Inbox, and Stations, whose DJs then pick Tracks by rule, still shaped by your skips. Only the AI parts are hidden: the **Generate** and **Regenerate** buttons and the Station's **Theme** field. If a configured LLM can't be reached (its host is asleep, say), the Listener sees "AI unavailable" after a few seconds instead of a hang.

Running Ollama on another machine, or on the same host with the stack's `ollama` profile, and measured timings, are in [the runbook](deploy/README.md#11-generated-playlists-optional-llm).

### Last.fm and other outside lookups

Outside metadata lookups are on, as in Navidrome: the app fetches artist info, album info and similar artists from Last.fm (with `ND_LASTFM_APIKEY` set), Deezer and ListenBrainz. **This sends artist and album names from your Library to Last.fm** (and to those services). Generated Playlists use the similar artists to find more Tracks in the Library when its genre tags are sparse; if Last.fm can't be reached, they are built from the Library's own tags alone. Set `ND_ENABLEEXTERNALSERVICES=false` to turn all outside lookups off.

Scrobbling stays off: the app never sends what you play to Last.fm, and it doesn't offer to link a Last.fm account. Details are in [the runbook](deploy/README.md#similar-artists-from-lastfm).

### Inbox, uploads, ripping and Promote

New music never goes straight into the read-only Library. It lands in the **Inbox** (`deploy/inbox` on the host, `/inbox` in the container), which the app scans as a second library that only the admin sees. The Inbox is set up by default.

- **Uploads and dropped Files:** the admin's **Rips** page has an **Upload Files** button (mp3, m4a, opus, ogg, flac, aiff or wav, up to 500 MB each), and Files copied straight into the Inbox folder are picked up too. Each batch gets a review card, to fix its tags and then **Promote all** or **Discard**. See [the runbook](deploy/README.md#9-add-new-music-through-the-inbox).
- **Ripping** fetches the audio of a web page (usually a YouTube or YouTube Music video, playlist or album) into the Inbox with [yt-dlp](https://github.com/yt-dlp/yt-dlp). It is **off by default**: set `ND_RIPPING_ENABLED=true` in `deploy/.env` and run `docker compose up -d`. The Rip runs in the background and lands on a review card. See [the runbook](deploy/README.md#10-rip-a-video-a-playlist-or-an-album-into-the-inbox).
- **Promote** moves reviewed Files from the Inbox into the Library, keeping each Track's favorites, play counts and Playlist entries. The app may write to exactly one folder of the share, `Vinyl Underground/` ([ADR-0008](docs/adr/0008-app-writes-to-one-dedicated-library-folder.md)): create it on the share, give just that folder a second, read-write CIFS mount on the host (with `vers=1.0` for an SMB1 NAS, as above), and turn on `docker-compose.promote.yml` in `.env`. Promote is off until then. See [the runbook](deploy/README.md#12-promote-inbox-tracks-to-the-nas-optional).

**You are responsible for what you download.** Only rip what you have the right to copy, and check the terms of the site you rip from and the law where you live. Vinyl Underground and its authors don't check what you rip and accept no responsibility for it.

## Development

Building from source needs Go 1.27 (with cgo, for SQLite), Node 24 and ffmpeg. The build and test commands are in the [`Makefile`](Makefile) (`make setup`, `make dev`, `make test`), as in Navidrome.

## License and credit

Vinyl Underground is a modified fork of [Navidrome](https://github.com/navidrome/navidrome) v0.64.2, © Deluan Quintão and contributors. Like Navidrome, it is free software under the [GNU General Public License v3.0](LICENSE): you may use, change and share it under that license's terms, and it comes with no warranty. Navidrome's copyright notices are kept, and changes since the fork are in the git history. Vinyl Underground is not made or endorsed by the Navidrome project; please don't report its problems to them.
