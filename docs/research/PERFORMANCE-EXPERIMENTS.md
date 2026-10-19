# Metallic performance research and falsifiable experiments
October 18, 2026. Research hypotheses; no unsupported speed, throughput or player capacity claims.

## Relevant upstream technology
- Paper/Folia: https://docs.papermc.io/paper/dev/folia-support/ — Folia region-affinity APIs and incompatibilities are real; don't assume Paper plugin drop-in.
- Minestom: https://minestom.net/ — programmable Minecraft server framework, not a Bukkit replacement.
- Spark profiling: https://spark.lucko.me/ — CPU, allocations and tick diagnostics.
- Linux PSI: https://systemd.io/MEMORY_PRESSURE/ — pressure signals indicate actual resource stalls.
- Agones: https://agones.dev/site/docs/overview/ — fleet orchestration with warm server pools; complexity may exceed two-node need.
- Wings: https://docs.pterodactyl.io/v1/wings — legacy endpoint adapter only.

## Benchmark matrix
Engines: Paper baseline; Folia on compatible world+plugins; Minestom for original minigame engine workloads. Never claim universal comparisons if APIs/gameplay differ. Workloads:
A) empty hub, B) 32/64/128 simulated players distributed, C) 32/64/128 hotspot, D) entity-heavy combat, E) chunk generation and flyover, F) custom cosmetic/VFX explosion, G) Star AI simultaneous inference/learning interference.
Instrument TCP/UDP and Bedrock translation separately.

## Measurement hygiene
Capture CPU model, active frequency, memory channels, storage, JVM/runtime, GC, Java flags, world seeds, game version, plugins, profile fixtures and test-agent network. Use controlled arrivals and repeat at least five runs with warm-up. Observe server MSPT p50/p95/p99, TPS only as contextual metric, timeouts, action latency, network RTT jitter, allocation/GC pause, cgroup throttling, PSI, cache misses and disk stalls. Include server-side player hotspot and cross-server transfer.

Never run a synthetic flood against public third-party servers. Agents act only on local authorized staging nodes. Load profiles must be protocol-compliant and bounded.

## Engineering techniques to evaluate
1. Pregenerate and snapshot base worlds; compare full clone, copy-on-write reflink, overlay filesystem, and safety of saved world state. Never layer mutable world data on unsafe shared backing stores.
2. Shared immutable content-addressed resource packs with CDN caching; opt-in downloadable packs, platform-specific pack variants, Bedrock conversion validation.
3. Separate simulation critical path from async database, economy, chat and analytics operations. Apply deadlines/backpressure and bounded queues, never mutate Bukkit world state asynchronously.
4. Adaptive game instance warm pools driven by real demand and conservative cooldowns.
5. Dedicated simulation CPU reservations/cpu sets while model serving uses elastic lower-priority capacity; compare against unrestricted under identical workload.
6. Crossplay parity tests across Java and Bedrock movement/input/UI before enabling ranked competitive results.
7. Deterministic minigame transaction log and authoritative server hit detection; validate against network jitter rather than promising physically impossible zero latency.
8. Test whether specific profiling-driven engine patches consistently beat baseline before carrying long-lived fork maintenance.

## Decision gate for engine fork
No production fork until >10% repeatable improvement on a real bottleneck (provisional threshold), no loss in crossplay or plugin correctness, passing regression suite and manageable patch rebase cost. A smaller or negative gain is a valid outcome.

## Source use
Upstream documentation is used as engineering context, not as proof that Metallic has installed or verified these stacks.
