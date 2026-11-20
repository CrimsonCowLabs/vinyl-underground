# Vinyl Underground

Vinyl Underground is a modified fork of [Navidrome](https://github.com/navidrome/navidrome) v0.64.2 (© Deluan Quintão and contributors, GPL-3.0). Forked on 2026-11-20 and modified since; see git history.

A self-hosted music server for your own Library, with offline Downloads on your phone, AI-generated Playlists from a local LLM, and Stations hosted by DJ personas.

**Status: design stage.** Nothing is built yet. The plan is in [`docs/design.md`](docs/design.md), the vocabulary in [`GLOSSARY.md`](GLOSSARY.md), the key decisions in [`docs/adr/`](docs/adr/), and the work is tracked in this repo's issues.

Vinyl Underground will be a modified hard fork of [Navidrome](https://github.com/navidrome/navidrome) v0.64.2, licensed under GPL-3.0. It is intended for private networks only (e.g. a tailnet), not the public internet.

To run it on a tailnet with `docker compose` and `tailscale serve`, see [`deploy/README.md`](deploy/README.md).
