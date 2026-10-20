# Metallic product, discovery, and diagnostics research

Researched 2026-10-20. This memo separates documented upstream behavior from proposed Metallic behavior. Product pages are moving sources, not performance measurements. No signed-in competitor console was accessed. Mineclub's current official site could not be retrieved; an exact visual match has not been verified.

## 1. Current product baseline

| Capability | Primary evidence | Implication for Metallic |
| --- | --- | --- |
| Official games shown before community servers | Minehut's current home page separates Official Minehut Experiences, Featured Servers, and Most Popular Servers. | Put Metallic Games first, followed by friends/recently played and community discovery. Label official ownership explicitly. This layout alone is parity, not differentiation. |
| Browser file/plugin management and automatic sleep/wake | Minehut's current home page documents plugin uploads, a file manager, sleep when empty, and automatic wake on join. | Include these in the minimum usable manager, with truthful in-progress/error states. |
| Public versus hidden discovery | MineKeep describes public servers appearing in its hub while private servers stay hidden. | Visibility and execution location must be separate concepts; private hosted servers still use platform infrastructure and follow its rules. |
| Managed backups and export | MineKeep advertises daily snapshots after changes, downloadable backups, restore, and longer paid-plan retention. Minehut documents manual backup/restore, with restore replacing current server data. | Automatic backup, download, and a visible pre-restore checkpoint are required. A differentiator is a tested restore rehearsal and a precise recovery point. |
| Hibernation and authorized wake | MineKeep documents shutdown after five empty minutes, lobby waiting during startup, and whitelist/operator restrictions on waking private servers. | Coalesce simultaneous wake requests. Gate wake on authorization before allocating resources. Separate manual Stop from Hibernate so the owner controls whether join requests may restart a server. |
| External hosting | MineKeep explicitly supports connecting hardware elsewhere without moving world files, through its network/session configuration. | Public external/tunnel mode is distinct from uploading a managed copy. Do not describe a tunnel as securely hosted or remotely immutable. |
| Rules and moderation | Minehut prohibits player-count spoofing and bot use, accepts reports, and can suspend servers. MineKeep rules prohibit botting, malicious files/links, attacks, mining, and non-Minecraft computation; owners moderate their servers. | Publish platform rules with versioned acceptance, evidence, appeal, and clear public/private-hosted scope. Enforcement needs both infrastructure controls and human review. |
| Player privacy | MineKeep documents replacing player IPs before forwarding and restricting resource-pack hostnames to reduce client IP leakage. | Keep raw player network identifiers away from tenant servers; do not forward tenant-supplied pack URLs directly to clients. |

Sources: [Minehut home](https://www.minehut.com/) (retrieved Oct 20); [Minehut backups](https://support.minehut.com/hc/en-us/articles/27082434244883-How-do-I-create-a-backup) (updated Dec 19, 2024); [MineKeep home](https://minekeep.net/) (retrieved Oct 20); [MineKeep hibernation](https://docs.minekeep.net/hibernation); [MineKeep externals](https://docs.minekeep.net/external-servers); [Minehut rules](https://support.minehut.com/hc/en-us/articles/27075816947731-Minehut-Rules) (updated Oct 16, 2026); [MineKeep rules](https://minekeep.net/rules.txt); [IP masking](https://docs.minekeep.net/player-protection/ip-masking); [resource-pack whitelist](https://docs.minekeep.net/player-protection/resource-pack-whitelist).

The MineKeep home page mentions AI-feature limits in plans, but the inspected public documentation does not establish exactly which diagnostic functionality this includes. Do not claim it lacks diagnostics or that Metallic already outperforms it. Plan prices and resource tiers are unnecessary to the architecture and have not been adopted as Metallic pricing.

## 2. Metallic's proposed mode model

Model location, management, and visibility separately, then present understandable modes:

| User-facing mode | Location and trust | Platform behavior |
| --- | --- | --- |
| Local private | User computer; unmanaged | Local server manager and Minecraft launcher; no network registration, telemetry upload, mandatory compatibility component, or platform mutation of local files. |
| Managed private | Metallic host; managed | Explicit reviewed sync/import, platform terms, immutable compatibility layer, sandboxed workload, private authenticated invitation/whitelist, backups. Hidden from public discovery. |
| Managed public | Metallic host; managed | Same managed controls plus reviewed listing, network rules, discoverability, reporting, trusted population, and moderated pack delivery. |
| External public | User computer/other host; external | Explicit connection through Metallic gateway/tunnel, current network rules, trusted gateway counts only, public ingress revocable. Local disk and arbitrary code remain under the external operator's control. |

`Sync Now` should mean one reviewed checkpoint transaction: enumerate changes, resolve conflicts, snapshot the destination, upload bounded content, validate exact manifest, then activate a new generation. A continuously writable local world and cloud world must not both pretend to be authoritative. Offer **Copy to hosting** (independent branch) and **Move play to hosting** (single-writer handoff) rather than silently merging live region files. A downloadable checkpoint is the exit path back to local play.

A platform suspension acts on hosted allocations and network admission. It must not attempt to delete an owner's disconnected local copy. An external listing can be disconnected and delisted, but the platform cannot truthfully promise that a process controlled by somebody else contains undeletable plugins.

## 3. Trustworthy population and discovery

The important trust boundary is directly documented in Velocity's API. `ProxyServer.getAllPlayers()` enumerates connections on that proxy. `RegisteredServer.getPlayersConnected()` enumerates that proxy's connections to a backend. In contrast, `ServerPing.Builder.onlinePlayers(int)` changes the advertised response, and `ServerPing.Players` is explicitly a representation of what a server says it has online. Thus a server-list ping, plugin message, tenant console output, or tenant HTTP status response is not a trustworthy population authority.

Sources: [ProxyServer](https://jd.papermc.io/velocity/4.2.0/com/velocitypowered/api/proxy/ProxyServer.html), [RegisteredServer](https://jd.papermc.io/velocity/3.4.0/com/velocitypowered/api/proxy/server/RegisteredServer.html), [ServerPing builder](https://jd.papermc.io/velocity/3.5.0/com/velocitypowered/api/proxy/server/ServerPing.Builder.html), [ServerPing players](https://jd.papermc.io/velocity/4.2.1/com/velocitypowered/api/proxy/server/ServerPing.Players.html). These are API semantics, not a recommendation to mix those dependency versions.

### Proposed original population ledger

- Accept session facts only from enrolled gateway identities with per-process boot epochs and monotonic event sequences. Tenants cannot supply either the count or an authoritative session event.
- Key a current session by a stable authenticated identity namespace plus player identity, and bind it to the platform session generation. A transfer changes placement, never creates a second count.
- Count only sessions that completed authentication and admission to a specific game server. Do not count status pings, pending logins, network lobby users, reconnect queues, NPCs, or declared load-test actors as that server's playing population.
- Short freshness windows expire disconnected gateway observations. Sequence gaps trigger reconciliation from an authenticated full snapshot; stale snapshots display **unavailable**, not a retained impressive number.
- Distinguish Java UUID and Bedrock identity namespaces. Linked accounts require an explicit verified link; do not infer links from matching names.
- Handle a reconnect racing a disconnect by generation: an old disconnect must not erase the new session. Aggregation must be idempotent and deduplicate across proxies.
- Publish `connected authenticated accounts`, freshness, and edition compatibility. Authentication is evidence of account control, not proof of one human or legitimate gameplay. A paid, authenticated account can still be automated.
- Keep suspicious automation signals separate from raw connection counts. Apply bounded join rates, burst limits, per-account controls, challenge paths, abuse review, and ranking exclusion with appeal. Do not equate shared IPs with botting; households and schools share addresses.
- Offer an explicitly labeled local test mode whose synthetic sessions never enter public rankings or sponsorship statistics.

Community ranking should combine category, language, compatibility, healthy admission, recent successful joins, player retention, and fair exposure for new creators. Show paid placements as sponsored and give the owner the same auditable metric definition the player sees. Avoid one opaque descending player-count list that rewards manipulation. Separate private online presence from aggregate server counts; do not leak names through search or friend activity when their visibility policy denies it.

These ledger/ranking choices are Metallic proposals. They are not currently deployed or proof that every automated account can be identified.

## 4. Server manager and moderation controls

Recommended primary tabs: Overview, Console, Files, Backups, Compatibility, Players, Diagnostics, Access, and Activity. Every control should act through a typed operation tied to tenant, server, active generation, and idempotency key. The manager should distinguish requested action from observed process outcome.

Lifecycle states should include stopped, starting, running, draining, stopping, hibernated, crash-looped, suspended, and deleting. Readiness must come from an actual health/admission probe, not the existence of a process or an open TCP port. Persistent operation records make restarts and timeouts explainable.

Recommended behavior:

1. **Stop:** graceful save and shutdown with bounded escalation; disable automatic wake until explicitly resumed if the owner chose Stop.
2. **Hibernate:** drain, save, stop, release compute, retain durable storage; approved joins can wake it. Empty detection comes from the trusted session ledger, not a tenant-provided count.
3. **Restart:** checkpoint as configured, graceful stop, verify the prior process cannot write, start a new generation, and complete only after readiness. Backoff after repeated crashes.
4. **Suspend:** revoke admission/listing and operation grants immediately, then stop or quarantine the workload. Keep evidence and a recorded reason. Owner start/restart/sync cannot bypass suspension.
5. **Delete:** create a durable tombstone, revoke credentials/routes, and retain data for the configured recovery period before purge. Immediate irreversible purge is a separate elevated operation.
6. **Console:** in-game console input, scoped to this server; never a shell running on the host. Newline/control validation, rate limits, permissions, and audit records matter even for Minecraft console commands.
7. **Files:** tenant-root paths only, atomic staged writes, explicit protected runtime layer, storage quotas, safe archive extraction, and reject symlink/race escapes. A browser UI hiding a file is not protection.

Moderation should record actor, target, rule version, reason, evidence handles, duration, and appeal outcome. Separate server-owner enforcement from platform moderation. Do not auto-delete a world because a text classifier flags a line. Public listing metadata, thumbnails, chat reports, names, and pack uploads each need appropriate review. A connected server may still generate harmful content; infrastructure containment is not content moderation.

## 5. Crash diagnostics that produce useful evidence

Paper's official troubleshooting guide recommends backups and stopping the server before file modifications, explains stack traces and plugin dependency-aware binary search, and distinguishes watchdog and no-log crash cases. Paper bundles spark starting in 1.21; its profiling guide says the problem must be occurring while profiling. Timings is deprecated in favor of spark. Spark supports local profile output using `profiler stop --save-to-file`.

Sources: [Paper basic troubleshooting](https://docs.papermc.io/paper/basic-troubleshooting/) (May 14, 2025), [Paper profiling](https://docs.papermc.io/paper/profiling/) (Apr 19, 2025), [Paper commands](https://docs.papermc.io/paper/reference/commands/) (Sep 18, 2026), [spark commands](https://spark.lucko.me/docs/Command-Usage).

### Proposed Metallic diagnostics pipeline

Begin with a deterministic analyzer and bounded evidence. Classify known signatures into **probable causes**, not a confident causal story from any one stack frame. Separate Java heap exhaustion, native allocation/thread pressure, supervisor-reported cgroup OOM, disk exhaustion, Java bytecode/runtime mismatch, bind conflict, missing dependencies, EULA startup refusal, permission failures, malformed config, and stalled ticks. Exit 137 alone does not establish OOM.

Return a fixed code, confidence, severity, source line numbers, and safe next checks. Do not echo arbitrary log text into reports because logs can contain tokens, chat, IPs, paths, and malicious terminal escapes. Do not upload logs to external profilers by default. More advanced local analysis can correlate:

- last known-good image/plugin/config manifest against the failing manifest;
- supervisor exit reason and cgroup memory events with JVM memory symptoms;
- CPU throttling, disk latency, and tick/GC profiles;
- a dependency graph, not just one plugin name from an exception;
- isolated reproduction using a restored checkpoint and a proposed one-change experiment.

Before applying a fix, show the exact file diff, dependency impact, rollback checkpoint, and expected result. A plugin disable/update suggestion should not automatically delete a plugin or rewrite a live world. Heap dumps may be large and contain sensitive state; they need explicit storage quotas, access controls, retention, and an opt-in action. A missing class might indicate a missing dependency or an incompatible version; both remain hypotheses until a controlled test confirms it.

## 6. Resource packs and the Metallic identity

The product can pursue the polished social-server style the user likes through original Metallic artwork, legible UI, expressive cosmetics, cohesive icons, and game-specific sound design. No current official Mineclub reference was retrievable. Do not claim a verified one-to-one recreation or rename somebody else's pack. A useful first deliverable is an original asset inventory and style specification with provenance, followed by in-game previews the owner can judge.

Protocol translation does not automatically translate artwork. Geyser requires Bedrock-format packs and does not automatically convert Java packs or generate custom mappings. Its current custom-item system supports item-model mappings and legacy definitions, with edition limitations. Bedrock pack changes generally require a new connection; per-backend pack changes cannot be assumed seamless. Remote Bedrock pack delivery also has content-type and exact-length requirements.

Sources: [Geyser packs](https://geysermc.org/wiki/geyser/packs/), [Geyser custom items](https://geysermc.org/wiki/geyser/custom-items/). [Minecraft Java 1.21.9 release notes](https://www.minecraft.net/en-us/article/minecraft-java-edition-1-21-9) document major/minor pack format and metadata changes; [Java 26.3 release notes](https://www.minecraft.net/en-us/article/minecraft-java-edition-26-3) are the current release reference. Pin a supported client/asset matrix rather than assuming one ZIP is universal.

Recommended pack pipeline: original source assets -> validated build for each supported edition/profile -> automated format/size checks -> authenticated private review -> immutable content-addressed artifact -> approved platform CDN URL -> gateway-controlled pack offer. Separate a shared Metallic base pack from game-specific layers; provide a lightweight fallback and accessibility options. Choose whether a game truly requires a pack on a per-game basis, and show that requirement before joining. ViaVersion cannot make an old client render every new UI or custom item correctly.

The resource-pack URL privacy problem is material: the client contacts the resource URL, potentially revealing its address outside the gameplay proxy. MineKeep's documented allowlist is a useful baseline. Metallic can narrow the authority further by serving approved pack bytes itself, verifying content hashes, disallowing arbitrary external redirects, and isolating any pack-fetching service from metadata/control-plane networks. This is a proposed design, not an already tested protection.

## 7. Concrete implementation priorities

First: typed mode and lifecycle/admission policy; immutable required compatibility manifest; stopped checkpoint sync planner; deterministic bounded diagnostics; trusted session-count schema and freshness tests; original pack/profile manifests. Expose only capabilities that are actually implemented.

Next: durable tenant account/RBAC and operation store; isolated executor; authenticated gateway integration; streaming console/file broker; backups and tested restores; signed launcher installation/authentication; artifact pipeline; moderated discovery. A polished launcher without a safe executor would not satisfy the requested hosting promise.

Proof gates: tenant cross-access tests; denied host/metadata/peer egress; restart/suspend race tests; restore under a fresh fencing generation; corrupted/downgrade pack rejection; cross-version join tests with real clients; multi-proxy count reconciliation; bot-abuse/false-positive trials; a crash diagnosis validated against a controlled reproduction. Claims of being better should be tied to those measured outcomes.
