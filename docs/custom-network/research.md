# Metallic-MC: comparative research and primary-source ledger

**Research observed on 2026-10-20 UTC.** This report supports the
[custom network architecture](architecture.md). It combines the proxy,
policy/presentation, and runtime/infrastructure investigations. The intended
product owns its gateway, authorization, presentation, session, game-service,
and lifecycle contracts. Stock implementations provide comparison targets and
temporary migration interfaces. Universal performance superiority is unproven.

## 1. Evidence rules and the fair comparison

**Pinned** means an exact source commit or checksum-verified artifact.
**Versioned** means a documentation API/version URL, without claiming immutable
content. **Moving** means documentation, a wiki, or a default branch observed on
the research date. **Dated announcement** describes what its author announced,
including future plans. **Metallic proposal** and **inference** identify our own
engineering conclusions. None of these categories is a production benchmark.

The companion [upstream-sources.json](upstream-sources.json) contains metadata
only for 36 inspected files from Velocity, Gate, BungeeCord, ViaVersion, and
Geyser. Before copying that index, every local corpus file was checked against
its SHA-256, byte length, safe relative path, and exact repository/commit URL.
No upstream implementation source is vendored by this report. Wiki/API evidence
outside that corpus is identified separately below.

| Baseline | Existing strength we must acknowledge | Fair replacement question |
| --- | --- | --- |
| Velocity | Native acceleration, state-aware transport, compression checks, backpressure | Can our complete session engine improve aggregate resource use and recovery at equivalent work? |
| LuckPerms | Contextual inheritance, temporary nodes, cached data, invalidation, explanations | Can a simpler shared contract improve tested cross-component consistency without silently changing grants? |
| TAB | Per-viewer output, placeholders, network lists, visibility handling, conflict responses | Can coordinated dependencies reduce useful output work while preserving privacy and client behavior? |
| Paper/Folia | Compatibility, real gameplay, region-aware scheduling options | Which actual worlds and plugins benefit from a different execution model? |
| Gate/ViaVersion/Geyser | Alternative routing, protocol translation, crossplay | Which interfaces can support migration while custom components earn compatibility? |

An implementation performing fewer mechanics, skipping authentication, or
delegating translation elsewhere has not demonstrated an equivalent-work win.

## 2. Gateway: existing safeguards and specific opportunities

Velocity's inspected native implementation uses libdeflate and platform-native
cryptography with fallbacks. Its connection manager uses Netty event loops,
write watermarks and optional SO_REUSEPORT listeners. These are established
optimizations; choosing Go or Rust does not itself improve them. [S01] [S02]

The inspected compression decoder normally caps inflated serverbound packets at
2 MiB and clientbound packets at 8 MiB, checks actual versus declared size, and
enforces compression thresholds. Pre-join plugin messages have default byte and
count limits of 4 MiB and 1,024. Backend writability already controls upstream
reading. Claims that Velocity lacks packet bounds or backpressure would be
incorrect. These details describe the pinned development source, separately
from our Velocity 4.3.0 build44 pilot artifact. [S03] [S04] [S05]

**Metallic proposal:** reserve packet and queued-action bytes across sessions,
projects, and the gateway before allocation. One thousand occupied 4 MiB queues
would total approximately 4 GiB before other memory; this is arithmetic, not an
observed upstream vulnerability. Bound queue age and inflate concurrency as well
as bytes. Preserve ordered gameplay; obsolete cosmetics may be coalesced, while
authoritative operations require explicit rejection or durable completion.

Modern forwarding authenticates the proxy's identity payload and requires a
correctly configured backend. The backend delegates player authentication to the
proxy, so restricted origin access and secret custody are essential. Forwarding
is not a replacement for a firewall or an encrypted inter-host transport. Sensitive
plugin channels also need source validation and explicit handling; a matching
channel name is not authority. [S06] [S07] [S08]

**Inference:** a fleet improves new-session distribution and failure isolation;
it does not move a destroyed process's live socket, cipher state, or queues.
Backend switching within a living gateway is a different operation. Java 1.20.5
transfer packets establish a new target connection; cookies are limited to 5 KiB
and persist across transfers but not full disconnects. [S09]

**Metallic proposal:** keep one ordered session owner, use immutable local policy
snapshots, and fence asynchronous results by generation. A transfer ticket binds
verified identity, target incarnation, generation, expiry, and one-use nonce.
Durable state remains server-side. The first playable gateway milestone must
complete real online login, encryption, compression, configuration/play, ordinary
movement/inventory/chat, and a backend switch. Framing and status responses alone
do not satisfy it.

Gate Lite explicitly delegates authentication and omits higher-level backend
switching and commands. Compare it as a routing baseline, separately from full
proxies. ViaVersion supports a family of translation directions and adapters;
its maintainers recommend placement with sufficient backend state for accuracy
and performance. Translation placement and exact versions belong in the
compatibility matrix. [S16] [S17]

## 3. Bedrock: exact pin, two paths, and dated plans

Geyser's October 8 announcement says Bedrock 26.60 is planned for October 27,
2026, with a move away from RakNet. It says builds from 1237 support NetherNet.
The supported-versions page observed on October 20 reports Bedrock 26.30–26.52
and Java 26.3. An announced future transition is not evidence that the future
release has shipped or that our pin supports it. [S10] [S11]

Mojang's guide separates HTTP(S) signaling from WebRTC UDP gameplay. Its
capability request is `GET /v1/join`; SDP exchange uses
`POST /v1/join/{networkId}`. Identity assertions bind authenticated identity to
the negotiated connection. HTTPS alone neither authenticates all gameplay state
nor hides the advertised gameplay endpoint. [S12]

**Exact pilot finding:** the local Geyser-Velocity.jar matches SHA-256
`32f04b02378c5c71ca496a88b45b6c607774f99f8b0b9febeb0914c7e6b73ab7`.
Its embedded properties identify 2.12.0 build1265 and commit
`02dbfaccb8be68c47c176c96c24bdbddb472b375`. That source warns that loopback cannot
form the required ICE candidate pair and that players time out, including local
clients. The check emits a warning; it does not reject startup there. This is
artifact/source verification, not a completed real-client test. [S13]

The private pilot therefore remains gated on a separate Metallic-MC container
and reviewed private nonloopback networking. Widening a shared bare-host listener
is not the prescribed remedy. A listening signaling port cannot establish
Bedrock gameplay acceptance.

NXS's original maintainers describe registration, capacity heartbeats,
identity-bound encrypted admission tickets, and independent server validation.
Tickets expire after 120 seconds; gameplay travels directly to the host rather
than through the signaling provider. Floodgate separately delegates Bedrock
identity authority through its protected key. [S14] [S15]

**Metallic proposal:** own NXS admission, approved advertisements, capacity leases,
and join-result accounting while initially retaining an isolated maintained
translator. Protect and test both TCP signaling and UDP gameplay. Rewriting
cryptography or promising DDoS immunity would not establish an advantage.

## 4. Authorization: explicit semantics, honest migration

LuckPerms API5.5 supports multiple context values and context-satisfaction modes.
Its cache API distinguishes recalculation, which can keep old data available,
from reload, which invalidates it and makes later gets wait. Results can identify
the deciding node and inheritance origin. Platform configuration also controls
inheritance traversal and context behavior. Caches, invalidation, and explanations
are not missing features. [S19] [S20] [S21] [S22]

Metallic's first local policy language deliberately has smaller semantics:
case-sensitive exact context pairs, bounded role inheritance, temporary rules
and memberships, default denial, and matching-denial precedence. It does not
implement every LuckPerms wildcard, regex, traversal, metadata, or platform
default. An importer must report unsupported constructs and run differential
queries before changing enforcement; a displayed rank is not permission proof.

Cedar documents default denial, forbids overriding permits, and evaluation-error
diagnostics. OPA decision logs identify policy revisions and support sensitive
input masking. Zanzibar addresses ordering between authorization changes and
protected content. These are precedents, not guarantees inherited by writing an
original evaluator. [S27] [S28] [S29]

**Metallic proposal:** publish one validated immutable policy document and bind
decisions to tenant, verified subject, session generation, resource incarnation,
policy version, trusted context, and validity bounds. Required context must come
from authorized issuers. Omitting a fact must not suppress a contextual denial
while leaving a broad allowance applicable.

**Inference:** a digest identifies data but does not prove freshness, issuer, or
causal ordering. A known revocation must raise a local minimum-version fence;
lost notifications require bounded leases or synchronous checks for sensitive
operations. Recheck at mutation/send time, not only queue entry. The architecture's
freshness intervals are design targets, not deployed guarantees. A restored old
snapshot cannot silently restore old privileges as current authority.

## 5. Presentation: shared dependencies without invented client capabilities

TAB already provides relational placeholders, configurable refresh behavior,
global lists, server grouping, and visibility diagnostics. Its nametag feature
handles coupled scoreboard-team properties and conflicting membership. Its
sidebar feature detects competing sidebars and can yield/recover. A custom
arbiter is not the first recognition of these conflicts. [S23] [S24] [S25] [S26]

**Metallic proposal:** render from a frozen manifest of identity, policy,
relationship, context, style, locale, protocol, and resource-pack revisions.
Compute deltas against each viewer's committed model. Reuse a fragment only
when every visibility/content dependency matches. Pairwise relations still have
pairwise cost; bounded candidate sets and safe common fragments matter more
than an attractive cache-hit number.

Visibility precedes decoration. Viewer privilege loss and target privacy changes
both invalidate views. Purge obsolete queued output, prioritize removals, and
recheck authority before transport. Hidden presence can leak through counts or
search results even after removal from a player list. Actual entity visibility
requires the backend's separate enforcement.

One connection-scoped writer owns each coupled protocol surface and relinquishes
it explicitly during handoff. Internal snapshot coherence does not make multiple
vanilla packets atomic or prove what a client has displayed. TAB documents client
limits, including the sidebar's 15 visible rows and version-specific team
behavior. A custom implementation must publish tested edition/protocol fallbacks
instead of promising arbitrary layouts on every client. [S24] [S25]

## 6. Runtime, authority, and recovery

Folia's independently ticking regions can merge as activity converges; a crowded
connected arena does not become parallel merely because other cores are idle.
Paper's integration guidance distinguishes entity schedulers from location
schedulers. Minestom is an authored-server library without default vanilla
gameplay or Bukkit compatibility; its observed README advertises 26.2. Valence
describes incomplete features and read-only Anvil support. These are specific
evaluation candidates, not interchangeable survival engines. [S30] [S31] [S32] [S33]

**Metallic proposal:** preserve a Paper compatibility profile and evaluate an
authored lobby/arena separately. A capability descriptor records engine/artifact,
protocol, gameplay module, inventory schema, world format, and checkpoint mode.
Database results return to the correct engine owner and recheck the session
generation. Shared mutable entity references across workers are not a concurrency
model.

Chubby's sequencer design places stale-owner validation at the protected
operation's recipient. PostgreSQL supplies transaction and row-lock mechanisms;
serializable conflicts require complete transaction retries. [S34] [S35] [S36]

**Metallic proposal:** reserve a destination, quiesce/checkpoint the source,
advance the generation into an intentionally unowned frozen state, then let only
the named destination claim it. At most one writer is the invariant; uninterrupted
ownership is not. A canceled frozen transfer must not revive the old fence.
Commit ownership, operation receipts, checkpoint pointers, and outbox records
under one durable transaction. Lost replies are reconciled by operation ID.

A SQL fence does not stop an old JVM writing world files. Persistent-world
failover requires executor/storage proof that the old writer cannot continue.
Separate survival inventory, match loadouts, cosmetics, and transactional
balances. Moving an item between native world state and a SQL ledger eventually
needs escrow and reconciliation; reversing write order simply changes which
crash window can lose or duplicate it.

Agones demonstrates atomic game-server allocation despite eventually observed
selection metadata. Nomad documents why two voting peers cannot tolerate one
failure. These justify atomic reservations and modest initial deployment rather
than unsupported high-availability claims. [S37] [S38]

Start with one modular authority/controller, private database, gateway/bridge,
and a bounded set of workers inside the approved project boundary. Preserve
Metallic's separate containment and Old Servers/Wings ownership. A constrained
executor accepts approved typed jobs, not arbitrary shell commands, images,
mounts, or a game-accessible Docker socket.

Linux cgroup CPU bandwidth, memory pressure limits, hard memory containment, and
device I/O limits have different meanings. PSI measures resource stalls;
system-level CPU `full` is not a useful admission signal. Admission must inspect
effective ancestor limits, current reservations, startup peaks, disk/inodes, and
fresh pressure/tick observations. A JVM heap cap is not total process memory,
and missing telemetry is not free capacity. [S39] [S40]

PostgreSQL recovery requires a valid base backup and WAL chain. Its backup
verification tool explicitly does not replace a restore test. Restic's ordinary
integrity check does not read every data pack unless the relevant read-data mode
is requested. [S41] [S42] [S43]

**Metallic proposal:** coordinate world and database checkpoints, record their
relationship in one manifest, and restore into an isolated target with a fresh
authority epoch. Exercise actual engines, ledger totals, world markers, and a
real login. Checksums alone do not prove application consistency, independent
backup custody, or successful recovery.

## 7. Experiments required before superiority claims

Pin artifacts, client/backend versions, enabled features, forwarding, compression,
native acceleration, world/seed, generation state, simulation/view distances,
entity mechanics, player dispersion, and translation placement. Record physical
versus logical cores, NUMA, runtime flags, cgroups, NIC, storage, and generator
saturation. Compare total resident/cgroup memory, not one runtime's heap against
another runtime's RSS.

| Experiment | Proposed workload and evidence; all targets unmeasured |
| --- | --- |
| Gateway capacity | Offered concurrency ladder 64/256/512/1,024/2,048 and 1/5/20/50/100 joins per second; stop at observed capacity or safety limits |
| Interactive traffic | Match idle, ordinary movement/chat/inventory, chunk-heavy travel, configuration, and transfer traces; separate useful bytes from status traffic |
| Network/authentication | Controlled RTT/loss/bandwidth; isolated authentication fixture with 10/50/250 ms delays and failures; small authorized real-account acceptance |
| Policy/presentation | Cold/warm queries, context churn, revocation, placeholder/fan-out bursts, queue age, privacy removals, packet bytes, and client behavior |
| Simulation | Equivalent engine mechanics and player distribution; report relevant region tails and worst stalls, not averages over idle regions |
| Failure/recovery | Auth/control outages, slow reader, cell kill, stale writer resumption, transfer-cut crashes, full disk, and isolated restore drills |

Use scheduled/open-loop arrivals and include queue waits, rejection, timeout,
and intended start times. Closed-loop bots reduce offered work during stalls;
wrk2 documents the resulting coordinated-omission problem. Apply that methodology
in a Minecraft harness, rather than treating an HTTP benchmark as one.
HdrHistogram provides useful distribution recording and correction mechanisms.
Do not average per-cell percentiles or subtract unrelated p99 measurements.
[S44] [S45]

Proposed runs use five minutes of warmup, fifteen minutes of measurement, at
least five repetitions, and a longer soak below measured capacity. Report
p50/p95/p99/p99.9, allocation and CPU per useful work, queue bytes/age, RSS/native
memory, tick tails, authentication stages, lost sessions, and ownership/state
errors with uncertainty. Do not load-test public authentication providers.

A candidate custom cell target is p99 proxy-only delay at most 2 ms under an
explicit 500-active-session/20-join-per-second trace with four reserved cores and
4 GiB total memory. A candidate optimization gate is 20% lower CPU or resident
memory at matched throughput with no correctness regression and no more than
5% p99 deterioration. These are decision thresholds to test, not achieved results
or promised host capacity. Local offline-core microbenchmarks cannot establish
LuckPerms, TAB, engine, or full-network superiority.

## 8. License and completion boundaries

The inspected files identify [Velocity core](https://github.com/PaperMC/Velocity/blob/5e8f92b98e6870c0118dadfdff42479d680654cc/LICENSE)
as GPLv3 and [its API](https://github.com/PaperMC/Velocity/blob/5e8f92b98e6870c0118dadfdff42479d680654cc/api/LICENSE)
as MIT, [Gate](https://github.com/minekube/gate/blob/8dc77eacd4b0458b8805f652d8081a5e9d301c77/LICENSE)
as Apache-2.0, and ViaVersion core/API as GPLv3/MIT. Their exact license/source paths
are recorded in the metadata index; ViaVersion also describes its scope in the
original README. BungeeCord's inspected LICENSE contains a commercial
software-hosting permission restriction. These are engineering distribution/reuse
constraints, not legal conclusions about a proposed commercial arrangement.
Review actual file-level terms before copying or distributing implementation.
[S17] [S18]

The delivered custom components are offline foundations. They do not yet prove
durable distributed freshness, real-client rendering, playable gateway behavior,
world-storage fencing, or measured fleet capacity. Runtime/container access and
actual client acceptance remain separate gates. Research identifies the next
tests and design contracts; it does not turn those proposals into deployed facts.

## 9. Primary-source ledger

**V** = Velocity commit `5e8f92b98e6870c0118dadfdff42479d680654cc`.
**G** = Geyser commit `02dbfaccb8be68c47c176c96c24bdbddb472b375`.
Other exact commits and per-file hashes are in `upstream-sources.json`.
All moving evidence below was observed on 2026-10-20.

| Reference | Primary source and evidence status | Specific contribution |
| --- | --- | --- |
| [S01] | Velocity native README; pinned V | Native compression/crypto and fallbacks |
| [S02] | Velocity ConnectionManager; pinned V | Event loops, watermarks, listener behavior |
| [S03] | Velocity MinecraftCompressDecoder; pinned V | Directional caps, thresholds, output validation |
| [S04] | Velocity ClientPlaySessionHandler; pinned V | Bounded pre-join plugin messages |
| [S05] | Velocity BackendPlaySessionHandler; pinned V | Writability-driven read control |
| [S06] | PaperMC forwarding docs; moving | Identity MAC, backend setup, mode compatibility |
| [S07] | PaperMC security docs; moving | Backend restrictions and forwarding limits |
| [S08] | PaperMC plugin messaging; moving | Trusted source and handled-message requirements |
| [S09] | Mojang Java 1.20.5 release; dated release | New connections, transfer intent, cookie limits |
| [S10] | Geyser announcement; 2026-10-08 | Announced 26.60 timetable and transport migration |
| [S11] | Geyser supported versions; moving | Observed 26.30–26.52/26.3 support |
| [S12] | Mojang NetherNet guide; moving | Signaling, gameplay, identity assertions |
| [S13] | Geyser NetherNetServer; pinned G | Exact build1265 loopback warning |
| [S14] | Open Collaboration NXS overview; 2026-10-15 | Ticket, provider, and gameplay separation |
| [S15] | Geyser Floodgate setup; moving | Identity-key authority and backend integration |
| [S16] | Gate Lite maintainer docs; moving | Routing-only scope and omitted features |
| [S17] | ViaVersion README; moving, corpus pinned separately | Translation placement, direction, license scope |
| [S18] | BungeeCord LICENSE; pinned corpus commit | Hosting-related permission restriction |
| [S19] | LuckPerms ContextSet; versioned API5.5 | Multi-value context semantics |
| [S20] | LuckPerms cached container; versioned API5.5 | Recalculation versus invalidating reload |
| [S21] | LuckPerms Result; versioned API5.5 | Deciding node and inheritance-origin metadata |
| [S22] | LuckPerms Velocity configuration; moving master | Traversal and context configuration |
| [S23] | TAB Placeholders; moving maintainer wiki | Relational values and update behavior |
| [S24] | TAB Nametags; moving maintainer wiki | Coupled team state and conflicts |
| [S25] | TAB Scoreboard; moving maintainer wiki | Sidebar limits and competing-sidebar handling |
| [S26] | TAB Global playerlist; moving maintainer wiki | Network visibility and diagnostics |
| [S27] | Cedar authorization; moving | Default denial, forbid precedence, errors |
| [S28] | OPA decision logs; moving | Revision-aware audit and masking |
| [S29] | Zanzibar, USENIX ATC2019; original paper | Authorization/content causal consistency |
| [S30] | Folia README; moving | Region concurrency and plugin compatibility |
| [S31] | Paper Folia support; moving | Correct entity/region scheduling |
| [S32] | Minestom README; moving | Authored engine scope and protocol statement |
| [S33] | Valence README; moving | Incomplete engine and persistence scope |
| [S34] | Chubby, OSDI2006; original paper | Recipient-side sequencer validation |
| [S35] | PostgreSQL explicit locking; current docs | Row locks and transaction lifetime |
| [S36] | PostgreSQL isolation; current docs | Serializable conflicts and retries |
| [S37] | Agones GameServerAllocation; moving | Atomic allocation versus cached selection |
| [S38] | Nomad consensus; moving | Two-peer failure behavior and quorum |
| [S39] | Linux cgroup v2; current kernel docs | CPU, memory, and I/O control semantics |
| [S40] | Linux PSI; current kernel docs | Stall measurements and CPU-full caveat |
| [S41] | PostgreSQL continuous archiving; current docs | Base backup, WAL chain, recovery |
| [S42] | PostgreSQL pg_verifybackup; current docs | Verification does not replace restore |
| [S43] | Restic repository verification; stable docs | Metadata versus actual pack-data checks |
| [S44] | wrk2 original repository; moving | Scheduled latency and coordinated omission |
| [S45] | HdrHistogram original repository; moving | Distribution recording and interval correction |

[S01]: https://github.com/PaperMC/Velocity/blob/5e8f92b98e6870c0118dadfdff42479d680654cc/native/README.md
[S02]: https://github.com/PaperMC/Velocity/blob/5e8f92b98e6870c0118dadfdff42479d680654cc/proxy/src/main/java/com/velocitypowered/proxy/network/ConnectionManager.java
[S03]: https://github.com/PaperMC/Velocity/blob/5e8f92b98e6870c0118dadfdff42479d680654cc/proxy/src/main/java/com/velocitypowered/proxy/protocol/netty/MinecraftCompressDecoder.java
[S04]: https://github.com/PaperMC/Velocity/blob/5e8f92b98e6870c0118dadfdff42479d680654cc/proxy/src/main/java/com/velocitypowered/proxy/connection/client/ClientPlaySessionHandler.java
[S05]: https://github.com/PaperMC/Velocity/blob/5e8f92b98e6870c0118dadfdff42479d680654cc/proxy/src/main/java/com/velocitypowered/proxy/connection/backend/BackendPlaySessionHandler.java
[S06]: https://docs.papermc.io/velocity/player-information-forwarding/
[S07]: https://docs.papermc.io/velocity/security/
[S08]: https://docs.papermc.io/velocity/dev/plugin-messaging/
[S09]: https://www.minecraft.net/en-us/article/minecraft-java-edition-1-20-5
[S10]: https://geysermc.org/blog/preparing-for-nethernet/
[S11]: https://geysermc.org/wiki/geyser/supported-versions/
[S12]: https://mojang.github.io/bedrock-protocol-docs/guides/nether-net-onboarding-guide/
[S13]: https://github.com/GeyserMC/Geyser/blob/02dbfaccb8be68c47c176c96c24bdbddb472b375/core/src/main/java/org/geysermc/geyser/network/bedrock/nethernet/NetherNetServer.java
[S14]: https://opencollaboration.dev/articles/nethernet
[S15]: https://geysermc.org/wiki/floodgate/setup/
[S16]: https://gate.minekube.com/guide/lite
[S17]: https://github.com/ViaVersion/ViaVersion
[S18]: https://github.com/SpigotMC/BungeeCord/blob/c2f28c7572afbc0211ecb82f437526ca4ef0b11a/LICENSE
[S19]: https://javadoc.io/static/net.luckperms/api/5.5/net/luckperms/api/context/ContextSet.html
[S20]: https://javadoc.io/static/net.luckperms/api/5.5/net/luckperms/api/cacheddata/CachedDataManager.Container.html
[S21]: https://javadoc.io/static/net.luckperms/api/5.5/net/luckperms/api/cacheddata/Result.html
[S22]: https://github.com/LuckPerms/LuckPerms/blob/master/velocity/src/main/resources/config.yml
[S23]: https://github.com/NEZNAMY/TAB/wiki/Placeholders
[S24]: https://github.com/NEZNAMY/TAB/wiki/Feature-guide:-Nametags
[S25]: https://github.com/NEZNAMY/TAB/wiki/Feature-guide:-Scoreboard
[S26]: https://github.com/NEZNAMY/TAB/wiki/Feature-guide:-Global-playerlist
[S27]: https://docs.cedarpolicy.com/auth/authorization.html
[S28]: https://www.openpolicyagent.org/docs/management-decision-logs
[S29]: https://www.usenix.org/system/files/atc19-pang.pdf
[S30]: https://github.com/PaperMC/Folia
[S31]: https://docs.papermc.io/paper/dev/folia-support/
[S32]: https://github.com/Minestom/Minestom
[S33]: https://github.com/valence-rs/valence
[S34]: https://www.usenix.org/legacy/event/osdi06/tech/full_papers/burrows/burrows.pdf
[S35]: https://www.postgresql.org/docs/current/explicit-locking.html
[S36]: https://www.postgresql.org/docs/current/transaction-iso.html
[S37]: https://agones.dev/site/docs/reference/gameserverallocation/
[S38]: https://developer.hashicorp.com/nomad/docs/architecture/cluster/consensus
[S39]: https://docs.kernel.org/admin-guide/cgroup-v2.html
[S40]: https://docs.kernel.org/accounting/psi.html
[S41]: https://www.postgresql.org/docs/current/continuous-archiving.html
[S42]: https://www.postgresql.org/docs/current/app-pgverifybackup.html
[S43]: https://restic.readthedocs.io/en/stable/045_working_with_repos.html
[S44]: https://github.com/giltene/wrk2
[S45]: https://github.com/HdrHistogram/HdrHistogram
