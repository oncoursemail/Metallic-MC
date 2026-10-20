# Metallic roadmap / game network + future hosting

Each milestone has acceptance criteria. Feature ideas are grouped by dependency and **not represented as completed**.

## Current owner direction: custom software, October 20

The expanded [launcher and server platform decision](platform/architecture.md)
adds local/private independence, explicit checkpoint sync, managed private/public
hosting, a distinct external tunnel tier, protected compatibility, trustworthy
population evidence, recovery and moderation. P0–P6 in that document govern this
product work alongside C0–C7 below. Original lifecycle, sync, population and
diagnostic packages plus private Server Studio tools are implemented;
[the delivery record](platform/implementation.md) distinguishes these from
runtime execution, desktop launching and public hosting.

The [custom network specification](custom-network/architecture.md) now governs
Gateway, Authority, Presence, Director, Runtime, Games and Recovery development.
Its C0–C7 gates complement the product phases below. C0 includes four original
offline Go packages and a runnable integration lab; it is not a playable proxy or
a full LuckPerms/TAB replacement. The first durable and playable adapters are
next. Velocity remains a comparison baseline instead of the final gateway.
See the [research](custom-network/research.md), [measurement plan](custom-network/benchmarks.md)
and [implementation evidence](custom-network/implementation.md).

## M0 — Control foundation [PROTOTYPE IMPLEMENTED]

Read-only admin dashboard, authenticated Linux agent, CPU/RAM/disk/load, static service health probes, tests, threat model and initial network architecture. Live telemetry is ephemeral and must not be used as a durable reservation or billing record. Architect host policy has separately opt-in persistence.

**Done means:** local controller starts, agent heartbeat is authenticated, the dashboard displays real node telemetry, unauthorized API calls fail, and Go unit/race checks pass.

## M0.5 — Shared resources + Star AI status [PREVIEW IMPLEMENTED]

- Read-only Resource Manager estimates admission of small built-in workload templates against live CPU, RAM, monitored-disk, swap and Linux PSI metrics. No server is started or capacity allocated.
- Read-only separate bearer-token API for Star AI to explain infrastructure status. Disabled by default; explicitly does NOT grant privileged commands.
- The existing Wings service and heavy Star AI model processes on the connected machines were observed only; no host changes were made.
- Source: [shared-platform.md](shared-platform.md), [hardware-snapshot.md](hardware-snapshot.md), [star-ai-integration.md](star-ai-integration.md).

## M1 — Playable private Java + Bedrock network [STAGING TOOLING IMPLEMENTED; TRANSPORT AND GAMEPLAY PENDING]

- Operate Velocity gateway on a private/internal interface behind an approved protective edge.
- Install Geyser-Velocity and Floodgate; verify present Bedrock TCP/UDP transports and auth.
- First Paper 26.3 / Java 25 backend; `modern` forwarding, server firewalls, protected Bedrock key.
- Build MetallicCore: welcome/lobby, secure ranks/permissions, moderated chat, join/quit handling, server transfer, real telemetry and feature flags.
- Game profile presets, safe world backups, login transfer test matrix for desktop, mobile and console joining routes.

**Done means:** Java and Bedrock can authenticate, play and move through at least two protected backends with no direct backend access; restore a world backup.

The current pinned Geyser/loopback profile is known to be unable to establish
NetherNet gameplay. Launch is now guarded. Owner EULA acceptance is recorded;
dedicated container provisioning and a reachable private ICE interface remain
unresolved. A successful configuration check is not M1 completion.

## M2 — Real cluster operations [PRIVATE HOST POLICY AND RECOVERY IMPLEMENTED; ORCHESTRATION PENDING]

- PostgreSQL identity and audit store; short-TTL cache and pub/sub as needed.
- Per-node credential enrollment, signed releases, authenticated agent commands with least privileges, history and an audited job queue.
- Create/stop/restart/delete lifecycle with confirmation and reversible backups; safe resource quotas, placement, reservations and auto-recovery.
- Prometheus/OpenTelemetry, historical MSPT/TPS/per-core CPU, join failures, gateway load, game dashboards and alerts.
- Admin interface with role-based access, MFA/SSO where supported, audit trail and rollback UI.

**Done means:** a node can fail and recover without corrupting identity/permissions/rewards; all changes are auditable.

## M3 — Metallic signature minigames [NOT STARTED]

- Match Director, parties, friends, cross-server invitations, social lobbies, player profiles and match history.
- Fast match spin-up, arenas and cosmetics with asset-budget enforcement; Bedrock-aware menus and accessibility.
- Anti-cheat supported by movement baselines and server-side validation, plus human moderation and appeal workflows.
- Safe server-side plugin APIs, reusable modules, achievements, seasons and cross-game progression.
- Test candidate modes: movement/parkour, duels, build, social experiences, custom events and survival worlds. Don't clone other servers' proprietary code or visual assets.

**Done means:** at least one original game mode passes join/load/security and fairness tests across clients.

## M4 — Engine performance laboratory [NOT STARTED]

- Maintain Paper and Folia baselines with fixed versions and reproducible worlds.
- Experimental fork only where a specific measured bottleneck justifies patching upstream.
- Paper plugin compatibility API contract and regression suite, world-format protection, automated rebase test.
- Evaluate Minestom for network lobbies and short-lived minigames, which cannot directly run Bukkit/Paper plugins.
- Investigate serialization, packet batching, chunk scheduling, pathfinding and GC; reject regressions.

**Done means:** demonstrable improvement on agreed multi-scenario workloads, no silent compatibility regressions, and stable rollback.

## M5 — Public hosting (Metallic Hosting) [SEPARATE LATER PRODUCT]

- Multi-tenant isolation for potentially untrusted JARs/plugins, lifecycle APIs, secure uploads and storage.
- Server templates (Paper, supported forks, and tested plugin presets), bounded quotas, CPU/memory/disk/network limits and per-tenant backups.
- Customer roles, controlled billing integration and product catalog, support and incident handling.
- Resource pool isolation from the public Metallic game network; capacity reservation so tenants cannot degrade first-party games.
- Self-service UI, recovery points, region selection where capacity exists, and privacy/regulatory analysis.

**Done means:** penetration testing, isolated resource/cross-tenant tests, backup restores, payment failure flows, abuse procedures and support readiness pass. **Do not sell hosting before this milestone.**

## Dashboard design language

Visual design: tailored industrial editorial aesthetic, graphite surfaces, thin separators, olive-metal accent, restrained motion, deliberate typography, genuine telemetry, responsive navigation. Avoid superficial “AI dashboard” statistics or permanent glowing progress cards. Future network admin views: topology graph, per-server tick histograms, actual user activity, maps, build deploy diffs, incidents and cost/resource forecasts based on measured workloads.

## Licensing/commerce

Respect upstream GPL obligations where applicable. Do not claim plugin compatibility without automated integration tests. Follow Minecraft Usage Guidelines when creating web properties, paid ranks, marketplaces, or hosting offers. Obtain appropriate payment and tax guidance before sales.
