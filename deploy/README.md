# Deploy over Tailscale

Runs Vinyl Underground with `docker compose` on a host in your tailnet, with the Library mounted read-only and the app served over HTTPS by `tailscale serve`. HTTPS is required: without it the browser won't register the PWA's service worker, so the app can't be installed and Downloads can't work ([ADR-0006](../docs/adr/0006-offline-downloads-in-the-pwa.md)).

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
| `VU_UID` / `VU_GID` | `1000` / `1000` |
| `VU_PORT` | `4533`, on `127.0.0.1` only |

If any of those is wrong for your host, or you want another app setting (`ND_*`), make a `.env` and uncomment just those lines:

```bash
cp .env.example .env
$EDITOR .env
```

The commands in this guide use these shell variables. Load them once per shell; this reads `.env` if there is one and fills in the defaults:

```bash
set -a; [ -f .env ] && . ./.env; set +a
: "${VU_LIBRARY_DIR:=/mnt/music}" "${VU_DATA_DIR:=./data}" "${VU_PORT:=4533}"
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

## Updating

```bash
git pull
docker compose up -d --build
```

The database and settings live in `$VU_DATA_DIR`, so they survive rebuilds. Back it up with the app stopped, or while it runs with `docker compose exec vinyl-underground navidrome backup create`, which writes into `$VU_DATA_DIR/backup`.
