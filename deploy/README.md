# Deploy over Tailscale

Runs Vinyl Underground with `docker compose` on a host in your tailnet, with the Library mounted read-only, a writable Inbox for new music, and the app served over HTTPS by `tailscale serve`. HTTPS is required: without it the browser won't register the PWA's service worker, so the app can't be installed and Downloads can't work ([ADR-0006](../docs/adr/0006-offline-downloads-in-the-pwa.md)).

The app is meant for private networks only ([ADR-0001](../docs/adr/0001-hard-fork-navidrome.md)). Use `tailscale serve`, never `tailscale funnel`, which would put it on the public internet.

| File | What it is |
|---|---|
| `docker-compose.yml` | The stack. It holds no personal values. |
| `.env.example` | Every setting, commented out at its default. |
| `.env` | Optional. Only the settings you change. Gitignored, never commit it. |

All commands below run from this `deploy/` folder unless they say otherwise.

## 1. Before you start

- **Docker** with the Compose plugin, and **Tailscale** on the host, logged in to your tailnet.
- **The Library mounted read-only on the host**, e.g. at `/mnt/music` (for an older NAS over SMB1, mount with `vers=1.0`).
- **The Library already cleaned**: names that differ only by case merged ([`contrib/library-cleanup`](../contrib/library-cleanup/README.md)), and Duplicates moved to Quarantine. The app has no dedupe code ([ADR-0002](../docs/adr/0002-quarantine-duplicates-instead-of-in-app-dedupe.md)), so whatever is in the Library on the first scan becomes a Track.
- **HTTPS certificates turned on for the tailnet**: in the Tailscale admin console, under DNS, enable MagicDNS and HTTPS Certificates. The machine's name is published in public Certificate Transparency logs when its certificate is issued, so pick a machine name you don't mind being public.

## 2. Configure

Nothing is required. With no `.env`, the stack uses these defaults:

| Setting | Default |
|---|---|
| `VU_LIBRARY_DIR` | `/mnt/music` (mounted read-only) |
| `VU_DATA_DIR` | `./data` (created and handed to `VU_UID` on first start) |
| `VU_INBOX_DIR` | `./inbox` (mounted read-write at `/inbox`; created and handed to `VU_UID` on first start) |
| `VU_UID` / `VU_GID` | `1000` / `1000` |
| `VU_PORT` | `4533`, on `127.0.0.1` only |
| `VU_PROMOTE_DIR` | unset: Promote is off (see section 12) |

If any of those is wrong for your host, or you want another app setting (`ND_*`), make a `.env` and uncomment just those lines:

```bash
cp .env.example .env
$EDITOR .env
```

The commands in this guide use these shell variables. Load them once per shell; this reads `.env` if there is one and fills in the defaults:

```bash
set -a; [ -f .env ] && . ./.env; set +a
: "${VU_LIBRARY_DIR:=/mnt/music}" "${VU_DATA_DIR:=./data}" "${VU_INBOX_DIR:=./inbox}" "${VU_PORT:=4533}"
: "${VU_TS_HOSTNAME:=$(tailscale status --json | jq -r '.Self.DNSName | rtrimstr(".")')}"
```

## 3. Check the Library before the first scan

The first scan runs as soon as the container starts, so check first that no Duplicates or Quarantine Files will be picked up.

`_Quarantine/` inside the Library must contain a `.ndignore` file, which makes the scanner skip it. So must the NAS's recycle bin if it lives on the share (`trashbox/` on Buffalo LinkStations), or be emptied: Files removed over SMB land there, and the scanner would turn them into Tracks. This lists each `.ndignore`:

```bash
find "$VU_LIBRARY_DIR" -maxdepth 2 -name .ndignore
```

Then check that no Duplicates are left, with the first and fourth steps of the [Duplicate recipe](../contrib/library-cleanup/README.md#recipe-quarantine-duplicates): `jdupes` lists the sets of matching Files and changes nothing, and `dupes.py` must report `0 Duplicates to quarantine`. Non-audio matches, such as a DVD's `.IFO` and `.BUP`, are expected and ignored.

If it plans any, finish the Duplicate pass first and check again.

## 4. Start the app and run the first scan

```bash
docker compose up -d --build
docker compose logs -f vinyl-underground
```

The first build takes several minutes. The scan starts on its own. Wait for the log line `Scan completed`; the `Completed processing folder` lines before it show how many Tracks each folder added. After that the Library is rescanned every 24 hours (`ND_SCANNER_SCHEDULE`); an admin can also start a Quick or Full Scan from the activity menu in the app's top bar.

Check that the scan picked up no Quarantine Files and no Duplicates. Both counts must be `0`:

```bash
# Tracks whose File lies under the Quarantine folder
docker compose exec vinyl-underground sqlite3 /data/navidrome.db \
  "select count(*) from media_file where missing = 0 and path like '%Quarantine/%';"

# Likely Duplicates: Files with the same title, size and duration as another Track's File
docker compose exec vinyl-underground sqlite3 /data/navidrome.db \
  "select count(*) from (select 1 from media_file where missing = 0 group by title, size, duration having count(*) > 1);"
```

If either is not `0`, stop the app (`docker compose down`), fix the Library, delete `$VU_DATA_DIR`, and start again so the first scan is clean.

## 5. Serve it over HTTPS with `tailscale serve`

`docker compose` publishes the app on `127.0.0.1:$VU_PORT` only, so nothing else on the LAN can reach it. `tailscale serve` on the host proxies HTTPS from the tailnet to that port:

```bash
sudo tailscale serve --bg "$VU_PORT"
tailscale serve status
```

`status` should show `https://$VU_TS_HOSTNAME` proxying to `http://127.0.0.1:$VU_PORT`. `--bg` keeps the config across reboots. The first request may take a few seconds while Tailscale gets the certificate. From any device on the tailnet:

```bash
curl -fsS "https://$VU_TS_HOSTNAME/ping"; echo                          # prints "."
curl -fsSI "https://$VU_TS_HOSTNAME/app/manifest.webmanifest" | head -1  # HTTP/2 200
```

No base URL setting is needed: the app is served at the root of the tailnet name, and it reads the `X-Forwarded-Host` and `X-Forwarded-Proto` headers that `tailscale serve` sends to build its own links.

To stop serving: `sudo tailscale serve --https=443 off`.

## 6. Create the two Listeners

1. Open `https://$VU_TS_HOSTNAME` in a browser on the tailnet. On the first visit the app says "To start, create an admin user". This first Listener is the **admin**.
2. Log in as the admin, open the menu at the top right, then **Users**, then **+ Create**. Enter the second Listener's username, name and password, and leave **Is Admin** off.

The same can be done from the host, which prompts for each password (choose your own usernames). Passwords are only ever typed in, never stored in `.env`:

```bash
docker compose exec vinyl-underground navidrome user create -u <admin-username> --admin
docker compose exec vinyl-underground navidrome user create -u <listener-username>
docker compose exec vinyl-underground navidrome user list
```

Each Listener has their own favorites, ratings, play counts, history and Playlists. To check they are kept apart:

1. As the first Listener, star a Track and play another to the end.
2. Log in as the second Listener (another device, or a private window). **Favourites** must not show the starred Track, and **Albums > Recently Played** must not show the played Track's album.
3. Star a Track and play one as the second Listener, then check the first Listener's lists don't show them either.

## 7. Install the app on an Android phone

On each Listener's phone:

1. Install the Tailscale app, log in to the tailnet and connect. In the app's settings, make sure **Use Tailscale DNS** is on, so the tailnet name resolves.
2. In Chrome, open `https://$VU_TS_HOSTNAME`. The address bar must show a padlock with no certificate warning.
3. Log in as that Listener.
4. Open Chrome's menu and tap **Install app** (or **Add to Home screen**, then **Install**). The Vinyl Underground icon appears on the home screen and in the app drawer.
5. Open it from the icon. It should open in its own window, with no browser address bar.

## 8. Stream over mobile data

1. On the phone, turn Wi-Fi off and keep mobile data on.
2. Make sure the Tailscale VPN is still connected (the key icon shows in the status bar). Turning on Android's **Always-on VPN** for Tailscale keeps it connected.
3. Open the installed app and play a Track. It should start within a few seconds and keep playing through the end, including with the screen locked.

If it doesn't load on mobile data but does on Wi-Fi, check `tailscale status` on the host and in the phone's Tailscale app: both must be connected to the same tailnet.

## 9. Add new music through the Inbox

New music never goes straight into the Library, which stays read-only. It lands in the **Inbox** (`$VU_INBOX_DIR` on the host, `/inbox` in the container), which the app scans as a second library called "Inbox". Only admins can see it: other Listeners never see its Tracks in browsing, search or shared Playlists, and new Listeners aren't given it. Don't add it to a Listener under **Users**, which would show them its Tracks.

Copy Files into the Inbox, in any folder layout. The folder watcher usually picks them up within seconds, and the daily scan catches anything it misses; an admin can also start a Quick Scan from the activity menu. To check:

```bash
cp -r "/path/to/New Album" "$VU_INBOX_DIR/"
docker compose exec vinyl-underground sqlite3 /data/navidrome.db \
  "select count(*) from media_file mf join library l on l.id = mf.library_id where l.name = 'Inbox' and mf.missing = 0;"
```

As the admin, the new Tracks can be played, favorited and added to Playlists straight away. The library selector in the side menu shows "Inbox" next to the main Library. Logged in as another Listener, the Tracks must not show up anywhere.

### Review cards for uploaded and dropped Files

Whenever the Inbox is set up, the admin gets the **Rips** page in the side menu, with ripping on or off: uploads and dropped Files aren't Rips. Each batch of new music gets a review card there, with the same review, **Discard** and **Promote all** as a Rip (sections 10 and 12). A card says where its Files came from: **Uploaded**, **Dropped into the Inbox**, or the Rip's URL.

- **Upload:** press **Upload Files** and pick several Files at once: mp3, m4a, opus, ogg, flac, aiff or wav, up to 500 MB each. They are sent one at a time, each with its own progress, and land together in a new Inbox folder named `Upload <date> <time>`, on one card. A File of another type, over 500 MB, or whose content isn't really audio of its type (the app checks its first bytes, then reads it with TagLib) is refused with the reason beside it, and nothing of it is kept. An upload streams into the hidden `.uploads` folder first and is moved into place only once it is complete; a File already there is never replaced (the new one gets ` (2)`). The app answers each upload request with no overall time limit, as long as data keeps coming (it gives up after 2 minutes of silence). `tailscale serve` has no body-size limit; another reverse proxy in front must allow 500 MB requests (e.g. nginx's `client_max_body_size 510m`).
- **Drop:** Files copied straight into the Inbox (above) get cards after the scan that picks them up. Each top-level folder is one card, its Files in track order, whatever folders are inside it (`New Album/CD1/...`). A File copied loose into the Inbox gets a card of its own, as a single video's Rip does; copy an album as a folder to get one card. A File is adopted only once it has stopped changing: its size and time stay the same for a minute, its Track was scanned at its full size, and nothing has been added to its folder for a minute, so a folder still being copied (even of 68 Files) becomes one card once the copy is done. Hidden Files and folders are never adopted (copies under way by `rsync`, the app's own temp Files), nor are the Files of Rips and uploads. A File copied later into a folder that already has a card joins that card.
- **Tags:** the Files are kept exactly as they are. They get no `SOURCE` tag and no "Ripped" badge, unless they already carry an http(s) `SOURCEURL` (a File ripped elsewhere). Their proposals (section 10) start from their own artist, title, album and year tags, or, with no title tag, from the File name (`Artist - Title.mp3`, without a leading track number), and their "already have" warnings never point at their own Inbox Track. Proposals come in the background at about one Track a second, so a folder of 68 Files has all its proposals after a minute or two.
- **Discard** deletes the Files from the Inbox for good, as for a Rip, along with the folders it leaves empty.

Files copied in by hand must be readable by `VU_UID`. The app sets up the Inbox library on start from `ND_INBOX_PATH` (`/inbox` by default), and scans it straight away when it is new. It must not be inside the Library, or contain it. With `ND_INBOX_PATH=` empty, or no folder at that path, no Inbox library is set up; one that already exists stays until an admin deletes it under **Libraries**.

## 10. Rip a video, a playlist or an album into the Inbox

Ripping is off by default. To turn it on, add `ND_RIPPING_ENABLED=true` to `.env` and run `docker compose up -d`. **You are responsible for what you download:** only rip what you have the right to copy.

As the admin, open **Rips** in the side menu, paste the link to a video, a playlist or an album (YouTube and YouTube Music are tested; any site yt-dlp supports is accepted) and press **Rip**. For the best tags, use an album link from `music.youtube.com`. The job shows **Queued**, then **Downloading**, then **Ready for review** once its Files are in the Inbox and scanned as Tracks, or **Failed** with yt-dlp's error message. Jobs run one at a time in the background, so closing the tab doesn't stop them. The page refreshes the list every few seconds while a Rip is queued or downloading.

A playlist or album is one job with one item per Track, ripped one at a time, in order. Expand the job to see each Track's status; while it runs, the job shows how many are done (e.g. **Downloading (3 of 12)**). A Track that fails doesn't stop the others: it shows its own error, and the job ends **Ready for review (1 failed)**. Only if no Track could be ripped does the job end **Failed**. A playlist of more than 50 items lists its Tracks and asks before it is queued; one of more than 300 is refused.

If the app stops in the middle of a Rip, the Track it was on starts again from scratch on the next start. Tracks already in the Inbox are not ripped again, and a job whose Files had all landed is only scanned.

- **yt-dlp** is not in the image. The first Rip downloads the standalone build for the host (amd64 or arm64) from yt-dlp's latest GitHub release into `$VU_DATA_DIR/bin`, and checks it against the release's `SHA2-256SUMS`. It never reads a yt-dlp config file. The image includes [deno](https://deno.com) (Alpine's package), the JavaScript runtime yt-dlp needs to read YouTube pages fully; its cache is `$VU_DATA_DIR/cache/deno`.
- **Audio** is the page's best Opus stream, else its best AAC stream, saved as `.opus` or `.m4a` as it is, never re-encoded. A page with neither fails.
- **Tags:** the File gets `SOURCE` (the site, e.g. `YouTube`), `SOURCEURL` (the page), and the title and artist the site gives.
- **File name:** `<Artist> - <Title> [<video id>].<ext>`, at the top of the Inbox for a video, and in a folder named after the playlist or album for a playlist (`<Album>/<Artist> - <Title> [<video id>].<ext>`). Each job gets a folder of its own: ripping the same album again makes `<Album> (2)`. A File already there is never replaced; the new one gets ` (2)`. While downloading, the File is in the hidden `.rips` folder, which the scanner skips.
- **Scan:** the Inbox is scanned once, when the whole job is done. (The library watcher may pick Files up sooner.)
- **Proposed tags:** once a Track has landed, the app proposes its tags in the background. The artist and title come from YouTube Music's own metadata (for `music.youtube.com` and "Topic" uploads), else from the video's title with "(Official Video)", "[HD]", "Lyrics" and the like taken out. The album and year come from [MusicBrainz](https://musicbrainz.org): the earliest official studio album by the main artist, else the single, never a compilation, live or remix release. If MusicBrainz finds nothing, the album is the Track's own title. The review card offers each proposal under its field, and **Use proposals** takes them all; nothing is written to the Files until you press **Save**. **Refresh proposals** looks them up again. MusicBrainz is asked at most about once a second, so a long album's proposals take a little while. To turn the lookups off, set `ND_RIPPING_MUSICBRAINZ=false` (they are off too with `ND_ENABLEEXTERNALSERVICES=false`); proposals then come from YouTube Music or the video's title alone.
- **Playlist links:** a video link that also names a playlist (`watch?v=...&list=...`) rips just that video. Paste the playlist's own link (`playlist?list=...`) to rip all of it.
- **Time limit:** a Track that takes longer than `ND_RIPPING_TIMEOUT` (default `30m`) is stopped and marked failed. The limit is per Track, not per job. When a failed Track is tried again after a yt-dlp update (below), the second try gets its own time limit.
- **Keeping yt-dlp working:** sites like YouTube change often, and only a newer yt-dlp keeps up. The app updates yt-dlp in `$VU_DATA_DIR/bin` three ways, without rebuilding the image:
  - **On a schedule:** `ND_RIPPING_YTDLPUPDATESCHEDULE` (default `@daily`; a cron expression or `@every 24h` also works, and `0` turns it off).
  - **On demand:** the **Update yt-dlp** button on the Rips page, next to the installed version. It refuses while a Track is being ripped or another update is running; press it again once that has finished. On a platform with no standalone yt-dlp build (anything but Linux amd64 and arm64), the app can't update a yt-dlp you put in place yourself.
  - **After a failed Track:** the app updates yt-dlp once and tries that Track once more before marking it failed. It skips this when no update could help: an unsupported URL, a private, removed, age-restricted, members-only or blocked video, a playlist, the Rip's own time limit, or an error of the app's (such as a full disk).

  Each update looks up the latest release's tag first, and downloads nothing when `yt-dlp --version` already shows that tag. Otherwise it fetches both the binary and `SHA2-256SUMS` from that tag, checks the checksum, and checks the new binary runs before it replaces the old one. A failed update leaves the old binary in place. An update never replaces yt-dlp while a Track is being ripped: it waits for that Track to finish, so in a playlist it runs between two Tracks.

## 11. Generated Playlists (optional LLM)

A Listener can type a Prompt ("rainy Sunday 90s indie") and a length (25 by default, 10 to 100), and get a Playlist picked by a local LLM from the Library ([ADR-0004](../docs/adr/0004-llm-ranks-library-candidates-never-invents-tracks.md)). The LLM never names Tracks itself: it turns the Prompt into genres, seed artists and years, the app finds up to 150 matching Tracks in the Library, and the LLM picks and orders them. Every Track in the result is a real Track the Listener can play.

With no LLM configured, the **Generate** button is not shown. If the LLM can't be reached (its host is asleep, say), the Listener sees "AI unavailable" after a few seconds rather than a hang.

The app talks to any OpenAI-compatible endpoint (Ollama, llama.cpp, LM Studio), set by two settings in `.env`:

```bash
ND_LLM_BASEURL=http://<llm-host>.<tailnet>.ts.net:11434   # one with no path gets /v1 added
ND_LLM_MODEL=qwen3.5:4b-q4_K_M
```

Both are needed and both are empty by default: with only the URL set, the **Generate** button stays hidden. For a machine on the tailnet, use its full MagicDNS name (or its tailnet IP). Docker's DNS inside the container doesn't add the tailnet's search domain, so a short name that works on the host fails in the app with "bad address".

Then `docker compose up -d` to restart the app with them. Optional: `ND_LLM_APIKEY` for endpoints that need one, `ND_LLM_TIMEOUT` (default `3m`) for one LLM call, and `ND_LLM_CONNECTTIMEOUT` (default `5s`) before reporting it unavailable.

**The model must be a GGUF build.** Every call constrains the reply with a JSON schema (`response_format` of type `json_schema`, which Ollama applies as its `format` option), because a ~4B model can't be trusted to write JSON from instructions alone. Ollama's MLX builds refuse this with "structured output is unavailable" (seen on Ollama 0.40.16). Use a tag that names a quantization, such as `qwen3.5:4b-q4_K_M`; `ollama show <model>` must list `gguf` as its format. Thinking is turned off on every call (`reasoning_effort: "none"`, the switch Ollama honours on its OpenAI-compatible route), as it made each call ten times slower.

### LLM on another machine (reference setup)

The reference setup runs Ollama on an always-on Apple Silicon Mac on the tailnet, which reads long prompts far faster than a CPU-only host:

1. Install Ollama on the Mac and pull the model: `ollama pull qwen3.5:4b-q4_K_M`.
2. Make Ollama listen on the Mac's tailnet address, not just localhost: set `OLLAMA_HOST=<tailnet-ip>:11434` for the Ollama service.
3. Keep the Mac awake on power (`sudo pmset -c sleep 0`), and Tailscale running at login.
4. Ollama has no login of its own, so use a tailnet ACL to allow port 11434 only from the app's host.
5. Check from inside the app's container, which resolves names differently from the host: `docker exec vinyl-underground wget -qO- http://<llm-host>.<tailnet>.ts.net:11434/api/tags` lists the model.

### LLM on this host (`ollama` profile)

For a self-hoster without a second machine, the stack includes Ollama as an optional service. It runs on the CPU, so expect slower Playlists than the Mac's timings below.

```bash
docker compose --profile ollama up -d
docker compose exec ollama ollama pull qwen3.5:4b-q4_K_M
```

and in `.env`: `ND_LLM_BASEURL=http://ollama:11434`, `ND_LLM_MODEL=qwen3.5:4b-q4_K_M`. Models are kept in `$VU_OLLAMA_DIR` (default `./ollama`). Ollama isn't published on any port: only the app reaches it.

### Measured fit

`qwen3.5:4b-q4_K_M` (3.3 GB) on Ollama 0.40.16 on an 8 GB A18 Pro Mac, with a Library of 15,150 Tracks and 125 genres, from the app's host over the tailnet (2026-11-20):

| Prompt | Length | Candidates | Prompt → filters | Ranking | Total |
|---|---|---|---|---|---|
| upbeat 80s pop for a summer road trip | 25 | 75 | 7–12 s | 15–20 s | 22–32 s |
| rainy Sunday 90s indie | 50 | 150 | 8–13 s | 35–40 s | 46–53 s |

Both gave fitting Playlists of real Tracks (Cyndi Lauper, The B-52's, Thompson Twins; Radiohead, Smashing Pumpkins, Blur, Nirvana). The first request after the model was unloaded also pays its load time, so `ND_LLM_TIMEOUT` allows minutes.

### Similar artists from Last.fm

When the Library's genre tags are sparse, the genres alone find few Tracks. So the app also asks Last.fm for artists similar to the LLM's seed artists, and offers Tracks by those that are in the Library, after the seed artists' own Tracks. Set a Last.fm API key (free, from <https://www.last.fm/api/account/create>) in `.env`:

```bash
ND_LASTFM_APIKEY=<your-api-key>
```

Without one, Deezer answers instead (`ND_AGENTS`, default `lastfm,deezer,listenbrainz`, sets the order). For a seed artist in the Library, the answer is cached with its artist info. **This sends the seed artists' names to Last.fm (and Deezer)**, like the artist and album pages already do.

If Last.fm is slow or can't be reached, a preview waits at most 4 seconds for it, then carries on from the Library's tags alone. `ND_ENABLEEXTERNALSERVICES=false` turns off every outside lookup.

Nothing is ever scrobbled to Last.fm: the app doesn't offer to link a Last.fm account, so `ND_LASTFM_SECRET` isn't needed.

## 12. Promote Inbox Tracks to the NAS (optional)

Once a Rip is reviewed, **Promote all** on its card moves its Files out of the Inbox into the Library on the NAS, and each Track stays the same Track: its favorites, ratings, play counts, bookmarks, Playlist entries and shares carry over, and every Listener can now see it. The app may write to exactly one folder of the share, `Vinyl Underground/` ([ADR-0008](../docs/adr/0008-app-writes-to-one-dedicated-library-folder.md)); the rest of the Library stays read-only. Promote is off (no button, no endpoint) until that folder is set up as below.

How it fits together:

- On the host, the whole share stays mounted read-only (e.g. at `/mnt/music`), and **just the `Vinyl Underground` folder** gets a second, read-write CIFS mount of its own (e.g. at `/mnt/vinyl-underground`). CIFS can mount a folder of a share: `//nas-host/share/Vinyl Underground`.
- In the container, the read-only Library is at `/music`, and `docker-compose.promote.yml` binds the read-write mount over `/music/Vinyl Underground`. So promoted Files sit inside the Library at their final path, and the scanner sees them there. Docker allows a read-write bind inside a read-only one, but it can't create the mountpoint folder inside a read-only bind, so the folder must exist on the share first.

### On the NAS: create the folder

The folder must exist before anything is mounted, and the read-only mount can't create it. Make it once from any machine that can write to the share (a desktop file manager, the NAS's web admin, or `smbclient`). For an SMB1-only NAS, `smbclient` needs `-m NT1`:

```bash
smbclient //nas-host/share -A ~/nas-credentials -m NT1 -c 'mkdir "Vinyl Underground"'
ls "$VU_LIBRARY_DIR/Vinyl Underground"   # now visible through the read-only mount
```

### On the host: mount just that folder read-write

1. A credentials file, readable only by root, so the password isn't in `/etc/fstab` (the read-only mount can share it):

   ```bash
   sudo install -d -m 700 /etc/samba-creds
   sudo install -m 600 /dev/null /etc/samba-creds/nas
   sudoedit /etc/samba-creds/nas
   ```

   with:

   ```
   username=<nas-user>
   password=<nas-password>
   ```

2. The mountpoint, owned by root and **not writable**: if the mount ever fails (`nofail` lets the host boot without it), the app finds a folder it can't write to and turns Promote off, instead of filling a folder on the local disk:

   ```bash
   sudo install -d -m 555 /mnt/vinyl-underground
   ```

3. One line in `/etc/fstab`. The space in the folder name is written `\040`:

   ```
   //nas-host/share/Vinyl\040Underground  /mnt/vinyl-underground  cifs  rw,vers=1.0,credentials=/etc/samba-creds/nas,uid=1000,gid=1000,file_mode=0664,dir_mode=0775,iocharset=utf8,noserverino,_netdev,nofail  0  0
   ```

   - `vers=1.0` is for an older NAS that speaks only SMB1; leave it out (or use `vers=3.0`) on anything newer. The kernel must allow SMB1 (`CONFIG_CIFS_ALLOW_INSECURE_LEGACY`), as for the read-only mount. Use the same `vers` as that mount.
   - `uid`/`gid` must be `VU_UID`/`VU_GID`, the user the app runs as: CIFS has no Unix owners of its own, so these and `file_mode`/`dir_mode` decide who may write. `0664`/`0775` let that user write and everyone read.
   - `iocharset=utf8` keeps accented names intact; `noserverino` avoids the inode-number problems of older SMB servers.
   - `_netdev` waits for the network at boot; `nofail` lets the host boot when the NAS is away.

4. Mount it and check it, and that the rest of the share is still read-only:

   ```bash
   sudo mount /mnt/vinyl-underground
   findmnt -no OPTIONS /mnt/vinyl-underground | tr , '\n' | grep -x rw
   findmnt -no OPTIONS "$VU_LIBRARY_DIR" | tr , '\n' | grep -x ro
   sudo -u "#${VU_UID:-1000}" sh -c 'touch /mnt/vinyl-underground/.vu-check && rm /mnt/vinyl-underground/.vu-check' && echo writable
   ```

### Turn Promote on

Add to `.env`, then `docker compose up -d`:

```bash
COMPOSE_FILE=docker-compose.yml:docker-compose.promote.yml
VU_PROMOTE_DIR=/mnt/vinyl-underground
```

The override sets `ND_INBOX_PROMOTEPATH=/music/Vinyl Underground`. On start the app checks that folder is inside the Library (not the Inbox) and writable, and logs `Promote is enabled`; otherwise it logs why and Promote stays off. Promote works with ripping on or off, on the cards of Rips, uploads and dropped Files alike (section 9).

Docker binds the host mount as it is when the container starts. Start the container after both mounts are up, and if the NAS mount drops and comes back, `docker compose restart vinyl-underground`. While the read-write mount is missing, the scanner sees an empty `Vinyl Underground/` and marks its Tracks missing; they come back, with everything attached, at the next scan once it is mounted again.

### What Promote does

- **Where Files go:** `Vinyl Underground/<Album Artist>/<Album> (<Year>)/<NN> <Title>.<ext>`, from each File's tags (as saved on the review card; save your edits first). No album artist uses the artist, then "Unknown Artist"; no album is "Unknown Album"; no year leaves out ` (<Year>)`; no track number leaves out `<NN> `. Names lose the characters SMB refuses (`\ / : * ? " < > |` become `_`), leading dots, trailing dots and spaces, and are capped at 120 bytes.
- **Letter case:** the NAS is case-insensitive, so a folder whose name differs only by letter case (or Unicode composition) is reused rather than made beside it, and a File name counts as taken in any case. A File is never replaced: the new one gets ` (2)`, ` (3)`...
- **Moving safely:** the Inbox is on local disk and the share is CIFS, so a File can't simply be renamed across. Each File is copied to a hidden temp File beside its destination, flushed to disk, read back and compared with the original (size and SHA-256), and only then put in place; the Track is pointed at it, and the Inbox copy is deleted last.
- **File by File:** a File that fails stays in the Inbox, with the error shown in the card's dialog and kept on its item; the others carry on. Press **Promote all** again once it is fixed. When no File is left, the job goes, as a Discard does, with its emptied Inbox folder. Both libraries are then rescanned.
- **Restarts:** the destination is recorded before a File is copied, so if the app stops in the middle, the next start finishes the move (when the copy is complete and matches) or undoes it (removing the temp File; the original never left the Inbox). This waits until the read-write folder is mounted and Promote is on again.
- **Who:** only the admin can promote. With Promote off, or no Inbox, there is nothing to see.

## Updating

```bash
git pull
docker compose up -d --build
```

The database and settings live in `$VU_DATA_DIR`, so they survive rebuilds. Back it up with the app stopped, or while it runs with `docker compose exec vinyl-underground navidrome backup create`, which writes into `$VU_DATA_DIR/backup`.
