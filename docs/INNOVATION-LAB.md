# Metallic Innovation Lab — engineering experiments, not marketing claims

**Date:** 2026-10-18. **Status:** research and measurable prototypes, not deployed features.

Goal: operate Metallic Minecraft, Metallic social game, and Star AI on the owner's existing machines as one coordinated, highly efficient fleet, without a training job causing live game lag or an untrusted mod compromising other workloads. No claim of "zero lag", "world's fastest" or "DDoS-proof" is verifiable today.

## Real infrastructure baseline (owner-authorized read-only sample)

- `starai`: Ubuntu 24.04, AMD EPYC 7451, 24 physical/48 logical cores, **four NUMA domains**, 125 GiB RAM, one connected 1 Gb/s plus one 10 Gb/s interface, 9.8 TB data volume, approximately 223 GB system volume that was nearly full (89%), multiple llama-server processes consuming most compute. The 10 Gb/s NIC is NOT proof of internet uplink speed. Disk `sda` appears behind a PERC controller; verify whether the data set is spinning media, SSDs or mixed RAID before making tiering claims.
- `alexbuz1`: Ubuntu 24.04, Intel Core i7-9700, eight physical cores, 15 GiB RAM, 238 GB NVMe, one 1 Gb/s interface, active Wings + Docker and Star AI llama-server. Do not repurpose existing managed containers until migration is approved.
- Both: unified Linux cgroup v2 and kernel PSI available. GPU visibility via `nvidia-smi` was unavailable; don't infer the absence of GPUs. Public ISP throughput, protected edge, virtualization options, per-core clocks, sustained storage IOPS, thermals, UPS, switch topology and available cgroup controller delegation remain **unverified**.

## Experiment A — Latency SLO governor (first priority)

**Original system-level idea:** construct a fleet-wide score from *actual* game MSPT tails, proxy packet latency and node CPU/memory/I/O PSI. A game instance with MSPT p99 pressure triggers **learning-job admission deferral**, not indiscriminate killing. Current Go planner already rejects stale metrics and high I/O PSI; future policy will include task-level cgroup PSI, memory.low protection and explicit workload leases.

Proof gates: controlled full-node baselines without game workloads; repeatable match fixtures; p50/p95/p99 MSPT and p99 proxy delay over 30+ minutes, testing under competing AI inference and checkpointable learning. Starter goals (to validate, not guarantees): p95 MSPT <35 ms, p99 <45 ms for the defined game mode; no memory-OOM or severe swap storms; learning has bounded checkpoint/restore latency. Avoid interpreting average CPU utilization as a measure of per-tick performance.

Risks: CPU throttling via `cpu.max` can itself increase game tick latency. Benchmark exclusive/allowed core sets, `CPUWeight`, memory.high and I/O limits under equivalent loads instead of assuming more limits equal more speed. Never rewrite cgroups created and owned by Docker/Wings behind its back.

## Experiment B — NUMA-aware game lanes on EPYC

Measure Epyc processor physical cache topology and `/sys/devices/system/node` CPU affinities. Reserve *sets* of cores/memory-locality for latency-sensitive game instances and distribute AI batch tasks across remaining domains. Compare three baselines: Linux defaults, cpuset only, and cpuset+memory binding. Mixed-node NUMA effects are hardware/OS dependent and can harm performance. Don't change the Star AI service without staging/approval.

Proof: more stable game MSPT p99, lower cross-NUMA memory traffic, no negative impact to Star AI inference throughput or model reload time. Record frequency, thermals, disk stalls, kernel versions and confounders; revert if there is no meaningful win.

## Experiment C — World Foundry: fast warm game fleets

Templates are immutable and contain no secrets. Pre-generate chunks, pre-warm server images and keep a small *measured* ready buffer per game mode. A match allocates an existing healthy instance through a lease; when emptied, it drains and resets rather than leaving corrupted reusable world state. Scale based on queue depth, current warm capacity and observed cold-start latency, with hysteresis and cooldown. For lightweight original modes, evaluate Minestom's in-memory instance model; for vanilla/plugin-compatible modes use Paper. Never pretend Minestom can run Paper plugins.

Proof: p95 queue-to-play time, warm-start time, RAM overhead per idle slot, TPS/MSPT at peak, and correctness of world reset after unclean exit. Agones ready-buffer autoscaling is a reference, not a reason to deploy Kubernetes before needing it.

## Experiment D — Dual-lane Star AI scheduler

Split Star AI into: (1) small bounded *live* inference lane (for players/NPCs/creator assistants) with strict timeouts, concurrency controls and warm model budget; (2) resumable *offline* learning/benchmark lane that uses spare capacity. An overloaded NPC request must not stall a Minecraft tick thread: asynchronous, bounded, fail-fast, with non-AI fallback. `llama.cpp` model idle sleep can unload memory but introduces reload latency; measure first-token latency before switching it on for any live service.

STAR's current reviewed-learning workflow stores text lessons, not trained model weights. It cannot be assumed that "learning a lot" eliminates CPU/RAM demand. Cached outputs, quantized models, smaller task-specialized routing, KV-cache discipline and prompt deduplication are candidates for benchmarking; do not silently change existing model configuration.

## Experiment E — Protocol-aware Java + Bedrock edge

Use Velocity for Java proxying and modern forwarding plus firewall-restricted backends; adopt Geyser/Floodgate and specifically account for Bedrock 26.60 NetherNet's **TCP signaling and UDP gameplay**. For production, compare managed edge providers that explicitly support both transports, and test IPv4/IPv6, origin isolation and direct-backend bypass. Do not publish the home origin or claim a local firewall protects against ISP uplink saturation.

Proof: authentication, reconnect, transfer, Java/Bedrock packet compatibility and UI parity tests; rate-limit/abuse handling and verified provider capacity documentation. Automatic Java/Bedrock content parity is not universal: resource pack, controller, chat and protocol differences need gameplay QA.

## Experiment F — Game engine laboratory (longer-term)

A custom Paper-compatible engine fork is a **hypothesis** to be justified by profiling, not the starting product. Record hardware+JDK+version+plugins+world+player behavior. Candidates: chunk persistence and compression, tick scheduling, entity hot spots, isolated IO pipelines and game-specific deterministic simulation services. First compare a repeatable Paper baseline against Folia for sufficiently separated populations, and Minestom for features that don't need vanilla Minecraft. Maintain test conformance, crash/restart correctness and network-protocol behavior. A '20% faster' claim must have multi-run statistically credible results and regression artifacts.

## Experiment G — Untrusted tenant hosting, separately isolated

Later customer hosting cannot share the public game network's trust boundary. Docker namespaces are a useful baseline but they aren't by themselves equivalent to strong isolation from intentionally hostile mods. Evaluate microVM-based isolation or separately managed VM hosts when opening to untrusted customers, and measure memory/CPU overhead. Separate customer databases, object storage, backups, quota enforcement, anti-abuse, billing and support. Never allow customer admins to run a command on infrastructure managers or reach Star AI secrets.

## Star AI improvement pipeline — autonomous proposals, supervised promotion

1. **Observe:** collect opted-in, privacy-filtered game/system traces and performance regressions. No player chat stored for training without a privacy/legal plan.
2. **Hypothesize:** STAR describes a specific measured bottleneck and a measurable improvement target; no vague "I improved everything" updates.
3. **Experiment:** open a branch/PR in the appropriate repository, attach an experiment design, reproducible fixtures, unit/integration tests, and compute limits. STAR may generate code; it cannot edit its own scoring/evaluation gate.
4. **Evaluate:** separate CI checks and blind holdouts; game logic simulations, stress tests only against owner-owned staging, UI screenshot comparisons, static analysis, dependency licensing and security review.
5. **Review:** low-risk docs/tests may be auto-proposed. Real deployments, public networking, billing, moderation, secrets, and infrastructure changes require owner approval.
6. **Canary:** stage first, watch latency/error KPIs, roll back on regression; no automatic direct writes to live worlds.
7. **Report:** dashboard shows factual state: "Studying", "Experiment ready", "PR #... waiting review", "Staging benchmark failed", not generic unsupported activity.

This gives STAR a continuous **improvement workflow**, not unrestricted permission to modify production autonomously.

## Measurement and prioritization

| Measurement | Baseline before claim | Experimental proof |
|---|---|---|
| Minecraft lag | per-mode p95/p99 MSPT and GC logs | lower p99 in matching player/workload fixtures |
| Connection quality | proxy packet latency, Bedrock join/transfer success | fewer failures at equal client and regional load |
| Fleet density | enforceable CPU/RAM/IO requests, real reservations | more reliable simultaneous instances, not just higher thread count |
| AI interference | CPU/mem/io PSI with actual game MSPT | reduced p99 game degradation during AI work |
| Start time | provisioning+world load + readiness | lower p95 time to playable instance |
| Resource efficiency | power draw, swap, cache, IOPS | measured improvement per node with confidence intervals |
| Reliability | crash recovery and corruption drill | recover in specified SLO, no silent player-data loss |
| Dashboard craft | keyboard/touch/contrast, user feedback | pass owner review, browser QA, accessibility audits |

## Research references (official primary sources)

- Linux PSI: https://www.kernel.org/doc/html/latest/accounting/psi.html
- cgroup v2: https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html
- systemd resource control: https://www.freedesktop.org/software/systemd/man/devel/systemd.resource-control.html
- Velocity firewall + modern forwarding: https://docs.papermc.io/velocity/security/
- Folia plugin compatibility: https://docs.papermc.io/paper/dev/folia-support/
- Minestom scope: https://minestom.net/docs/when-to-use
- Geyser NetherNet migration: https://geysermc.org/blog/preparing-for-nethernet/
- Agones warm pool: https://agones.dev/site/docs/reference/fleetautoscaler/
- Pterodactyl Wings architecture: https://docs.pterodactyl.io/v1/wings
- llama.cpp server sleep/metrics: https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md
