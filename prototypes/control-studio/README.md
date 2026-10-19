# Metallic Control — Prototype Studio

**Status: interactive design prototype, NOT a production control panel.** All server counts, players, alerts, topology and usage figures are intentionally synthetic design fixtures and labeled as simulated. Buttons update only local in-memory mock state. There are no fetch() calls, no credentials, no API endpoints, no remote server effects.

Open `index.html` in a browser, or serve this folder locally (`python3 -m http.server 8181`) and open `http://localhost:8181`.

Three interchangeable original design directions:
- **Foundry:** calm editorial graphite and warm paper, rigorous operations layout.
- **Nocturne:** cinematic low-luminance high-density command surface.
- **Fieldnote:** cobalt-ink technical blueprint with instrument-grid geometry.

Works to try: switch concept; click topology nodes and edges; review and search servers; open server details and console/file/backups mock tabs; switch role from moderator to admin in moderation; run simulated suspend, ban, archive actions with reason and typed confirmation; undo archive; inspect audit events; browse Star AI proposals; use `/` or Cmd/Ctrl+K to search; press keys 1-7 to switch pages.

Functional limitations: all actions are simulated; dialogs enforce only browser-level demo permissions. There is no real authorization, persistent audit store, secrets, containers, billing, user sessions, or production moderation API. Server creation does not start JVMs. Legacy Wings integration is deliberately read-only in the **design**, and no live Wings connection exists in this preview.

License: prototype code authored for Metallic. No third-party UI source, commercial logos or copyrighted dashboard layouts copied.

For researched comparison, see docs in the Metallic GitHub feature branch. Preserve this studio and earlier dashboards as separate options until user approval.
