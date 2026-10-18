# ADR 0002 — Metallic Adaptive Compute
Status: PROPOSED (2026-10-18). No live deployments or claimed benchmark gains.

## Boundary and legacy infrastructure
"Old Servers" is the read-only integration label for existing Pterodactyl Wings workloads on alexbuz1. Never delete, restart, reconfigure, or migrate a legacy instance without inventory, tested backup/restore, and operator authorization. Treat the old panel/Wings API as a third-party trust boundary, not a system daemon controlled by Metallic. Begin with inventory and status only; optional authenticated API adapter follows.

Star AI, Metallic social game, and Metallic MC run as distinct workload families, each with independently configured resource limits and credentials. "Separate container" means separate services/instances; one giant container per brand is not the target. Never imply containers alone are a full security boundary for customer-controlled mods.

## Hypotheses to test
H1: CPU pressure (PSI), Linux cgroup statistics, and game MSPT percentiles predict imminent gameplay degradation better than host CPU-utilization alone.
H2: GPU/CPU-heavy Star AI background work can yield resources without harming interactive AI inference or active matches.
H3: Keeping a small, measured ready buffer reduces p95 player queue-to-game startup time; too-large buffers waste RAM.
H4: Placement using NUMA/cache topology and real CPU affinity reduces p99 MSPT relative to unconstrained placement under mixed load. Must be demonstrated per machine.
H5: Opportunistic model sleep/reload can save memory at acceptable p95 inference latency; never assume cached lessons eliminate inference costs.

## Scheduling protocol
Input: authenticated, timestamped node telemetry; actual per-cgroup CPU usage/throttling, memory.current/high/events, PSI CPU/memory/IO, disk latency/free space; game: MSPT p50/p95/p99, player count, world role; network: bytes, packet drops, RTT; Star AI: inference queue length, token latency, model residency and learning-job checkpoints.

Stages: VALIDATE (deny stale/unknown telemetry), HARD-GUARDS (management/game reserves and per-tenant limits), PLACEMENT (SLO-weighted constraints), PREPARE (resource reservation lease with expiry), START (one idempotent instance), READY (health/readiness gates), ALLOCATE (join only after ready), OBSERVE, DRAIN, SAVE/STOP, RELEASE, RECONCILE.

Do not automatically move active Minecraft worlds: live world migration is not assumed to exist. Drain participants or preserve the server until match completion. No forced shutdown of ongoing players merely to free resources for AI jobs. Use hysteresis, cooldowns, and budget caps so admission decisions do not oscillate.

## Policies
P0: control-plane/database and authentication safety reserve.
P1: live Metallic / Minecraft game sessions, explicit memory and CPU reservations.
P2: active interactive Star AI inference and content moderation.
P3: new lobby/minigame starts, only with capacity.
P4: Star AI offline evaluation, learning, model builds and asset pipelines; checkpoint and pause, reschedule at lower pressure.

Priority alone does not guarantee real-time latency. Use cgroup-v2 cpu.max/cpu.weight and cpuset where experiments justify it, memory.high/max, IO controls, process quotas, and disk quotas; test kernel throttling, swap and graceful cancellation. Do not globally disable swap or modify kernel parameters without a documented rollback.

## Acceptance gates
- Replay test with timestamps, workload fixtures, and adverse scenarios: stale agents, exhausted RAM, low disk, 99th percentile tick stall, node loss, simultaneous admissions, split-brain, rollback.
- Measure game MSPT p50/p95/p99, queue latency p95/p99, Star AI time-to-first-token p95 and quality, available memory, swap/page faults, crash recovery time and energy per unit of work.
- A/B trial versus fixed reservations, under same hardware and equal workload; publish failures and intervals, do not cherry-pick.
- Security: mTLS node identity, RBAC, action audit, approvals for destructive or billable actions. AI proposes policies but cannot disable safeguards.

## Future expansion
Compare Nomad on small clusters versus Kubernetes+Agones for session server fleets only after repeatable local profiles demonstrate a need. Consider cost, human operation, failure domains and rollback.
