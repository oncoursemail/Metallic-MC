# Proving the custom network is better

Experiment specification, **2026-10-20 UTC**. This file defines future comparative
experiments. It contains no measured Velocity, Gate, LuckPerms, TAB, Paper or
Folia result. The small Go tests and microbenchmarks described in
[implementation.md](implementation.md) are evidence about the offline core only.

## 1. Claims and units

The architecture's main hypothesis is that shared ownership, policy and view
versions can reduce inconsistent behavior while bounded, coordinated admission
can improve overload recovery and resource predictability. Language choice,
fewer features, a smaller heap, average TPS and a successful TCP probe cannot
establish this hypothesis. Measure the part that is claimed and preserve the
same required behavior.

| Proposed claim | Required comparison | Primary unit |
| --- | --- | --- |
| Lower gateway cost | Metallic Gateway against pinned Velocity and Gate, same authenticated clients/backends and protocol work | CPU seconds per accepted gameplay second; total process/cgroup memory; end-to-end p99 |
| Better overload behavior | Same offered join/status/gameplay rates and same resource limits | Existing-player latency/disconnects, admitted/rejected work, queue age/bytes, recovery time |
| Faster policy lookup | Equivalent authorization queries and semantics, including explanations where required | Decision latency distribution and allocations, with compilation measured separately |
| More reliable revocation | Same published change and disconnected/reordered consumers | Commit-to-enforcement and commit-to-client-clear latency; forbidden decisions after the contract deadline |
| Less display work | Same visible content, viewers, relationships and client outcomes | Bytes/packets per viewer, compute time, queue age, allocations and visual convergence |
| Safer transfers | Identical state contract and failure schedule | Duplicate writers/commits, inventory and ledger invariants, completion/repair latency |
| Better engine behavior | Same gameplay, world, plugin/API contract, effective settings and player actions | Per-region/hotspot tick duration, action latency, misses and resource cost |
| Useful horizontal growth | Same per-player work on documented extra capacity | Aggregate admitted concurrency at the latency/correctness boundary; bottleneck location |

The first custom policy language does not implement the entire LuckPerms API.
Unsupported semantics must be marked unsupported, not scored as a faster correct
answer. The first Presence code has no packet writer and cannot be compared with
TAB's client-visible performance. The first wire decoder cannot be compared with
a complete authenticated gateway's player capacity.

## 2. Retain a reproducible evidence bundle

Every experiment records exact source commits, artifact SHA-256 values, build
toolchain, dependency locks, OS/kernel, CPU topology and frequency policy,
effective ancestor cgroup limits, memory, storage and network path. Record the
JVM build, heap, native compression/encryption support and flags for Java
candidates, and the compiler/runtime settings for Go candidates. A language
comparison intentionally changes runtime; it must declare that change.

Retain effective configuration, policy export, templates/world hashes, required
plugins and adapters, client/edition/input matrix, workload scripts/seeds,
instrumentation and collector revision. Store full immutable raw timing and
correctness records with completion markers, dropped-record counters and an
index of content hashes. Use separate load generators and verify their headroom.
Sanitize authentication data; retain identities as stable test IDs.

Measure cold startup, warm steady state and overload/recovery separately. An
initial design uses at least five matched run pairs in alternating or randomized
order, with a documented warmup, at least ten measured minutes per steady-state
run, and a longer soak after an initial pass. Increase duration when event rates
do not produce enough tail samples. These are proposed minimums, not a claim that
five pairs prove every small difference. Repetitions, rather than individual
correlated ticks, are the unit for uncertainty estimates.

Report every run and pair, p50/p95/p99, sample counts, deadline-miss fraction and
confidence intervals appropriate to repeated runs. State collection error and
lost data. Keep an unfavorable scenario visible; do not average a crowded fight
and an idle lobby into one flattering score. A claimed improvement must exceed
measurement uncertainty and have no unaccepted correctness or compatibility
regression.

## 3. Gateway and transport scenarios

Start with one actual Java client and one backend, then verify the full login,
encrypted/compressed connection, configuration/play transitions, keepalive,
movement, inventory and chat. Add a second backend and perform a real switch.
Only after this conformance gate should scripted client counts increase.

| Scenario | Controlled work and important observations |
| --- | --- |
| Ordinary play | Fixed actions and chunk demand; per-direction wire bytes, action response time, CPU, RSS/cgroup memory, connection count |
| Chunk-heavy admission | Same initial world/chunk/cache state, compression threshold and player movement; encoded/inflated bytes and join-to-play stages |
| Slow receiver | Intentionally constrained clients; queue byte/item/age bounds, read pausing, fairness to ordinary players and cleanup on disconnect |
| Authentication surge | Fixed offered rates, mock failure tests separated from real-account conformance; bounded HTTP/DNS/concurrency and no offline-mode fallback |
| Status/query flood in an owned lab | Offered rate and resource cap; established-player impact, rejection reason and recovery; not an upstream volumetric DDoS claim |
| Malformed/compressed frames | Fragmentation, overlong lengths, threshold errors, bad checksums, declared/actual size disagreement, trailing streams and disconnect cleanup |
| Configuration and compression boundary | Reordered asynchronous completions, queued packets during backend switch, stale generation completion and correct cipher/compression ordering |
| Gateway drain/failure | New-admission removal, existing session behavior, legitimate reconnect/transfer, outstanding handoff and lost-reply reconciliation |
| Bedrock real path | HTTP signaling and actual UDP/WebRTC gameplay; ICE addresses, loss, MTU, device/input cohorts, bridge switching and Java-world interaction |

The transport corpus must include valid packets near each bound as well as
invalid cases. Record differences from each pinned implementation as explicit
compatibility/security choices. Tolerating extra wire encodings or rejecting
trailing zlib data is not automatically a client-visible performance advantage.

Latency load must continue on a defined arrival schedule during stalls, rather
than quietly issuing fewer requests when the server slows down. The
[wrk2 author's explanation of coordinated omission](https://github.com/giltene/wrk2)
is the methodological reference; wrk2 itself is an HTTP tool and is not the
Minecraft workload generator. Record scheduled, sent, accepted, completed,
timed-out and rejected actions distinctly. Do not omit failures from latency
reports or count rejected gameplay as successful throughput.

## 4. Authority and Presence scenarios

Export the baseline policy and collect a representative query corpus: inherited
and direct assignments, negative rules, contextual and temporary memberships,
future starts/expiries, disabled worlds/modes, vanished players, and unknown
subjects/actions. Mark wildcard, regex, operator, traversal, metadata and plugin
semantics not yet supported by Metallic. The migration adapter must give a
specific unsupported reason and keep the original export available.

Measure three separate operations: compiling/replacing a policy; evaluating a
warm immutable policy; and propagating a committed policy change through a
running network to actual enforcement and display. A nanosecond lookup with no
I/O says nothing about database publication or partition behavior. Baselines
must receive equivalent warm-cache conditions and equivalent requested decision
explanations. LuckPerms already has cached permission data and explicit reload
and recalculate behavior; the [API documentation](https://javadoc.io/static/net.luckperms/api/5.5/net/luckperms/api/cacheddata/CachedDataManager.Container.html)
must guide the baseline adapter rather than an invented cache model.

For Presence, vary both player count and distinct viewer/target outputs. All
viewers sharing one public prefix is a different workload from many different
party, locale and vanish relationships. Measure the complete channel, including
client encoding and delivery. Preserve the same formatting, animation rate,
placeholder refresh requirements and visibility. Count skipped no-op writes,
invalidated cache entries, queued stale updates, removal ordering and bytes per
viewer. Check actual Java and Bedrock screens, not just a server-side JSON model.

Test a moderator losing a role while a display update is queued; a hidden player
changing servers; a party splitting; locale/resource-pack changes; a future
deny becoming active; and a client reconnecting while an old view completion is
still pending. Revision/digest/context/session fields must affect the cache key
and queue decision. Target-only or rank-only keys do not satisfy this contract.

The initial v1 package bounds are 256 roles, 4,096 subjects and rules, 1,024 view
candidates and 256 model rows. They are memory/complexity limits, not a 4,096
player deployment claim or client layout limit. A larger policy scope requires
an explicit implementation change and a complete-denial-preserving partition
design; silently truncating rules to fit would be a security failure.

### Proposed freshness acceptance

The architecture proposes a maximum five-second ordinary permission lease and
p99 connected propagation below one second. The future authenticated publisher
must define issue/expiry times, clock-skew allowance, minimum acceptable revision,
and recovery from a disconnected subscriber. Compare the issued lease's entire
lifetime, not a fresh five seconds starting after a delayed notification arrives.
Monotonic local elapsed time prevents clock rollback from extending use.

High-value economy, reward, ownership and administrative commits require current
authority in the transaction; they cannot use an expired or disconnected cached
allow. Display clearing must be measured at the actual client and account for
network failure. A disconnected client cannot receive a packet, so define
connection termination/rejoin behavior instead of promising impossible delivery.
These contracts are not implemented by a content digest or by `ValidUntil` in
the current offline packages.

## 5. Failure and durability matrix

| Injected failure | Required invariant or explicit outcome |
| --- | --- |
| Repeated reserve/claim/award ID | One logical effect or a stable conflict; different payload under an existing ID rejected |
| Source paused before freeze | It remains the only owner; target cannot claim |
| Source paused after freeze | Old generation cannot commit; a world-file writer must also be fenced before another mounts writable storage |
| Lost response after committed claim | Reconcile authoritative state; no speculative second award or automatic cancellation |
| Target dies before/after claim | Transfer remains recoverable without reviving the old fence; durable state determines next action |
| Policy events lost/reordered/duplicated | No revision rollback; lease expiry bounds permitted stale actions; sensitive commits stop on lost authority |
| Database restart/transaction conflict | Full transaction retry with scoped idempotency; no mixed owner/checkpoint/outbox records |
| Controller restart with a start job in flight | Reconcile the actual process incarnation; avoid duplicate instance/world start |
| Stale heartbeat or network partition | Missing telemetry is not proof of death or spare capacity |
| Disk full, exhausted inode or memory pressure | Admission stops predictably; durable failure is observable; active state is not replaced with empty defaults |
| Restore older combined backup | Fresh authority epoch, coherent world/database manifest, retained revocation plan and actual game/database restore validation |
| Failover of a stateful world | Refuse until runtime/storage fencing proves the previous writer cannot resume |

The offline session model currently rejects duplicate commands; it does not
cache durable success receipts. Fault-injected database integration is future
work. Never infer exactly-once network execution from an in-memory state machine
or assume a caller-supplied checkpoint digest proves durable Minecraft saving.

## 6. Engine and infrastructure experiments

Keep dense lobby, spread survival, exploration, concentrated combat, redstone or
entity workloads, creator edits, backup contention and cold instance startup as
separate scenarios. Paper, Folia and a custom Minestom mode need different
compatibility qualifications. An authored arena with fewer simulation features
may be a good product design; it is not a like-for-like Paper acceleration result.
Folia's [region design](https://docs.papermc.io/folia/reference/overview/) and
[plugin ownership requirements](https://docs.papermc.io/paper/dev/folia-support/)
constrain which work can actually proceed independently.

On shared hardware, reserve host/control/database headroom and inspect effective
cgroup ancestry. Hold quotas and affinity stable between matched runs. Capture
CPU/memory/I/O pressure, throttling, faults, network drops, queue age and disk
space alongside game metrics. Linux [cgroup v2](https://docs.kernel.org/admin-guide/cgroup-v2.html)
provides distinct control interfaces; [PSI](https://docs.kernel.org/accounting/psi.html)
measures stalls, not a promise of unused capacity. Do not tune unrelated Wings or
Old Servers to obtain a better result for Metallic-MC.

Grow offered load in documented steps and stop at the first correctness,
latency, resource or recovery gate. Derive an operating limit below the observed
failure point with tested headroom. Publish the limiting resource. A proxy may
handle more connections while a crowded game tick, UDP path, database or storage
limits the actual network. The product goal is useful, stable gameplay at the
declared workload, not the largest unauthenticated connection counter.

## 7. Existing verifier scope

The repository's [benchmark-gate v2](../benchmark-gate.md) recomputes descriptive
statistics and checks raw trace integrity for an **engine-artifact-only**
comparison. It requires matching runtime and other controlled hashes. It cannot
honestly accept a Go-versus-Java gateway comparison, a permissions language
change, a different plugin stack or a display redesign under that schema.

Keep v2 strict. A future network comparison schema must explicitly declare its
allowed component/runtime changes and include authentication, policy, transfer,
visibility, load-generation and failure evidence. The existing verifier does
not run clients or prove collection authenticity. No manually invented JSON
statistics or `measured` label can replace actual collection.

The release decision must state the exact scenario won, the measured difference,
uncertainty, remaining losses and operational tradeoffs. Until that evidence
exists, the appropriate label is **implemented hypothesis under test**.
