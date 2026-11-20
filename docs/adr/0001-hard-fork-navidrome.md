# Hard fork of Navidrome, never rebased

We build the app as a permanent hard fork of Navidrome (pinned at a single upstream release) and modify its Go backend and React UI directly. We will never rebase onto or merge from upstream.

Navidrome gives us Listeners and logins, streaming, transcoding, Playlists and per-Listener favorites and history for free. Our core features cannot be added from outside the code: Downloads need audio caching in the UI's service worker, Stations need a self-refilling player queue, and Navidrome plugins cannot add UI pages or API endpoints. We considered keeping Navidrome unmodified and adding our own PWA plus a sidecar service, but chose one codebase instead, and accepted the fork's costs by refusing to rebase.

## Consequences

- We take on no upstream fixes, including security fixes. That is acceptable only because the app is meant to be reachable solely over a private network such as a tailnet. The project is published for others to self-host, so this constraint must be stated plainly to them; supporting public internet exposure would reopen this decision.
- The fork is published as a renamed, standalone GPL-3.0 project that credits Navidrome. It is not a GitHub "fork", because it will never sync with upstream.
- Upstream conventions (the old React 17 / react-admin 3 / MUI v4 UI stack, the migration system, the native API style) are now ours to keep or replace freely.
