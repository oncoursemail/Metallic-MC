# Metallic creator experience preview

Open `index.html` directly in a browser. It is a standalone design prototype with embedded CSS and JavaScript; it needs no packages, server, sign-in, or network requests. It does not modify the existing control UI or its open design branches.

The preview includes four server templates, live world-plan editing, an in-memory draft, JSON draft export, a manually advanced joining simulation, three original game concept cards, and Java/Bedrock/classic-Java menu concepts. Tab navigation supports arrow keys and Home/End. Touch layouts, focus indicators, and reduced-motion preferences are supported.

The exported JSON explicitly carries `kind: design_preview_world_draft`, `provisioned: false`, and `protection_verified: false`. It is not a provisioning API request or an authentication token. No world is created, no capacity is allocated, and none of the games or native Minecraft menus are implemented by this HTML file. The joining stages explain the intended backend contract rather than pretending that a server started.

## Validation on 2026-10-19

The local file was checked in headless Chromium at 1280 x 900 and 390 x 844. All three views fit their viewport without horizontal overflow. Draft creation, literal text rendering, JSON download, four joining stages and reset, all three game details, three menu presentations, action descriptions, and keyboard tab navigation were exercised. There were no page errors or external network requests. Desktop and phone screenshots were inspected for spacing and readability.

## Integration work

Connect the reviewed UI to a separately authenticated backend only after implementing durable world revisions, scoped creator permissions, idempotent creation/wake jobs, capacity reservation, game-aware readiness, lease-fenced world ownership, backups and tested restore. Use one typed menu/action model for native Java dialogs, Bedrock forms, and a supported legacy fallback; this preview's CSS is a visual concept, not a protocol implementation.
