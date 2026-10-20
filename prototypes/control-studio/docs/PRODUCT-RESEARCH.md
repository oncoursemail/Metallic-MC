# Metallic Control — competitor research and feature-parity blueprint
Research baseline: October 18, 2026. This document reflects public provider documentation, not hands-on claims about private dashboards.

## Products and primary sources

### Minehut
- https://www.minehut.com/ : browser creation, Java/Bedrock, free and paid plans, configurable web file manager, plugins, automatic sleep/wake and DDoS protection.
- https://minehut.wiki.gg/wiki/Dashboard : owned and subuser server lists, reorderable server cards, creation/deletion UI, per-server status and controls. Wiki historical descriptions may lag product changes.
- https://support.minehut.com/hc/en-us/articles/33252734474899-How-to-Setup-a-Minehut-External-Hosted-Server : supported external server workflow with connected discovery/protected routing.

### MineKeep
- https://minekeep.net/ : under-a-minute creation, discoverable hub, optional hub adverts/analytics, Java/Bedrock address, automated backups, external server connection and pricing/features.
- https://docs.minekeep.net/hibernation : 5-minute idle sleep and wake-on-player-join with lobby transfer; whitelist safety.
- https://docs.minekeep.net/server-administration/adding-plugins : manual plugin upload and file manager.

### Pyro / Pyrodactyl / Elytra
- https://pyro.host/docs/game/overview : console, files, databases, schedules, backups, users, proxy routes, private networks, software, startup, SFTP, network allocations and settings.
- https://pyro.host/docs/game/users-permissions : per-action permission scoping including privileged backup/restore and startup settings.
- https://pyro.host/docs/game/backups : automatic policies, content-addressed incremental storage behavior; requires measurement before adoption.
- https://github.com/pyrohost/pyrodactyl : original repository states inactive; Apache-2.0 license noted, brand and trademark limitations remain.
- https://github.com/pyrodactyl-oss/pyrodactyl : successor archived on 2026-07-10; do not assume up-to-date security support.
- https://github.com/pyrohost/elytra : original Wings derivative; AGPL-3.0 in forked code, older components retain MIT. Evaluate exact file licenses and network-service AGPL obligations before adapting any code. This project does not copy any panel/daemon source.

### Pelican
- https://pelican.dev/docs/comparison/ : permissions, backup, plugin marketplaces, authentication and security feature comparisons.
- https://github.com/pelican/plugins : first-party themes, Modrinth integrations, tickets and proof-of-concept billing. Its example billing plugin explicitly warns against production use.
- https://github.com/pelican/panel/security : security advisories underscore server-side permissions and safer API boundaries.

## Feature parity — desired outcomes, not current implementation

| Domain | Minimum parity | Metallic differentiation | Milestone |
|---|---|---|---|
| Server creation | Select game/server type, version, size, domain and owners | Admission policy rejects launches threatening gameplay SLOs, with explanation | M2 |
| Lifecycle | Start, stop, restart, safe delete/restore | Verified health/readiness, draining, immutable audit trail and rollback | M2 |
| Game software | Paper, Purpur, Folia, Fabric, NeoForge and original engine minigames | Version matrix with plugin checks, upgrade staging, Bedrock parity smoke suite | M2–M3 |
| Java/Bedrock | Same hub/world where technically possible | Controller/touch-aware UI, packet translation and load budgets | M2–M3 |
| Console/files | Streamed logs, file manager, safe editing, SFTP-equivalent | Timelined config diffs, staging preview and secure change approvals | M2 |
| Backups | Daily + on-demand, restore/download/retention | Dedup immutable snapshots, offsite replicas, restore drills and blast-radius previews | M2 |
| Roles | Account owner/subuser + scoped permissions | Reusable project roles; server-specific scopes; MFA; dual-control for high-risk actions | M2 |
| Modding | Discovery/install/update of mods and plugins | Dependency/permission/license checks, signed artifact catalog, reproducible packs | M3 |
| Worlds | Create, swap, upload, pregenerate and backup | Reflink snapshots and optional warm pools with measured readiness | M3 |
| Scheduling | Timed restarts/backups/commands | Resource-aware quiet windows, SLO-preserving placement and rollback | M2 |
| Host discovery | Search, tags, public/private, queues | High-quality curated discovery, recommendations with privacy/anti-spam controls | M4 |
| Domains/edge | Server aliases, DDoS ingress, own server bridging | Verified origin isolation, transport-aware Java + NetherNet UDP/TCP routing | M2 |
| Moderation | Player kick/mute/ban, server delisting/suspend, appeals | Evidence-linked cases, staff RBAC, server owner notices, reversible enforcement | M2–M3 |
| Insights | Uptime, CPU, players, networking | Deep gameplay p95/p99 MSPT, JVM GC, chat report spikes, resource PSI, node topology | M2–M3 |
| Marketplace | Plans, addons and shared templates | Measured quota/cost, anti-abuse controls, separation of owned game fleet and tenants | M4 |
| AI assistant | Operational answers | Star AI submits reproducible, reviewed patches; no unrestricted production admin power | M3 |

### Out-of-scope for early delivery
Billing and customer self-service hosting, open public-facing destructive APIs, dynamic permissions inferred from screenshots, custom anti-DDoS scrubbing code, and multi-region availability claims. Launch these only after security and capacity tests.

## Performance architecture research, testable not guaranteed

1. **Pressure-aware admission:** cgroup-v2 `cpu.max`, `cpu.weight`, `memory.high`, `memory.max`, `io.max` and Linux PSI. Measure service p99 latency under mixed AI + gameplay contention rather than maximize CPU average. https://docs.kernel.org/admin-guide/cgroup-v2.html
2. **GameServer allocation:** Agones' atomic Ready-to-Allocated transitions and optional ready-buffer autoscaling are good conceptual references. With just two physical hosts, avoid operational Kubernetes complexity until tested ROI. https://agones.dev/site/docs/reference/gameserverallocation/ and https://development.agones.dev/site/docs/reference/fleetautoscaler/
3. **Server engine choices:** Paper's default plugin ecosystem, Folia's region schedulers for suitable separated work, and Minestom for custom short-lived minigames (not Bukkit compatible). Benchmark each with equivalent **gameplay** rather than compare empty server counts. https://docs.papermc.io/paper/dev/folia-support/
4. **Crossplay forward compatibility:** Geyser's October 2026 guidance says upcoming NetherNet changes require TCP signaling and UDP traffic. Confirm current build/mode, allocate and shield both transports. https://geysermc.org/blog/preparing-for-nethernet/
5. **Server tick pipeline:** game-world mutations stay on Paper/Folia-defined thread/region schedulers. Move DB, mail, cosmetics catalog, matchmaking and analytics to bounded async calls with timeouts. https://docs.papermc.io/paper/dev/scheduler/
6. **Warm pools:** measured pre-warmed lobbies/minigame sessions only; world snapshot integrity, ID assignment and queue expiration make naive pooling unsafe.
7. **Kernel tuning:** Don't blindly disable swap, move cores around, use huge pages, tune TCP sysctls or put IO on RAM disks without baselines, safety and rollback.

## Differentiation scorecard
User outcomes, not claims of universal superiority:
- Join-to-game p95 and p99 (including Bedrock) under defined load.
- Gameplay server p95/p99 tick duration at same features, plugins, entities and view distances.
- Hosting create-to-ready p95, with cold/warm benchmarks shown separately.
- Server recovery time, successful world restores, player data integrity.
- Permission bypass resistance and moderator response time with audited incidents.
- CPU/RAM/energy per player-minute across the actual two-machine cluster.
- Owner satisfaction through real usability tasks, not mock-up glamour metrics.

### Design benchmark scoring
Evaluate Foundry, Nocturne, Fieldnote and the preserved Control v0.2 against operator tasks: discover lagging match in <20 seconds; identify responsible host; isolate suspected workload; review server suspension evidence; locate restore action; explain who changed a template. Record task completion, error rate, comprehension and aesthetic preference. Keep all concepts until reviewed.
