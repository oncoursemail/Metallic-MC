# Metallic-MC — Minecraft Network

**Project naming:** [Metallic-RC / Metallic-MC / Metallic-RB / Metallic-PX](docs/PROJECT_IDENTITIES.md). Family domain: **Metallic.club** (application routing is not configured by this repository change).

**Private, self-hosted Minecraft Java + Bedrock network infrastructure.**

Metallic-MC is being designed as a complete game network with a dedicated public edge, custom game services, a cluster control plane, observability, and an eventual hosting platform. It is intended to scale horizontally across hardware owned by Metallic.

> **Current stage: custom offline network core, private staging tooling and control foundation.** The repository includes original packet framing, permissions, session ownership and player display code; a runnable integration lab; a real read-only dashboard; conservative placement preview; an opt-in scoped Star AI status API; a Linux metrics agent; and optional persistent hosting policy. The pinned loopback Geyser profile cannot establish Bedrock gameplay, so pilot launch is blocked. A playable custom gateway, live Java/Bedrock login, public protected edge, secure multi-tenancy and a custom game engine remain unverified or unimplemented. A successful `go test` is not a game-server load benchmark.

## Verified technical direction (October 2026)

- **Java-first game servers:** Paper 26.3 / Java 25 for compatibility-first worlds and custom plugins. Benchmark Folia for workloads where players occupy separated regions; Folia is not a transparent replacement for Paper.
- **Front door:** Metallic-owned Gateway is the target. Velocity and Gate are explicit comparison/migration baselines. Require online authentication, authenticated backend forwarding, isolated backends and a verified protective edge before promotion.
- **Bedrock:** Geyser-Velocity and Floodgate. Current Bedrock transport requirements include NetherNet TCP+UDP at the Geyser port, plus optional legacy RakNet as documented by Geyser. Never design the public edge as Java TCP-only.
- **Custom code:** Metallic Control and Agent plus Gateway, Authority, Presence, Director, Runtime, Games and Recovery modules. Four offline core packages are implemented; live adapters, durable authority and the playable custom gateway are later gates.
- **Selective engine engineering:** Start with Paper/Folia for compatibility. Profile before forking; use Minestom for purpose-built instances that do not need vanilla or Bukkit plugins. Separate an experimental fork from stable production.
- **Scale horizontally:** Target 1k -> 5k -> 10k *aggregate concurrent players*, measured across many game server instances and multiple physical machines. No capacity number is claimed until replayable load tests pass.

## Original custom network core

The owner's October 20 direction replaces the earlier plan to stop at Velocity
extensions. The target is software Metallic owns for routing, permissions,
presentation, player ownership, games and fleet policy. The common contract binds
authority epoch, session generation, process incarnation, policy revision/digest,
trusted context and viewer revision. Existing products remain fair comparison
baselines while replacements prove their behavior.

- [Architecture and staged delivery](docs/custom-network/architecture.md)
- [Deep primary-source research and pinned evidence](docs/custom-network/research.md)
- [Comparative experiment specification](docs/custom-network/benchmarks.md)
- [Implemented code, verification and remaining integration](docs/custom-network/implementation.md)

Run the deterministic offline scenario with Go 1.23+:

```sh
go run ./cmd/metallic-core-lab
```

It connects bounded packet buffering, inherited/contextual permissions, timed
denials, stale display rejection and a fenced player handoff. The output is
explicitly synthetic and `productionReady: false`. It opens no listener and does
not authenticate a Minecraft client or save a world. See each package's README
under `internal/network/` for exact API and caller responsibilities.

## Implemented foundation: M0 + read-only M0.5

1. Authenticated **Metallic Control** web application with four functional pages: Overview, Compute Nodes, Game Services, Operations.
2. A Linux **Metallic Agent** that sends real CPU, RAM, disk, load and system uptime metrics; can perform configured TCP reachability checks.
3. Private-by-default local binding, authenticated agent heartbeat, rate-limited admin login, secure session cookies, body-size caps, CSP and response headers.
4. Resource Manager page: preview placement of **Metallic-MC**, **Metallic-RC**, and **Star AI** workloads using real current node metrics and conservative headroom checks; no allocation is performed.
5. Opt-in **Star AI read-only status API** under `/api/v1/ai/context`, using its own 24+ character token; disabled by default and exposes no credentials, local addresses or hostnames.
6. Expanded Linux agent telemetry: logical CPU threads, PSI CPU and memory pressure, swap used/total, in addition to existing metrics.
7. No fake sample player counts, no command-execution endpoint, no preloaded dummy servers, and no privileged remote agent operations.
8. Tests with Go's race detector, a CI workflow, architecture, threat model, load-test criteria, and deployment notes.

## Metallic Architect: optional private workbench

Set `METALLIC_ARCHITECT_ENABLED=true` and open `/architect` after signing in to try the new private workbench.

- Discord identity linking with PKCE, browser/session binding, short-lived flows, replay rejection, unlinking, and identity-only metadata persistence. Requires your own configured Discord application.
- Single-use host pairing, unique scoped credentials, revocation, bounded allocation records, and loopback route authorization plans. These are policy operations; they do not start Minecraft or create tunnels.
- Read-only Hangar and Modrinth search and exact-version inspection with integrity/compatibility checks and explicit dependency blockers.
- A restrained purple/silver interface for accounts, hardware and plugin inspection, with no fabricated downloads, public addresses or player statistics.

The workbench is disabled by default and uses the existing private administrator. Host state defaults to memory, with an optional private durable Linux store. A protected edge, native installer, isolated host executor, multi-user identity and public directory are later implementation gates.

See the [workbench setup and API](docs/architect-workbench.md), [platform design and primary-source research](docs/architect-platform.md), [host policy contract](docs/hosting-agent-contract.md), and [plugin catalog contract](docs/plugin-catalog.md).

## Run the local development prototype

Requires **Go 1.23+ on Linux**. Java 25 is required later to run current Paper 26.3, but is not needed to try Metallic Control.

```bash
# Generate two secrets with a reputable random generator, e.g.:
export METALLIC_ADMIN_PASSWORD="$(openssl rand -base64 32)"
export METALLIC_AGENT_TOKEN="$(openssl rand -hex 32)"

# In terminal 1:
go run ./cmd/metallic-control

# In terminal 2, reusing the SAME agent token:
export METALLIC_CONTROL_URL=http://127.0.0.1:8080
export METALLIC_NODE_ID=metallic-lab-01
export METALLIC_SERVICES=proxy@127.0.0.1:25565,lobby@127.0.0.1:25566
go run ./cmd/metallic-agent
```

Open **http://127.0.0.1:8080** and sign in with `admin` and the `METALLIC_ADMIN_PASSWORD` from terminal 1. These example service probes will read **unreachable** until Minecraft ports actually listen; that is expected. If you don't want service probes yet, leave `METALLIC_SERVICES` empty.

Keep the control plane on `127.0.0.1` initially. For remote access, put it behind an authenticated HTTPS reverse proxy or a VPN; set `METALLIC_COOKIE_SECURE=true` and ensure transport end to end is trusted. Do not publish port 8080 to the internet. Agents on different nodes must use an HTTPS endpoint and an agent credential. The read-only telemetry agent still uses a shared token. Architect host pairing uses separate per-host credentials for policy calls; authenticated live tunnels and mTLS remain deployment milestones.

The resource planner is a **read-only what-if tool**; it does not book capacity or restrict existing Star AI processes. Real cgroup enforcement, safe persistent reservations and automatic game booting are **not yet implemented**.

To give Star AI a separate read-only view, optionally generate `METALLIC_STARAI_READ_TOKEN` (minimum 24 characters; different from the agent token), keep it in a secret store, and send it with `GET /api/v1/ai/context` on a protected private HTTPS connection. It never grants command execution. See [Star AI integration](docs/star-ai-integration.md).

Current control-application limitations: no persisted administrator sessions, live nodes, metrics or audit log; admin access is a single account, and the new offline policy core is not yet wired into its enforcement path. Agent/service probes are TCP reachability only; Linux-only agent. See [Security](docs/security.md) before enabling remote connections.

```bash
go test -race ./...
go vet ./...
go build ./cmd/metallic-control
go build ./cmd/metallic-agent
```

## Private pilot and restart recovery

The [private Java/Bedrock comparison pilot](docs/private-pilot.md) stages four pinned upstream artifacts, loopback configurations, authenticated modern forwarding, an owner-managed whitelist and a hub plus game world. Source inspection of the exact pinned Geyser build established that its NetherNet gameplay cannot connect over loopback. `run` and `preflight` now reject that profile before Java executes; configuration validity and a TCP probe cannot claim playable readiness. A verified private container interface and reachable ICE address remain to be implemented. The owner's Minecraft EULA acceptance is recorded in the private comparison runtime; generated source defaults still require explicit owner acceptance.

Architect can optionally keep its host registry in a [private durable Linux store](docs/hosting-persistence.md). Set `METALLIC_HOSTING_STORE_PATH` only with Architect enabled. Host registrations, credential digests, revocations and allocation plans survive controller restart. Pending pairing tickets, administrator sessions and live telemetry remain ephemeral; nodes need fresh authenticated heartbeats. Unavailable or corrupt state stops startup instead of resetting permissions.

The [service recovery bundle](docs/service-recovery.md) prepares checksum-verified controller and telemetry binaries for dedicated Linux service accounts, private configuration, bounded restart and resource limits, graceful shutdown, and explicit rollback. Installation and enabling are separate steps. It manages only its own two services and never changes Wings or Old Servers. The pilot also supports compact backups while fully stopped and verified restoration into a new directory.

The integrated backend also observes Linux I/O pressure and uses it in placement previews. Background learning is deferred when I/O pressure is unknown. Legacy Wings observation reads process names only. The moderation package provides tested authorization decisions; it does not execute live sanctions or server operations.

These capabilities are development/staging code. See the dated [integration and validation record](docs/completion-2026-10-20.md) for what was actually run and the remaining public-launch gates.

## Repository layout

```text
cmd/metallic-control/    Control API + dashboard executable
cmd/metallic-agent/      Linux host monitoring process
cmd/metallic-core-lab/   Deterministic offline integration scenario
internal/network/       Original policy, session, presentation and framing core
internal/control/        Auth, metrics ingestion, JSON API and AI context
internal/resources/      Pure, tested placement-preview policy engine
internal/control/web/    Original Metallic dashboard design, assets, login
internal/agent/          Linux CPU/memory/disk measurements and TCP probes
docs/architecture.md     Target network topology and service boundaries
docs/custom-network/     Current original architecture, research and experiment plan
docs/security.md         DDoS, authentication, secrets, incident handling
docs/benchmarks.md       Test methodology, profiling and release gates
docs/roadmap.md          Versioned implementation phases and product vision
docs/hardware.md         Your physical-server discovery worksheet
docs/hardware-snapshot.md Sanitized read-only inventory of your two connected machines
docs/shared-platform.md  Shared Star AI / Metallic-RC / Metallic-MC architecture and scheduler choices
docs/star-ai-integration.md Scoped AI status API and safe in-game AI integration architecture
infra/                   Future edge, proxy, node and telemetry deployment manifests
```

## First milestones

- Foundation: controller + agent + real telemetry **implemented locally and tested**.
- Shared resources M0.5: read-only resource planner and opt-in Star AI context endpoint **implemented locally and tested**; no privileged scheduler or containers deployed.
- Custom network C0: original policy/session/presentation/framing core and deterministic integration lab **implemented offline and tested**; no live protocol or plugin claim.
- Java/Bedrock comparison pilot: Velocity + Geyser + Floodgate + Paper 26.3 **staged, launch blocked** pending the private container transport and actual client validation.
- Custom Gateway, Authority and Presence adapters: online session protocol, durable authorization, client packet output and migration conformance **planned**.
- Metallic game suite: cross-server party/chat, invitations, matchmaking and original events **planned**.
- Fleet manager: server templates, placements, capacity management, safe restarts, snapshots and persistent metrics **planned**.
- Engine lab: controlled Paper/Folia benchmarking and optimized fork **research stage**.
- Hosting platform: isolated per-user game servers, access control, billing integration, plugin/mod compatibility and abuse prevention **long-term**.

## Upstream sources & operating constraints

- [PaperMC — Paper developer setup](https://docs.papermc.io/paper/dev/project-setup/) and [Paper Java requirements](https://docs.papermc.io/paper/getting-started/)
- [Folia overview](https://docs.papermc.io/folia/reference/overview/) and [Folia plugin compatibility](https://docs.papermc.io/paper/dev/folia-support/)
- [Velocity forwarding](https://docs.papermc.io/velocity/player-information-forwarding/) and [backend security](https://docs.papermc.io/velocity/security/)
- [Geyser setup](https://geysermc.org/wiki/geyser/setup/) and [Floodgate](https://geysermc.org/wiki/floodgate/setup/)
- [Minestom suitability](https://minestom.net/docs/when-to-use)
- [Cloudflare Spectrum](https://developers.cloudflare.com/spectrum/get-started/), [TCPShield residential restrictions](https://docs.tcpshield.com/miscellaneous/protect-a-home-hosted-server)
- [Minecraft Usage Guidelines](https://www.minecraft.net/en-us/usage-guidelines)

Upstream versions, licenses, and security advice evolve. Pin artifacts by version and checksum when preparing a real deployment; audit plugin licenses before copying upstream source or distributing a fork.

### Pressure telemetry safety

Linux CPU and memory PSI files may be absent in constrained development sandboxes. The node agent continues to report basic host telemetry, but explicitly publishes `pressureAvailable: false`; the advisory placement manager rejects that host until reliable PSI is available. Missing PSI is **never** treated as zero pressure. Live admission will require stronger per-container cgroup accounting and reconciled resource reservations.
