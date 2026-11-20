# Downloads live in the PWA's browser storage, transcoded to Opus

Offline playback could have come from a native Android app or an existing Subsonic client. We chose to build it into the web UI as a PWA: a service worker caches transcoded audio (Opus, ~160 kbps) in browser storage, and the app requests persistent storage so Android doesn't evict it. That keeps one codebase across phone and desktop. Navidrome's own service worker cached only the app shell, so this is new code.

## Consequences

- HTTPS is mandatory for service workers. Deployments must serve the app over HTTPS (e.g. `tailscale serve`).
- Downloadable units are Playlists and albums. A downloaded Playlist syncs automatically when online: added Tracks download and removed ones are deleted.
- Storage depends on the browser. A storage meter is shown instead of a hard cap.
