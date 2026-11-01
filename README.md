# METALLIC / Minecraft Network

**Private, self-hosted Minecraft Java + Bedrock network infrastructure.**

Metallic is being designed as a complete game network with a dedicated public edge, custom game services, a cluster control plane, observability, and an eventual hosting platform. It is intended to scale horizontally across hardware owned by Metallic.

> **Current stage: foundation prototype, not production deployment.** The repository includes a real read-only dashboard, a conservative resource-placement preview, an opt-in scoped Star AI status API, and a Linux metrics agent. It does **not** yet launch Minecraft instances, host games, implement DDoS protection, provide secure multi-tenancy, store telemetry persistently, or contain a custom Minecraft engine. A successful `go test` is not a game-server load benchmark.

## Verified technical direction (October 2026)

- **Java-first game servers:** Paper 26.3 / Java 25 for compatibility-first worlds and custom plugins. Benchmark Folia for workloads where players occupy separated regions; Folia is not a transparent replacement for Paper.
- **Front door:** Velocity with authenticated modern forwarding, locked-down backends, and a configurable Minecraft-focused DDoS mitigation edge.
- **Bedrock:** Geyser-Velocity and Floodgate. Current Bedrock transport requirements include NetherNet TCP+UDP at the Geyser port, plus optional legacy RakNet as documented by Geyser. Never design the public edge as Java TCP-only.
- **Custom code:** Metallic Control (control plane), Metallic Agent (host telemetry), later Metallic Gateway extensions, Match Director, game plugins, identity/party system, and server provisioning.
- **Selective engine engineering:** Start with Paper/Folia for compatibility. Profile before forking; use Minestom for purpose-built instances that do not need vanilla or Bukkit plugins. Separate an experimental fork from stable production.
- **Scale horizontally:** Target 1k -> 5k -> 10k *aggregate concurrent players*, measured across many game server instances and multiple physical machines. No capacity number is claimed until replayable load tests pass.

## Implemented foundation: M0 + read-only M0.5

1. Authenticated **Metallic Control** web application with four functional pages: Overview, Compute Nodes, Game Services, Operations.
2. A Linux **Metallic Agent** that sends real CPU, RAM, disk, load and system uptime metrics; can perform configured TCP reachability checks.
3. Private-by-default local binding, authenticated agent heartbeat, rate-limited admin login, secure session cookies, body-size caps, CSP and response headers.
4. Resource Manager page: preview placement of **Metallic MC**, **Metallic**, and **Star AI** workloads using real current node metrics and conservative headroom checks; no allocation is performed.
5. Opt-in **Star AI read-only status API** under `/api/v1/ai/context`, using its own 24+ character token; disabled by default and exposes no credentials, local addresses or hostnames.
6. Expanded Linux agent telemetry: logical CPU threads, PSI CPU and memory pressure, swap used/total, in addition to existing metrics.
7. No fake sample player counts, no command-execution endpoint, no preloaded dummy servers, and no privileged remote agent operations.
8. Tests with Go's race detector, a CI workflow, architecture, threat model, load-test criteria, and deployment notes.

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

Keep the control plane on `127.0.0.1` initially. For remote access, put it behind an authenticated HTTPS reverse proxy or a VPN; set `METALLIC_COOKIE_SECURE=true` and ensure transport end to end is trusted. Do not publish port 8080 to the internet. Agents on different nodes must use an HTTPS endpoint and an agent credential. Current foundation uses a shared agent token; per-node short-lived credentials and mTLS are production milestones.

The resource planner is a **read-only what-if tool**; it does not book capacity or restrict existing Star AI processes. Real cgroup enforcement, safe persistent reservations and automatic game booting are **not yet implemented**.

To give Star AI a separate read-only view, optionally generate `METALLIC_STARAI_READ_TOKEN` (minimum 24 characters; different from the agent token), keep it in a secret store, and send it with `GET /api/v1/ai/context` on a protected private HTTPS connection. It never grants command execution. See [Star AI integration](docs/star-ai-integration.md).

Current limitations: no persisted sessions, nodes, metrics, auditing or RBAC; admin access is a single account; agent/service probes are TCP reachability only; Linux-only agent. See [Security](docs/security.md) before enabling remote connections.

```bash
go test -race ./...
go vet ./...
go build ./cmd/metallic-control
go build ./cmd/metallic-agent
```

## Repository layout

```text
cmd/metallic-control/    Control API + dashboard executable
cmd/metallic-agent/      Linux host monitoring process
internal/control/        Auth, metrics ingestion, JSON API and AI context
internal/resources/      Pure, tested placement-preview policy engine
internal/control/web/    Original Metallic dashboard design, assets, login
internal/agent/          Linux CPU/memory/disk measurements and TCP probes
docs/architecture.md     Target network topology and service boundaries
docs/security.md         DDoS, authentication, secrets, incident handling
docs/benchmarks.md       Test methodology, profiling and release gates
docs/roadmap.md          Versioned implementation phases and product vision
docs/hardware.md         Your physical-server discovery worksheet
docs/hardware-snapshot.md Sanitized read-only inventory of your two connected machines
docs/shared-platform.md  Shared Star AI / Metallic / Metallic MC architecture and scheduler choices
docs/star-ai-integration.md Scoped AI status API and safe in-game AI integration architecture
infra/                   Future edge, proxy, node and telemetry deployment manifests
```

## First milestones

- Foundation: controller + agent + real telemetry **implemented locally and tested**.
- Shared resources M0.5: read-only resource planner and opt-in Star AI context endpoint **implemented locally and tested**; no privileged scheduler or containers deployed.
- Java/Bedrock network pilot: Velocity + Geyser + Floodgate + Paper 26.3, authenticated forwarding and private backends **not deployed**.
- MetallicCore plugin suite: proxy orchestration, cross-server party/chat, invitations, matchmaking and events **planned**.
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


## Control Surface v0.2 design branch (research prototype)

- **New visual system:** editorial industrial interface with off-white canvas, midnight navigation rail, purpose-built typography, restrained copper details, keyboard-focused native controls and responsive workspace.
- **Old Servers:** a distinctly labeled read-only legacy discovery page. Linux agents observe only whether a process named `wings` was observed through procfs; no Pterodactyl API credentials, hosted worlds, user accounts, or old containers are accessed or modified. A missing observation is **unknown**, not proof of no servers.
- **I/O pressure:** Linux agents now observe I/O PSI, and the placement preview rejects high reported storage stalls; background AI learning is deferred if I/O PSI is unavailable. Policy decisions remain suggestions until cgroup reservations and safe reconciliation are implemented.
- **Research:** [Innovation Lab](docs/INNOVATION-LAB.md) and [Control Surface art direction](docs/design/CONTROL-SURFACE.md). The proposal describes measurable experiments, not shipped scale/performance claims.
- **Interactive sample:** `python3 tools/build_design_preview.py /tmp/metallic-preview.html` creates a stand-alone browser preview. It contains **clearly marked synthetic data**, not a production dashboard or actual telemetry from personal hardware.

**Validation:** Go unit tests, race detector, vet and builds were completed locally. Chromium browser execution was restricted in the authoring environment, so pixel/interaction and WCAG audits still require a normal browser before approving the UI. No services on `starai` or `alexbuz1` were changed or restarted as part of this branch.
