# Metallic MC engine and Bedrock research

Research date: 2026-10-19 UTC. This is an interim engineering report for the coordinating agent. Primary-source findings are distinguished from original design proposals and targets. No Minecraft engine was built, benchmarked, installed, or deployed in this research task. No live server protection or measured capacity is established by this report.

## Most consequential findings

1. The current Paper requirements page specifies Java 25 for Paper 26.1 and later. The current download page advertises Paper 26.3. Older Java 21 recipes are inappropriate for this baseline. Pin actual builds and JVM versions in releases.
2. Bedrock networking has materially changed. Current Geyser setup requires TCP signaling plus UDP gameplay for NetherNet. Geyser's October 8, 2026 announcement says Bedrock 26.60, planned for October 27, will stop using RakNet. This is an upcoming compatibility deadline stated by Geyser, not an observed completed rollout.
3. HTTPS signaling or an HTTP reverse proxy protects and authenticates the signaling endpoint; gameplay subsequently uses a direct WebRTC connection. The protected gameplay address and advertised ICE candidates require their own validation. A protected website is insufficient proof that Bedrock gameplay is protected.
4. Paper compatibility, region parallelism, and a small custom minigame runtime should be offered as three engine choices under one network. Claiming every existing plugin can safely run with arbitrary multithreading would contradict Folia's documented execution model.
5. The best initial custom software is the orchestration, content, admission, measurement, and crossplay layer around established engines. Only introduce engine patches after a reproducible profile identifies a problem and a matched benchmark proves an improvement without breaking gameplay.

## Primary evidence ledger

The short summaries below are the factual basis. Subsequent proposals are engineering recommendations, not performance claims by these projects. Retrieval references are embedded in the citations for the parent to reopen before citing.

### E01 — Paper runtime baseline

Paper's current requirements state Java 25 for 26.1+ and Java 21 for 1.20–1.21.11. Paper is intended as a drop-in replacement for CraftBukkit and Spigot. This does not imply that every plugin depending on internals survives every Minecraft version.

URL: https://docs.papermc.io/paper/getting-started/ .

Current downloads: https://papermc.io/downloads/paper . The retrieved page advertised Paper 26.3 build 205; use the release API/checksum at build time rather than freezing that incidental build number into a permanent promise.

### E02 — Folia region ownership

Folia assigns independent loaded areas to independently scheduled region ticks. Adjacent regions eventually merge, and independent areas can split. Region state is owned by the thread ticking it. A slow region need not stall a separate healthy region when scheduler capacity remains, but nearby interacting players can occupy the same region. Global data still has a global region. Folia's scheduler is documented as neither NUMA-aware nor CPU-core-aware.

URL: https://docs.papermc.io/folia/reference/overview/ .

### E03 — Folia plugin contract

Plugins must opt into Folia support, and the support flag alone does not make them safe. Paper provides the same global, region, asynchronous, and entity scheduler interfaces so appropriately written plugins can target both. Entity work must follow the entity scheduler across region changes; location-bound work uses the region scheduler. This is a useful foundation for Metallic's own API adapters.

URL: https://docs.papermc.io/paper/dev/folia-support/ .

### E04 — Folia workload and capacity constraints

Folia's FAQ says distributed SMP and skyblock workloads benefit most and ideally recommends at least 16 physical cores. Its rough thread-sizing example is explicitly not universal and recommends leaving room for other work. It recommends world pregeneration. It also lists disabled commands, including several commands and datapack-related operations that a compatibility catalog must assess before migration.

URL: https://docs.papermc.io/folia/faq/ .

### E05 — Velocity's secure forwarding

Velocity modern forwarding authenticates forwarded player information with a MAC and applies to supported modern versions. Paper has native support. Legacy BungeeCord forwarding is less secure. The proxy and backend must agree on forwarding mode and secrets; the backend relies on the proxy for authentication.

URL: https://docs.papermc.io/velocity/player-information-forwarding/ .

### E06 — Backend isolation

Velocity explicitly recommends a firewall and states that modern forwarding does not replace it. Backends must accept only trusted proxy traffic. An encrypted tunnel can connect the proxy to a separately hosted origin; same-host loopback binding is another configuration. These protect backend access, not upstream bandwidth capacity by themselves.

URL: https://docs.papermc.io/velocity/security/ .

### E07 — Profiling and tail latency

Paper bundles spark from 1.21 and recommends it for profiling; profiling must occur while the problem exists. spark explains that averages can hide individual long ticks and provides tick monitoring and profiling limited to ticks above a threshold. Its tick-loop documentation explains the 20 TPS / 50 ms relationship.

URLs: https://docs.papermc.io/paper/profiling/ ; https://spark.lucko.me/docs/guides/Finding-lag-spikes ; https://spark.lucko.me/docs/guides/The-tick-loop .

### E08 — Minestom's actual tradeoff

Minestom is a developer library for implementing a custom Minecraft server. It does not implement Bukkit/Forge/Sponge APIs and does not supply vanilla gameplay wholesale. It offers a multithreaded runtime and instance model suited to selectively implemented experiences. Its repository distinguishes fixed releases from floating snapshots, which can silently change dependency resolution. The retrieved repository describes its current protocol as 26.2; do not assume it is automatically compatible with Geyser's current Java target.

URL: https://github.com/Minestom/Minestom .

### E09 — Minestom shared instance semantics

A SharedInstance references its parent's chunks. Block changes in either are reflected across all linked shared instances. It is therefore not an isolated mutable copy of a map. Use immutable shared scenery where appropriate; destructive arenas need separate chunks or a tested custom snapshot/overlay implementation.

URL: https://minestom.net/docs/world/instances .

### E10 — Minestom cross-thread synchronization

Minestom's Acquirable API controls safe access to entities owned elsewhere. Synchronous acquisition can block; an asynchronous option schedules acquisition later. Acquired references must not escape the protected scope. Multithreading changes how game state must be accessed rather than eliminating coordination costs.

URL: https://minestom.net/docs/thread-architecture/acquirable-api .

### E11 — Current Geyser protocol support

On the research date, Geyser's supported-version page states Bedrock 26.30–26.52 and Java 26.3. It emulates that Java client version. Backends on older protocols can need ViaVersion; older Bedrock releases are not a general supported target. This is a moving compatibility matrix, not a permanent version guarantee.

URL: https://geysermc.org/wiki/geyser/supported-versions/ .

### E12 — NetherNet migration announcement

Geyser's October 8 announcement states NetherNet support begins with build 1237 and describes the planned October 27 switch with Bedrock 26.60. It says RakNet remains the default at publication, so transport must be configured. It also states HAProxy headers currently work for RakNet and built-in signaling, but not NetherNet gameplay. NXS supports external signaling and load-balancing; MOTD/player-count updates through NXS are periodic.

URL: https://geysermc.org/blog/preparing-for-nethernet/ .

### E13 — Geyser transport configuration

Current setup specifies a Geyser port reachable over TCP and UDP for NetherNet. Running both transports additionally requires a distinct UDP WebRTC port. The default Geyser and Java ports cannot collide when built-in signaling consumes TCP. The setup guide recommends HTTPS signaling.

URL: https://geysermc.org/wiki/geyser/setup/ .

### E14 — Advertised addresses and transport overrides

Geyser exposes transport, signaling mode, separate signaling/WebRTC ports, and public advertised-address overrides. Built-in and NXS advertised addresses are configured separately. The documentation warns that specifying an advertised address does not configure port forwarding. NetherNet cannot bind to a loopback address in the same way as a Java backend. Resource pack paths exceeding 80 characters can prevent console players joining.

URL: https://geysermc.org/wiki/geyser/geyser-command-line-arguments-and-system-properties/ .

### E15 — Mojang's official NetherNet protocol

Mojang documents HTTP signaling followed by direct WebRTC data channels using UDP, DTLS, and SCTP. Signaling is only involved in initial negotiation. The current client configuration disables TCP ICE candidates and uses reliable and unreliable channels. Full ICE gathers candidates before signaling. The guide specifies verification of the client's signed identity assertion before negotiation, binding authenticated identity to DTLS fingerprints. Thus early authenticated admission is a documented protocol capability, while protected signaling alone does not protect the resulting gameplay path.

URL: https://mojang.github.io/bedrock-protocol-docs/guides/nether-net-onboarding-guide/ .

### E16 — HTTPS and external signaling

Geyser supports serving HTTPS itself or using a reverse proxy for signaling. HTTPS avoids the client's first-use trust prompt. External signaling may provide certificate management. Open Collaboration describes NXS registration, capacity heartbeats, short-lived encrypted admission tickets, and reporting join outcomes. This can inform a custom Metallic control plane without inventing cryptography or replacing gameplay transport.

URLs: https://geysermc.org/wiki/geyser/nethernet-https/ ; https://opencollaboration.dev/articles/nethernet .

### E17 — Floodgate authentication boundary

Floodgate can live at the proxy. Backend API use requires additional setup and identical key material between trusted Floodgate instances. Its setup guide explicitly warns that exposing the key permits abuse of the authentication boundary. Do not copy the network's identity secrets into arbitrary community-controlled server containers.

URLs: https://geysermc.org/wiki/floodgate/setup/ ; https://geysermc.org/wiki/floodgate/api/ .

### E18 — Native Bedrock forms

Geyser/Floodgate expose Cumulus forms: modal confirmation, image/button lists, and custom inputs such as text, toggles, dropdowns, and sliders. Response handlers support valid, invalid, and closed responses. A unified server-browser operation can render as a native Bedrock form rather than relying on mouse-specific inventory gestures.

URL: https://geysermc.org/wiki/geyser/forms/ .

### E19 — Java menus

Paper documents Dialog API support beginning in 1.21.7 for the Minecraft dialog feature added in 1.21.6. It supports custom in-game menus and input, including during connection configuration. Use capability-aware rendering and a fallback for clients/protocol adapters without the required features.

URL: https://docs.papermc.io/paper/dev/dialogs/ .

### E20 — Crossplay limitations and content

Geyser lists limitations that cannot be universally translated, including distinguishing left/right inventory clicks, glowing effects, certain custom recipes and Java-specific behaviors. Some visuals are supplied by its integrated resource pack and selected issues have Hurricane workarounds. Custom items, blocks, skulls, icons, and entities can use packs and mappings; Geyser does not automatically convert Java packs. Test actual assets and gameplay rather than assuming Java content becomes correct Bedrock content.

URLs: https://geysermc.org/wiki/geyser/current-limitations/ ; https://geysermc.org/wiki/geyser/custom-content/ .

### E21 — Anticheat and consoles

Geyser's compatibility list distinguishes anticheats that check Bedrock from those that merely ignore it; it labels the list community compiled and displays different verification dates. This is a shortlist for testing, not evidence that a product currently detects every exploit. The console guide describes third-party connection workarounds and reliability caveats; custom-server access must be checked on actual target consoles.

URLs: https://geysermc.org/wiki/geyser/anticheat-compatibility/ ; https://geysermc.org/wiki/geyser/using-geyser-with-consoles/ .

### E22 — ViaVersion placement

ViaVersion connects newer clients to older server versions. ViaBackwards and ViaRewind cover additional older-client cases. The project recommends avoiding installation simultaneously on proxy and backends and reports higher state accuracy with direct Paper access. Its release policy includes a delay after some Minecraft protocol changes, which is a reason to verify the complete compatibility matrix before rollout.

URLs: https://viaversion.com/setup ; https://github.com/ViaVersion/ViaVersion .

### E23 — Async chunk preparation

Paper recommends asynchronous teleportation when destination chunks are unloaded, avoiding synchronous chunk loads on the main thread. It warns against calling get/join on that future on the main thread because this can deadlock. Metallic should preserve scheduler ownership when composing asynchronous work, particularly across its Paper and Folia adapters.

URL: https://docs.papermc.io/paper/dev/entity-teleport/ .

### E24 — Resource controls and maintenance

Linux cgroup v2 provides hierarchical process resource controls. Resource quotas can support fair multi-tenant hosting, but do not by themselves isolate arbitrary plugin code from credentials, kernel interfaces, or private networking. Separately, Geyser's 2025 early-packet DoS report documents a real application-level issue patched in build 897 and later, reinforcing the need to keep the full protocol stack updated.

URLs: https://docs.kernel.org/admin-guide/cgroup-v2.html ; https://geysermc.org/blog/early-packet-handling-attack/ .

## Original engineering proposal: one platform, three engine choices

| Engine choice | Initial implementation | Best workload | Compatibility promise | Promotion condition |
| --- | --- | --- | --- | --- |
| Metallic Community | Pinned Paper distribution plus Metallic plugins and narrowly justified patches | User-created survival, creative, economy, existing plugin servers | Published tested plugin/version matrix; preserve supported Paper API contracts | Behavioral regression suite and measured tail-latency/capacity result |
| Metallic Regions | Folia plus explicitly audited Metallic and third-party plugins | Players separated among islands, plots, worlds, or distant SMP regions | Only the documented Folia-compatible set | Region transfer, cross-region interactions, commands, world saving and plugin checks |
| Metallic Arena | Minestom-based owned game runtime | Short matches, lobbies, controlled rules, repeatable arenas | Metallic game API; no Paper/Bukkit compatibility claim | Full required mechanics and crossplay tests plus baseline comparison |

Velocity remains the session router. It is not a tick engine competing with Paper or Folia. A network can have thousands of players across many independently hosted game instances without claiming that one heavily interacting vanilla world can hold them all on one machine. This distinction should appear in dashboards and future benchmark claims.

For the parent-reported EPYC 7451 (24 physical cores/48 threads, about 125 GiB RAM) and i7-9700 (8 cores, about 15 GiB), evaluate placement using measurements. These are supplied prior-session specifications, not live inventory confirmed in this task. A wide EPYC node is a plausible candidate for many independent workloads; the i7 is a candidate for latency-sensitive individual instances. Do not infer player capacities from core counts or allocate one giant heap merely because RAM exists. Reserve CPU, memory, storage I/O, and network headroom for the OS, proxies, backups, and burst work. Folia's published threading estimates are rough; benchmark the actual NUMA topology and container CPU affinity.

### 1. An explicit execution contract for Metallic games

Define a small API for owned code: schedule work on an entity owner; schedule work at a location; read an immutable state snapshot; submit bounded background computation; return a proposed state change to its owner; publish a durable command with an idempotency key. Adapt these operations to Paper, Folia, and Arena internally.

World mutation belongs to its owner. Database, network, AI, compression experiments and large file reads should not block a tick callback. A snapshot result must carry the state revision it was computed from; stale results are discarded or revalidated before application. Teleportation and transfers must have cancellation and failure semantics, including disconnects and shutdown.

Existing plugins that mutate arbitrary state synchronously retain their existing execution environment. A scheduler wrapper cannot make unknown shared mutable state safe. Nor can a watchdog safely stop arbitrary Java code at an arbitrary point and guarantee consistent inventory/world state. Budget enforcement for owned work should occur at cooperative boundaries; enforcement for unknown plugin workloads occurs through admission, isolation, and maintenance restarts with rollback, not fabricated thread safety.

### 2. Admit work before a server becomes overloaded

Develop a placement controller using recent tick-time distributions, CPU throttling, memory pressure, measured outgoing bandwidth, queue depth, and predicted startup cost. A server is eligible only when the workload fits all relevant limits. Reserve capacity for active matches. Send new matches to another ready instance; queue fairly if none is ready. Avoid constantly moving running matches, which would trade overload for transfer churn.

Publish separate maximums for owned game players, active community servers, storage allowance, and network traffic. A sleep policy can reduce idle cost, but warm pools consume resources even with no players. The dashboard should show what is actually reserved and available. Free access is a product policy; hardware, electricity, storage, transit and protection still have limits.

### 3. Warm the player's next destination

Keep a small measured pool of ready lobbies and popular arenas. Pre-generate finite maps. Preload destination chunks before a match or transfer. Cache immutable metadata and content locally. Predict demand using queue depth and near-term schedules before attempting more elaborate ML.

For destructive games, isolate mutable block data between matches. Immutable template sharing is useful only where mechanics allow it; Minestom SharedInstance is not copy-on-write isolation. Save template/version hashes so every match can be recreated for debugging. Measure cold-start and warm-start experiences separately, including resource pack download time.

### 4. Build a semantic interface layer

Define actions such as browse games, create server, choose template, edit access, invite party, confirm purchase, restore backup and report abuse independently from their presentation. Render them as Java dialogs/inventories or native Bedrock forms according to capabilities. Design one game-information model with edition-specific visuals.

Use large selectable targets, controller-friendly focus order, short labels and explicit actions. Avoid making right-click, hover text, a glowing entity, or chat tab-completion the only way to discover or execute a critical action. Support translations, reduced motion, subtitles where relevant, readable contrast and a fallback when a pack is declined. Every submitted action is reauthorized at execution time, so reopening an old menu cannot reuse expired privileges.

### 5. Give content a single source of truth with two tested outputs

Create a content manifest containing stable item IDs, behaviors, collision requirements, translations, models, textures, sound events and quality levels. Build separate Java and Bedrock packs and Geyser mappings from that manifest where the formats support them. Put hand-authored exceptions in the same manifest instead of scattered plugin code.

Validate missing mappings, invalid identifiers, excessive paths, missing textures, pack acceptance, visual fallbacks, animation bounds, GUI item visibility and client collision consistency. Keep product screenshots or small reference recordings for each edition and input type. Some mechanics should have different presentation while sharing authoritative results; a conversion script cannot erase protocol differences.

### 6. Separate identity, tenancy and community code

Keep platform accounts keyed to verified stable identities and explicit links rather than visible player names. Linking should require proof of control and preserve audit/history information for inventory, purchases, bans and creator ownership. Source IP can aid abuse detection but is not a user identity, especially after relays and carrier NAT.

Treat community-installed JAR files as arbitrary executable code. Curate plugins initially; isolate each customer workload's files, resource budgets and network reachability. Do not put central service credentials or network-wide forwarding/Floodgate keys into community-editable environments. Keep Bedrock forms and identity-sensitive features at a trusted gateway or provide a deliberately narrow authenticated service interface. Review any per-tenant proxy arrangement for cross-tenant impersonation before exposing custom plugins.

### 7. Turn current NetherNet into an admission opportunity

A custom Metallic signaling service could implement the documented NXS or HTTP signaling interfaces: verify supported authenticated identity, check bans and capacity, choose a healthy regional gateway, return only protected reachable addresses, and emit a short-lived admission result. Reuse vetted identity/cryptographic libraries and standard protocols.

Protect both signaling and gameplay endpoints, validate returned SDP address sets against an allowlist, and verify actual outside-client packet paths. Do not blindly inject PROXY headers into WebRTC gameplay. Test independent signaling and gameplay failures. A TLS certificate, successful MOTD, or successful DNS lookup is not a successful protected play session.

### 8. Make anticheat reflect game rules and edition differences

Use authoritative checks for inventory conservation, interaction range, cooldowns, rates, ownership, movement bounds and game-specific constraints. Gather telemetry on a new check before enforcing punishments. Test ordinary touch, controller and keyboard play, latency spikes, knockback, vehicles, flight modes and cross-edition collision differences.

Do not label an anticheat Bedrock-capable because it avoids kicking Bedrock users by exempting them entirely. Measure false positives as well as exploit detection. Store bounded evidence for review and appeals. Server-side limits remain important even when a particular movement model needs broader tolerances. Do not trust client-reported input type as a privilege or security bypass.

### 9. Reduce avoidable latency while respecting distance

For an interaction confirmed by the authoritative server, decompose delay into client/input processing, upstream transit, edge queueing/translation, tick scheduling, server computation, downstream transit and display. Measure the stages separately. A protected edge near the player does not move an origin simulation closer; a poor relay choice can increase distance.

Choose actual match compute by player-region mix and measured path health. Place static assets near downloaders. Batch persistence outside ticks with correct durability boundaries. Minimize unnecessary proxy hops and redundant protocol conversion. Maintain frame-time and particle/entity budgets on clients in addition to server tick budgets.

Within owned games, immediate visual feedback and appropriate reconciliation can make actions feel responsive, but do not show unconfirmed economic success or pretend packets arrived before they did. A 20 TPS scheduler offers a 50 ms cadence; with uniformly distributed arrival, the wait until the next tick averages 25 ms in a simple model. This is a model, not a measured Metallic delay. Increasing tick frequency for the entire vanilla simulation can change behavior and cost; prototype higher-frequency processing only in controlled game subsystems and validate actual client behavior. Geographic propagation cannot become zero.

### 10. Build transaction-safe transfers and recovery

An instance transfer is a state transition with a source, destination, identity, state revision, and unique transfer ID. Only one location owns a player's mutable inventory/economy at a time. Reserve the destination, prepare data, commit ownership once, then complete presentation. Timeout paths should reconcile ownership rather than simply retrying a reward or copying inventory twice.

Test disconnects at every stage, delayed acknowledgments, duplicate messages, a destination crash, a proxy restart and stale cached state. Make balances and items conserve their expected totals across the test. Short-lived minigames can often recover by replay/rejoin or abandon-and-refund rules rather than attempting a complex continuous world migration. Define the product rule before implementing recovery.

### 11. Apply adaptive visual budgets with gameplay invariants

Classify updates as gameplay-critical, informational, or cosmetic. Preserve authoritative collisions, enemy visibility and combat information. Reduce distant cosmetic particles, duplicate scoreboard updates and unnecessary decorative entity updates when budgets are tight. Track per-client outgoing bytes and frame-time effects where observable.

Use deterministic per-mode ceilings rather than unbounded effects. Have the art pipeline estimate worst-case simultaneous effects during a full match. A visually spectacular empty lobby is not proof that the same scene stays smooth with many players. Let players select a reduced-effects mode and remember that choice by platform/account.

### 12. Make engine improvement a controlled experiment

For every proposed patch, record a specific bottleneck, evidence, hypothesized improvement, behavioral invariants and rollback. Keep the smallest patch set necessary and track upstream commits so fixes can be absorbed instead of accumulating an unmaintainable fork. Measure each change separately before combining several.

Possible experiments include eliminating redundant allocations found in a profile, precomputing immutable game metadata, replacing repeated global scans with maintained indexes, smarter match placement, batching cosmetic packet updates, improving async chunk preparation and content cache behavior. Do not promise these are wins before testing. A custom Java server distribution is credible because its behaviors and measured results are clear, not because its name conceals upstream projects.

## Benchmark and evidence-gate design

### What one benchmark record must identify

Include the exact repository commit, build artifact hash, Paper/Folia/Velocity/Geyser/Floodgate/ViaVersion versions, Java build and flags, plugin hashes, effective configuration hashes, operating system/kernel, CPU model/topology, RAM, storage model, cgroup quotas/affinity, thermal/power state where available, load-generator host, world/template hashes, generated versus unexplored chunks, view/simulation distances, workload script version, random seed and protocol/client types. Describe background load. Timestamp the run.

Use a bounded workload record with offered load, completed actions, failed actions, concurrent active players, bot behavior, actual client validation count, duration, warmup, repetitions and termination criteria. Keep raw timestamped samples or a suitable histogram artifact, logs, spark profiles and correctness results alongside summaries. A hash proves artifact identity, not that reported measurements are honest; retain enough provenance to review collection.

### Required metrics

| Area | Record | Why it matters |
| --- | --- | --- |
| Simulation | p50/p95/p99 and selected p99.9 tick duration, deadline-miss fraction, longest stall, per-hot-region results | Network averages can hide one unplayable match or region |
| Player experience | join completion, input-to-authoritative-result, transfer completion, first playable frame where measured | TPS alone does not measure the user's wait |
| JVM | allocation rate, heap occupancy after GC, pause distribution, CPU utilization | Prevent a short warm test hiding memory churn/leaks |
| Host | CPU throttling, scheduler delay, memory pressure, disk latency/queue, NIC/drop counters | Detect shared-host interference and actual bottlenecks |
| Network | RTT/jitter/loss distributions, proxy and translation delay, bytes and packets per active player, handshake failures | Identify route and transport costs |
| Correctness | item/economy conservation, persistence hashes, duplicate rewards, cross-tenant access failures, command/plugin compatibility | Faster broken behavior is a failed experiment |
| Cost/capacity | active player-hours and completed matches per reserved resource budget, idle reserve | Compare efficiency at a defined service quality |

Quantiles require enough observations and repetitions. Do not treat every tick as an independent experiment; report variation across runs and realistic sessions. Randomize baseline/candidate order where practical. Use identical workloads and unchanged gameplay for a performance comparison. Label estimates and synthetic traces clearly; synthetic validator fixtures cannot support a real throughput claim.

### Workload matrix

1. **Dense lobby:** players grouped together, skin/profile joins, menus, chat, visibility, party actions and typical cosmetics.
2. **Distributed SMP:** players widely separated, stable pregenerated movement, inventories, farms and ordinary building.
3. **Exploration burst:** the same population crossing into previously ungenerated chunks; distinguish generation from loading.
4. **One hotspot:** players deliberately meet in the same area, stressing the limitation hidden by a distributed-region average.
5. **Owned combat arena:** movement, projectiles, knockback, damage, scoring, repeated rounds and teardown/reset.
6. **Complex mechanics:** representative redstone, fluids, hoppers, entities, portals and the actual supported plugin set.
7. **World-edit/import task:** large bounded creator edits concurrent with normal play, measuring fairness and progress.
8. **Multi-tenant contention:** one heavy workload alongside healthy ordinary servers; verify isolation and fairness.
9. **Lifecycle:** cold start, warm start, idle suspension, restore, failover, crash during save/transfer and destination capacity loss.
10. **Crossplay matrix:** Java supported versions; Bedrock current Windows, Android/iOS and console journeys; NetherNet and transitional legacy paths; pack accept/decline/slow download; controller/touch; high latency/loss on an isolated authorized test network.

Idle synthetic connections are only an ingress/memory test. In-server fake entities omit parts of client networking. Scripted protocol clients omit renderer, controls, some client behavior and real human play. Use the combination honestly. Load generation should run separately enough not to steal the CPU/network capacity being measured. All stress tests belong on owned isolated infrastructure with bounded stopping conditions; this report proposes no external-target load generation.

### Promotion gates

Each workload class defines its own baseline, hard correctness requirements, target and maximum capacity. Suggested initial targets for evaluation are p99 tick time below 50 ms at the admitted load, a lower p95 target to preserve headroom, no new correctness failure, and no meaningful regression in join/transfer time or resource cost. These numbers are proposed engineering goals, not achieved results, and should be set from observed baseline/noise rather than automatically accepted.

The gate must reject missing measurements, mismatched baselines, insufficient duration, absent client coverage, unexplained configuration differences, failing conservation checks, non-finite/negative metrics, synthetic data marked as measured, or a comparison with a weaker correctness/visual workload. It should output pass, fail, or insufficient evidence with a reason and the exact artifact identities. A schema-valid JSON file is not a passing benchmark.

For a Paper-to-Folia move, require a per-plugin support record and functional tests beyond the opt-in flag. For a Paper-to-Arena move, define the intended mechanic subset and validate all of it; no performance conclusion should be drawn by removing required behavior from the candidate. Do not publish claims against Minehut, Minekeep, Hypixel or another network without comparable authorized observations, clear workload definitions and appropriately narrow wording.

## Suggested delivery sequence and release policy

**Phase 1: establish a trustworthy baseline.** Inventory live hardware/network and software; confirm current transport support; collect peak and ordinary profiles; add the benchmark manifest and validator; implement one native crossplay server browser; publish which parts exist and which remain prototypes. A health check must separately test Java login, Bedrock signaling, actual Bedrock gameplay and an end-to-end backend transfer.

**Phase 2: ship a small excellent platform slice.** One owned game and one creator server template with a working create/join/invite/stop/restore loop. Add bounded scheduling, authenticated account mapping, limits, observed performance, clear queues, a warm pool and trust isolation. Confirm that backups restore useful worlds.

**Phase 3: evaluate engine specialization.** Test the representative SMP on Paper versus audited Folia, and the owned game on Paper versus Arena. Migrate only the winner for its specific workload after correctness gates pass. Keep known-good template/artifact versions and a rollback route.

**Phase 4: introduce original engine patches.** Pursue measured hotspots and reusable APIs. Prototype NUMA-aware placement, workload-sensitive batching, higher-frequency owned game processing, content overlays and more advanced prediction in the lab. State the workload and measurements for every claimed improvement.

For releases, pin production artifacts, test scheduled upstream updates in staging, and maintain a fast path for confirmed security fixes and protocol deadlines. Do not let every instance download an untested latest snapshot on boot. Canary by small traffic cohorts or one recoverable server, monitor the same metrics as the benchmark and roll back on defined degradation. Minecraft world format changes need backups and migration planning; downgrading a world is not equivalent to swapping a JAR. Full active-match/world migration across regions should remain a later research project with explicit consistency and failure semantics.

## Implementation status

A benchmark evidence-screening CLI is now included in this change set. See `../benchmark-gate.md` for the exact schema, verified behavior, and limitations. It does not run a workload or independently derive performance metrics. No present evidence establishes that Metallic is faster than another server or hosting provider.
