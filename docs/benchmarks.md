# Performance and reliability validation plan

**Target:** a network that sustains smooth gameplay at documented, repeatable peak demand on *specified hardware*. The words “zero lag” and “thousands of players” are aspirations until validated by these tests.

## Service level targets (initial, revise after baseline)

| Signal | Proposed release gate | What it tells us |
|---|---|---|
| TPS | 20 target during representative play | Game logic keeps up with ticks |
| P95 server tick time | < 40 ms | Avoid normal tick budget saturation |
| P99 server tick time | < 50 ms | Limit visible server tick spikes |
| P95 join-to-play time | < 5 s within healthy regional network | Login, session and routing work |
| P99 gateway response latency | Baseline + measurable threshold | Proxy overhead, not player internet lag |
| Game crash rate | No crashes through 60-minute load soak | Basic stability |
| Data integrity | 100% test transactions after clean/unclean restart | Protection against dupes and loss |
| Safe rollback | Documented tested restoration | Disaster recoverability |

These are **targets, not claims of achieved performance**. Record p50/p95/p99 latency and tick time, CPU per core, throughput, GC pauses, load, RAM, allocations, chunk load time, CPU flame graphs, entity/tile entity counts, and packet serialization throughput. Network latency depends on geography and routing.

## Experiments

1. **Single-player baseline.** New Paper world with no plugins and consistent seed/chunk pregeneration; measure CPU/tick allocations.
2. **20/50/100/250 player workload.** Coordinated movement, chat, inventory edits, combat, entity spawns, block breaking, chunks, resource pack, login/logout. Never claim bot-simulation is identical to human load.
3. **Worst-case hub.** Players clustered in one small area with cosmetics, tags, NPCs, particles, scoreboard updates and high packet broadcast fan-out. This is a poor use case for Folia regions.
4. **Spatial survival.** Players spread far apart; compare Paper with Folia using the same version, chunk layouts and Folia-compatible plugins. Measure speedups and plugin breakage, not just overall TPS.
5. **Match capacity.** Separate instances per match, stress matchmaking and teardown, crash a backend in mid-game, and test duplicate rewards and rescheduling.
6. **Proxy and Bedrock.** Java join storms, Bedrock join storms using actual clients, name/UUID consistency, NetherNet TCP/UDP ingress, movement/combat consistency, disconnect and fast transfer.
7. **Node failure.** Remove network access, disk pressure, full file system, server stop, JVM OOM, stale agent, gateway failure. Test recovery and no double allocation.
8. **Security/stability.** Authorized rate-limit tests and isolated flood testing against **owned** staging components only; do not load-test third-party IPs or services without authorization.
9. **Extended soak.** Multi-hour mixed workload; detect leaked threads, files, players, heaps, sockets, or unbounded caches.

## Benchmark protocol

- Pin exact Paper/Folia/Velocity/Geyser builds, plugin versions, JVM (Java 25 for 26.3), CPU power settings, cores, RAM, network, Linux kernel and world seed.
- Reproduce tests five times after warmup; report median plus variation. Compare `same hardware / same workload / same plugins` to avoid marketing-only speed results.
- Use `spark` profiles for Paper JVM hotspots; inspect GC logs, JFR, `async-profiler` if permitted, and off-CPU delays. Profile when a performance issue is actually happening.
- Hold features behind a measurable pass/fail gate. Do not merge optimizations that improve one benchmark while breaking correctness or plugin compatibility.
- Keep a permanent benchmark evidence folder and performance regression workflow when we have a running test environment.

## Capacity estimation

A scenario-based estimate, *not a hardware promise*: 5,000 players across 100 game instances averages 50 per instance. The capacity also requires enough machines to run those instances, multiple gateways, shared services, memory headroom, network throughput, and redundancy. Workload and tick budget define the actual players-per-instance limit; there is no universal magic number.

## First custom code candidates

- Chunk load budgeting and prediction, subject to profiling; immutable asset preprocessing and pregen.
- Bounded async persistence pipelines and cache invalidation with correctness checks.
- Packet/cosmetic update coalescing and visibility-aware broadcasting in our own plugins.
- Match-specific lightweight runtimes and recyclable game-instance templates.
- Separately instrumented plugin runtime cost and memory impact.

Do not start by replacing the entire Minecraft networking/physics stack: it increases upgrade risk, Bedrock compatibility problems, and time-to-playable. Keep an experiment branch for engine-level changes with upstream license requirements.
