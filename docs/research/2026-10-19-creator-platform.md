# Metallic MC: creator platform, game discovery, and hosting research

Research snapshot: 2026-10-19 UTC / 2026-10-18 evening America/Chicago. Prepared as internal research for the parent agent. This is a design proposal; no Metallic deployment, benchmark, account access, or competitor internal system inspection occurred. All vendor facts below come from their public primary sources. Proposed targets are explicitly targets, not measured results.

## 1. Decisions that materially change the plan

**Build an original games network and a creator platform around a measured hosting service.** A custom website, plugin library, hibernation controller, compatibility tests, and discovery system can be original Metallic software without immediately rewriting Minecraft or every infrastructure component. The first useful differentiation is a creator's entire journey: choose an experience, customize it, preview safely, invite friends, understand performance, and recover from mistakes.

**The competitor baseline has improved.** Minehut's four-hour free playtime restriction is gone. Neither crossplay, hibernation, daily backups, nor an AI label is enough by itself to distinguish Metallic. MineKeep now advertises AI-feature plan limits, so claiming uniquely AI-assisted hosting would be weak. See the verified comparison below.

**Use current release evidence, not recalled project maturity.** Pterodactyl's official site marks 1.x stable and 2.0 open beta; its latest stable release URL resolved to v1.15.1. Pelican's docs describe its forthcoming 1.0 contract, but the actual latest published release observed was v1.0.0-rc.1. Pelican has promising features; calling stable 1.0 shipped would overstate the evidence. [S08-S12]

**Modern Java clients have real native dialogs.** Minecraft Java 1.21.6 added dialogs and Paper exposes a Dialog API. Metallic can render a shared menu specification as a Java dialog, a Bedrock Cumulus form, or an inventory fallback according to the client's actual capabilities. A universal chest menu is no longer the only vanilla-compatible approach. [S17-S19]

**Safe community hosting is a different security boundary from trusted first-party games.** Do not run arbitrary uploaded Java plugins alongside Metallic's identity, currency, proxy, panel, or trusted game processes. Docker resource limits help but do not establish perfect hostile-code isolation. [S13-S15]

## 2. Verified competitor baseline

| Product | Publicly verified facts | Implication for Metallic |
|---|---|---|
| Minehut | Current Starter plan: 1 GB RAM, 10 player slots, 10 GB container storage, no playtime limit, sleeps when empty, no subusers. Current policy: own up to five free servers, one online at once. Java/Bedrock support, network discovery, DDoS protection, mods/plugins, backups are advertised. [S01-S02] | Compete on successful creation, predictable performance, collaboration, quality assurance, and recoverability. Do not market removal of a nonexistent four-hour competitor restriction. |
| MineKeep | Free plan: 1 GB memory, 10 players, 10 GB storage, shared CPU, no playtime limits, daily backups. Public hub discovery, proxy crossplay via Geyser/Floodgate, external servers, DDoS protection, downloadable backups, and paid promotion are advertised. Paid tiers mention AI features. [S03] | Free hosting and community discovery are established expectations. Measure any claimed improvement with the same workload and client mix. |
| MineKeep hibernation | Stops an empty server after five minutes. A joining player waits in the lobby and is redirected after startup. For whitelisted worlds only approved players/operators can trigger wakeup. [S04] | Wake authorization is a useful product/security pattern; it prevents strangers starting private worlds repeatedly. |
| MineKeep privacy | Documents masking actual player IPs before forwarding to creators. Also restricts resource-pack origins because an attacker-controlled pack URL could expose the player's IP. [S05-S06] | Privacy requires controlling content downloads as well as proxying the game connection. |
| Hypixel | Current support page states SMP creation requires VIP+ or above, offers quick/custom creation, invitation/member management, and in-game settings. An official developer announcement documents Housing functions, global stats, inventory layouts, event actions, and offline building; this announcement is dated 2022 and is historical feature evidence, not a complete current-limit table. [S07, S20] | A creator toolset should make reusable game logic, collaboration, and in-game editing usable. An operator-hosted vanilla SMP is a different product from unrestricted plugin hosting. |
| DonutSMP | The official store is accessible and links its server address, shop, community, and joining video. No reproducible public infrastructure benchmark or complete creator-hosting specification was established in this research. [S21] | Treat it as a gameplay/community aspiration. Do not invent a throughput, latency, or feature comparison. |
| Mineclub | Direct access to the cited official domain did not succeed through the retrieval service; searches did not establish a reliable current official feature specification. | Preserve the user's aesthetic/social inspiration, but do not assert that the original network is operating, closed, or technically inferior based on inaccessible pages. |

Price advertising is not a hardware benchmark. Terms such as Basic/Shared/Powerful CPU and unlimited slots do not specify sustained tick time, physical core entitlement, packet budget, or actual usable capacity.

## 3. Product structure: three experiences, distinct trust levels

### Metallic Originals

Operator-owned games with reviewed code, original art direction, bounded maps, and deliberate support for Java and Bedrock controls. These get reserved capacity and carefully controlled software versions. A small number of excellent games is the first release: it is easier to maintain a populated queue and polish a coherent identity than dozens of empty modes.

Suggested original game concepts, not market-uniqueness claims:

| Working concept | Core loop | Technical reason to prototype it |
|---|---|---|
| Foundry Relay | Small teams carry an energy core through a shifting industrial route, repairing switches and choosing shortcuts. | Short matches and bounded maps; role variety supports players who are less comfortable with precision PvP. Use server-authoritative objectives with locally immediate audiovisual feedback. |
| Rift Expeditions | A cooperative party enters a compact seeded dungeon, chooses risk/reward rooms, and extracts shared objectives. | Instances can be allocated per party; carefully capped entities, reusable room pieces, and fixed objectives make performance and QA tractable. |
| Circuit Works | Players build obstacle or logic courses from a constrained palette, publish a revision, and race one another's creations. | Directly exercises the creator runtime, template system, versioning, ratings, and discovery without requiring unrestricted uploaded code. |

Prototype Foundry Relay or Circuit Works first, then use real playtests to choose. “Better than Hypixel” should eventually mean evidence such as players returning, fewer abandoned joins, more fair mixed-device matches, and stable frame/tick/interaction behavior in chosen game modes.

### Creator Worlds

An accessible editor on curated runtimes: templates, tested plugins, visual event/action logic, resource-pack management, role-based collaboration, versioned drafts, and portable world exports. The default creator never needs to learn Linux or Java heap arguments to make something enjoyable.

Start with four templates: Friends Survival, Community SMP, Parkour/Challenge, and a compact Arena. Each ships a known configuration, onboarding objective, permission layout, restore point, short manual, and a compatibility manifest. Every template version specifies its runtime build, plugin versions/hashes, expected Java/Bedrock versions, pack build, and known limitations.

### Advanced Servers

Bring-your-own plugins and optional external origins are separate advanced capabilities. Arbitrary server code needs a dedicated execution boundary and a clear resource entitlement. Make them an invite-only beta until isolation and abuse controls are verified. Sharing a lobby listing must not give an external server access to internal network credentials or a trusted economy.

For a small local network, a practical first split is trusted games on their own VM/node and curated creator servers on another VM/node. Stronger per-tenant isolation is an evaluated expansion; do not assume spare physical hardware exists. If hardware supports only one host, honest fault-domain limits remain even with several VMs.

## 4. What should be custom and what should be reused

| Layer | Initial choice | Metallic's custom value |
|---|---|---|
| Operator management | Current supported stable Pterodactyl release, with a version-pinned API adapter; consider Pelican in a separately evaluated staging environment. | Own orchestration and product model do not depend on scraping a panel UI or coupling to its internal database. |
| Creator portal | Purpose-built Metallic frontend and backend, calling a narrow internal provisioning adapter. | Guided creation, verified templates, preview revisions, friendly diagnostics, safe collaboration, publish/discovery workflow, and recovery. |
| Small-fleet scheduler | A simple capacity-aware controller with durable desired state, leases, and idempotent jobs. | Hibernation, admission, placement, recoverable operations, warm instances, status streams. |
| Large ephemeral fleets | Evaluate Agones once Kubernetes is already justified by actual operating scale. | Add original-game allocation policies and public quality measurements. Agones manages game-server fleets and can coordinate scaling; it does not manufacture physical nodes on a fixed local server. [S10] |
| Matchmaking | Initially a bounded party/region queue in the platform service. | Party integrity, measured-region preferences, accessible mode selection, age of queue, fair device testing, reconnects. |
| Sophisticated matchmaking | Consider Open Match when multiple simultaneous queues/proposals and deduplication need it. | It supplies matchmaking primitives; player authentication, connections, game-server allocation, and a Director are still required. [S11] |
| Persistent storage | Per-world volume, explicit owner/lease, verified snapshots, separate backup destination. | One-click restore into a preview copy, export, published retention, world history and safe migration. |
| Pack distribution | Metallic-controlled asset gateway and immutable content hashes. | Dual-edition builds, smaller download variants, preview, provenance, rollback, privacy. |

Pterodactyl already supplies isolated containers, console/files/backups/schedules, API scoping, and server resource boundaries. Its beta extension features should not quietly become a production dependency. Pelican advertises passkeys/OAuth, administrative roles, themes/plugins, and per-node S3 or local backups, but its comparison page is a vendor self-comparison, not independent evidence that another panel lacks equivalent capabilities. [S08-S09, S12]

A panel does not automatically provide public world discovery, reliable joining during startup, Minecraft login authorization, secure cross-server transfer, world consistency, or strong protection from hostile plugins. The platform still has to implement these behaviors.

## 5. Hibernation and instant-feeling joining without false promises

Represent lifecycle states explicitly: created, validating, queued, starting, ready, draining, snapshotting, sleeping, restoring, failed, quarantined, and deleting. A proxy must know which states are joinable; the web UI must display the same state. An online TCP port is insufficient evidence that the world is ready for players.

Suggested joining sequence:

1. Authenticate the player at the trusted edge and check the world's visibility/allowlist.
2. Deduplicate repeated join requests and acquire one wake lease for the world.
3. Reserve real CPU/memory/disk/startup capacity. If none is available, return an honest queue state and maintain party placement.
4. Start the specific immutable template/runtime against its exclusively leased volume.
5. Wait for a game-aware readiness signal, required plugin initialization, pack manifest availability, and a bounded health check.
6. Register the routable backend with a short-lived revision and transfer the queued player/party.
7. On idle timeout, recheck active players, pending joins, saves, and scheduled game activity before draining and flushing.
8. Mark sleeping only after a successful save and stop. Snapshot asynchronously when appropriate, with the consistency guarantees stated in the product.

Use a lock/lease plus a fencing generation so a stalled old controller cannot remount the same persistent world after a replacement starts it. Idempotency keys should cover create, start, publish, backup, and restore. A retry must reuse or reconcile an operation rather than duplicate it. Expired leases need cleanup; failed starts must release reservations. Do not retry a crash loop indefinitely.

Two types of warm capacity are different. A prewarmed empty match server can be assigned a reviewed map rapidly. A persistent user world still needs its correct save, plugins, ownership, and initialization; a generic warm JVM does not make that arbitrary world instantly ready. Pre-pull common images and runtime files, cache template assets, schedule trusted template preprocessing, and maintain a very small measured warm pool. Increase the pool only when fewer abandoned joins justify its idle cost.

Do not market operating-system microVM boot time as Minecraft join time. The JVM, game startup, world loading, authentication, pack download, and client terrain initialization remain separate contributors.

The waiting space should preserve party membership and show what is happening, with a cancel/back option and a truthful estimated interval derived from recent samples. A short playable lobby activity is useful; fake “99%” animation is not.

## 6. One interface model across web, Java, and Bedrock

Define a typed screen schema with title, content, semantic actions, field constraints, permission requirements, localization keys, and a screen revision. Render it through capability-specific adapters:

| Surface | Suitable interface | Design constraint |
|---|---|---|
| Modern supported Java clients | Paper/Minecraft dialogs | Detect actual client/protocol capability. The presence of a new backend API is not proof that an old client can render the result. [S17-S18] |
| Bedrock via supported Geyser/Floodgate | Cumulus simple, modal, and custom forms | Use native input fields, toggles, and list buttons; avoid tiny inventory targets and hover-only instructions. [S19] |
| Older supported Java clients | Inventory/menu + chat prompts where necessary | Maintain action parity and readable labels; use pagination and clear back/cancel behavior. |
| Creator website | Responsive pages with keyboard navigation and useful mobile layouts | Show familiar tasks before log/CPU details, but expose diagnostics when the creator asks for them. |

Key screens: Discover, Play with Friends, My Worlds, Create, World Overview, Members & Roles, Templates/Plugins, Backups, Performance, Reports, and Settings. A world card should show recognizable artwork, creator identity, mode, actual online count, compatible editions, expected audience/session style, and a reliable Play action. Display operational health separately from popularity.

In-game creation should be short: choose template, give name, choose private/public, create. Advanced settings are editable afterward. Sensitive operations such as deleting a world, restoring over live data, changing owner, or expanding a collaborator's power need explicit review. Routine reversible edits should remain fast.

Never trust a client form response as authorization. Revalidate player identity, server membership, role, action scope, object ownership, expected revision, and expiry at execution time. Semantic action IDs should resolve to a permitted server action; client-provided free-form commands should not become privileged console input.

For branding: use a distinct Metallic visual language, original maps and icons, restrained motion, consistent spacing, legible typography, and optional reduced motion. Gather visual references without copying competitors' protected branding or recreating their entire interface one-to-one. Have humans test the result on a real phone/controller and modest desktop; an attractive screenshot alone is insufficient.

## 7. Creator tooling that would be worth choosing

The creator advantage is a complete versioned workflow:

- **Preview copies:** fork a world/config revision into a bounded staging session, try a change with friends, and then publish a known revision.
- **Config-aware editing:** forms know valid enum/range values, flag incompatible plugin combinations, and explain gameplay changes before restart.
- **Curated plugin catalog:** explicit licenses, version compatibility, artifact hashes, release notes, required permissions/network destinations, and rollback support.
- **Performance diagnosis:** correlate a plugin/config change with tick-time or startup deterioration; link to a proposed repair and its evidence.
- **Safe Star AI assistance:** generate a reviewable change, run it against a copy, report results, and apply only within the creator's already granted scope. Production failure should trigger a bounded known-good rollback, not unrestricted rewrites.
- **Visual game logic:** events, conditions, timers, counters, teams, checkpoints, objectives, scoreboards, and inventory presets. Compile to a constrained intermediate format with work quotas, rather than putting unrestricted scripts into a shared trusted JVM.
- **Asset pipeline:** one source asset project with separate tested Java/Bedrock outputs. Validate textures, pack metadata, client sizes, and fallback behavior; content equivalence needs verification.
- **World history:** named releases, author/audit trail, save-point notes, config diffs, tested migration paths, complete export.
- **Collaboration:** owner, developer, builder, moderator, and viewer permissions; configuration changes differ from in-game operator powers and host-level access.
- **Creator analytics:** join conversion, loading abandonment, session length, repeat visits, measured performance, and reports; aggregate data with privacy controls rather than exposing a list of player IPs or every movement.

For visual logic, cap instructions/actions per event and per interval, entities created, scheduling fan-out, maximum recursion/dependency depth, saved state, emitted network data, and synchronous work. Operations need explicit APIs; unrestricted filesystem, shell, network, and reflection are not safe default primitives. A game circuit exceeding a budget should disable the offending component and explain it rather than freeze the whole host. These are design choices, not an assertion that Java's historical SecurityManager can safely sandbox all plugins.

## 8. Fair discovery and a healthy platform economy

Offer editorial Originals, recently played, friends' worlds, small fresh creations, popular by genre, and a search/filter directory. Ranking can combine successful joins, voluntary return visits, crash rate, moderation confidence, language/region, and relevance. A fixed exploration allocation gives eligible new creators real exposure without unlimited fake-player rewards.

Use distinct sponsored placements, clear labels, and per-account budgets if promotion is eventually sold. Do not reward automated AFK time or raw connection counts as the main discovery signal; it incentivizes waste and manipulation. Deduplicate sessions carefully and cap a single world's share of discovery. IP-based signals alone are insufficient evidence of wrongdoing because households and carrier NAT can legitimately share an address.

The initial free offer should publish real concurrent-world, RAM, disk, backup-retention, upload, start-rate, and reasonable-use limits. Queue fairly under load, preserve free users' files according to a stated retention policy, and provide export before retirement. Paid reserved capacity can subsidize free worlds, but basic safety, reporting, and the ability to retrieve one's world should not depend on payment.

Mojang's current server guidance conditions monetization on its rules: competitive-advantage entitlements are constrained, cosmetics exclude cape-like products, virtual currency cannot have cash-out/real-world value or cross-server transfer, pricing and purchase history must be clear, and user privacy responsibilities remain with the operator. The product should start with simple compliant cosmetics/hosting capacity and avoid cash-out economies or speculative digital assets. Network-wide currency and creator payouts need a specific current-rule review before implementation; do not assume “one brand” makes every exchange permitted. [S22]

## 9. Hostile-code isolation and privacy by architecture

Recommended trust tiers:

1. Reviewed Metallic Originals and central services: separate secrets and privilege, signed/pinned releases, narrow service permissions.
2. Curated Creator Worlds: approved packages and bounded logic, container limits, strong network restrictions.
3. Arbitrary plugin uploads: isolated VM/microVM or a separately evaluated hardened sandbox pool; no infrastructure control socket or central credentials.
4. External servers: a defined federation/proxy protocol, limited identity assertions, revocable listing, no access to trusted data services.

Docker documents both resource control and risks from its daemon, mounts/capabilities, and kernel vulnerabilities. Only a trusted adapter should create containers. Deny privileged containers, host networking, arbitrary mounts, Docker socket access, raw sockets, host metadata endpoints, private infrastructure addresses, and unneeded outbound access. Apply memory/CPU/process/filesystem/network quotas and log limits. Separate network policy matters: containers can still reach one another if networking permits it. [S13]

Firecracker uses KVM with a small virtual-machine boundary and provides network/storage rate limits; it still needs hardware virtualization, guest management, orchestration, patching, and game compatibility work. gVisor is an alternative userspace-kernel sandbox with OCI integration, but explicitly carries compatibility and per-system-call overhead tradeoffs. Benchmark the actual Java/Netty/world-I/O workload before selecting it. Neither is an automatic zero-overhead security upgrade. [S14-S15]

The trusted network may need real IPs transiently for rate controls and incident investigation. Creator servers generally do not. Prefer tenant-scoped opaque identity/risk signals through a controlled API; if a compatibility IP mapping is necessary, design collision, rotation, abuse, and correlation behavior explicitly. A plain unsalted IPv4 hash is not equivalent to strong anonymization. Keep administrative access to real IP records auditable with a limited retention period.

Serve approved resource packs, form icons, and other client-downloaded assets through controlled domains. Reject redirects or uploads that turn the asset gateway into a request proxy for private addresses. Validate archive traversal, oversized expansion, executable content where inappropriate, allowed formats, ownership, and hashes. Don't let community plugin packets silently redirect players' clients to creator-controlled download endpoints and then claim the proxy hides their IP. MineKeep's documented pack-origin controls illustrate why this is necessary. [S05-S06]

Player-facing safety: block/mute/report, friend/invite privacy controls, ability to hide activity, platform rules distinguished from local-world rules, evidence with a retention boundary, appeal paths, and immediate containment for high-confidence abuse. AI can triage reports and summarize relevant evidence, but accusations and irreversible account decisions require meaningful checks. Moderation capacity is part of host capacity.

## 10. Capacity and cost equations; no invented hardware

The user's actual CPU models, memory, storage layout, uplink, power costs, concurrency, map sizes, backup destination, and ISP agreement were not observed. Do not translate generic RAM numbers into a promised player count.

For each candidate host, define:

- `R_total`: physical memory; `R_reserved`: OS, panel/proxies/agents, filesystem needs, and safety headroom.
- `m_i`: measured resident-memory envelope of server class `i`, including JVM overhead beyond `-Xmx`.
- `c_i`: measured CPU work needed at the chosen tick-time target and workload.
- `b_i`: measured sustained and burst network requirement including game and pack traffic.
- `d_i`: provisioned disk budget, expected growth, snapshots, and temporary update/restore needs.
- `n_i`: active instances of class `i`.

Admission must satisfy all relevant constraints: `sum(n_i*m_i) <= R_total-R_reserved`; `sum(n_i*c_i) <= usable_CPU_budget`; `sum(n_i*b_i) <= usable_uplink`; and provisioned growth plus backups and reserve must fit usable storage. These aggregate bounds are necessary, not sufficient: main-thread contention, core placement, SSD latency, cache/NUMA effects, and correlated spikes still matter. Do not count SMT threads as guaranteed physical-core equivalence.

For a single uniform template, a first conservative estimate is the minimum of memory, CPU, uplink, and storage-derived instance limits. Validate it under simultaneous joins, chunk generation, saves, and a noisy neighboring tenant. A memory-only placement strategy can give excellent-looking utilization while causing poor game ticks.

The stock of stored worlds and the number running are different. Sleeping worlds release game compute but still consume storage, backup, metadata, and operational resources. If average active-world arrivals are `lambda` per minute and average active duration is `L` minutes, the average active concurrency is approximately `lambda*L` under steady-state assumptions (Little's Law); actual peak and correlated-event distributions must determine admission headroom. Do not provision the average as the peak.

For ephemeral match prewarming, let `T_boot` be the observed startup window and `A(T_boot)` the number of allocations that can arrive within it. A design starting point is `warm_pool >= chosen_quantile(A(T_boot))`, adjusted for creation throughput and in-flight starts. This is a policy model, not a guarantee. Forecast by region/game/template and cap total warming to prevent empty capacity exhausting the fleet.

For local electricity, `monthly_kWh = average_watts / 1000 * hours_in_month`; multiply by the actual marginal electricity tariff, then add cooling/UPS losses where measured. Total cost includes connectivity, upstream protection, domains, backup storage, hardware replacement/depreciation, payment costs, and support/moderation. Existing equipment does not make these costs zero.

Useful product metrics: `cost_per_active_world_hour`, `cost_per_successful_join`, `cost_per_player_hour`, and contribution margin of reserved plans after variable cost. Model retention and world-export bandwidth; “free forever, unlimited everything” is not a capacity plan.

On fixed local hardware, Agones/Kubernetes can schedule and stop workloads but cannot add physical capacity unless separately integrated hardware/cloud capacity exists. Design an admission queue and demand limits first. A multi-region deployment requires actual compute placement or partners in those regions, not just global DNS.

## 11. Measurable release gates and staged delivery

These are proposed gates, not achieved performance:

### Stage A: private vertical slice

One protected entrance; one trusted game/lobby; one curated creator template; create/join/sleep/wake; private/public visibility; permissions; download/restore; Java/Bedrock native menu adapters; a minimal observer dashboard. Demonstrate recovery from a failed start, duplicate start, lost controller lease, full disk preflight, bad plugin/config update, and a host restart. Confirm that no arbitrary tenant can access the panel or another tenant's files/secrets. Do not open paid plans before this slice is reliable.

### Stage B: invited creators

Add the other templates, preview revisions, resource-pack publishing, operational diagnostics, limited Star-assisted repairs, public world pages, report/appeal workflow, and fair discovery. Invite a bounded cohort. Test mixed Java/mobile/controller play and collect successful-join and repeat-play measurements. Establish evidence for each resource entitlement and measure total operating cost.

### Stage C: public launch

Public free queue with explicit limits; reserved plans only if support and capacity make them sustainable; one or two polished Originals; known-good recovery automation; external backups; independent review of exposed services; compatibility matrix; published status/incident communication; rollback exercises. A single local fault domain should be described honestly in availability promises.

### Stage D: expansion justified by demand

Advance arbitrary plugin hosting, more geographic capacity, sophisticated matchmaking, Agones fleets, external server federation, advanced creator scripting, and a larger game catalog when measurements justify each. Do not simultaneously debut a new hosting business, untrusted code platform, payments economy, custom Minecraft engine, Kubernetes control plane, and many original games.

Suggested measurable acceptance questions:

- Does a new user create and successfully join a private crossplay world without instructions?
- Do cold-start/warm-join p50/p95/p99 match the displayed estimates under peak load?
- Can a developer add a tested plugin without losing world data, and undo it clearly?
- Are legitimate creators' servers protected from a neighboring CPU, memory, disk, or network hog?
- Are pack downloads, dialogs/forms, inventories, and player identity correct across the supported client matrix?
- Can an authorized operator restore an independently stored backup within the declared recovery objective?
- Are gameplay ticks and interaction success stable in the chosen maximum supported workload?
- Does discovery give new qualified creators exposure without rewarding bots or forcing ad spend?
- Are moderation load and costs within the capacity the team actually has?

## 12. Primary-source register

The URLs below identify the primary evidence reviewed on the stated date. Dates stated above were read from pages where present; crawl time is not treated as publication time. Search results from unrelated third-party domains were not used as evidence.

| ID | Source / URL | Use |
|---|---|---|
| S01 | https://www.minehut.com/server-plans | Current Starter features and storage. |
| S02 | https://support.minehut.com/hc/en-us/articles/53703725480851-Free-Server-Limits-and-Deletion-Plan | Updated Aug 3, 2026; five free owned, one online, no four-hour cap. |
| S03 | https://minekeep.net/ | Current plans/features, daily backup, external hosting, AI mention. |
| S04 | https://docs.minekeep.net/hibernation | Five-minute idle stop, authorized wake, auto lobby transfer. |
| S05 | https://docs.minekeep.net/player-protection/ip-masking | Player IP masking. |
| S06 | https://docs.minekeep.net/player-protection/resource-pack-whitelist | Resource-pack side-channel and host policy. |
| S07 | https://support.hypixel.net/hc/en-us/articles/360021570099-How-to-Create-an-SMP-on-Hypixel | VIP+ creation and native management. |
| S08 | https://pterodactyl.io/ | 1.x stable, 2.0 beta, panel and Wings/container functions. |
| S09 | https://github.com/pterodactyl/panel/releases/tag/v1.15.1 | Stable release resolved through releases/latest. |
| S10 | https://agones.dev/site/docs/advanced/scheduling-and-autoscaling/ | Packed/Distributed scheduling, fleet vs node scaling. |
| S11 | https://openmatch.dev/site/docs/guides/matchmaker/ | Match functions, Director, external authentication/allocation services. |
| S12a | https://pelican.dev/docs/comparison/ | Vendor-authored feature list, dated Oct 11, 2026. |
| S12b | https://pelican.dev/docs/versioning/ | API compatibility, latest-only support, PHP 8.4 transition; ahead of stable ship evidence. |
| S12c | https://github.com/pelican/panel/releases | Actual observed latest v1.0.0-rc.1, Oct 10. |
| S13 | https://docs.docker.com/engine/security/ | Containers, cgroups, daemon/mount isolation risks. |
| S14 | https://gvisor.dev/docs/ | Userspace-kernel isolation and tradeoffs. |
| S15 | https://firecracker-microvm.github.io/ | KVM microVM boundary and resource controls; do not reuse advertised boot as Minecraft performance. |
| S17 | https://www.minecraft.net/en-us/article/minecraft-java-edition-1-21-6 | Official introduction of Java dialogs. |
| S18 | https://docs.papermc.io/paper/dev/dialogs/ | Paper native Dialog API. |
| S19 | https://geysermc.org/wiki/geyser/forms/ | Cumulus simple/modal/custom forms. |
| S20 | https://hypixel.net/threads/housing-update-functions-global-stats-inventory-layouts-and-more.5071539/ | Historical 2022 official developer feature announcement. |
| S21 | https://store.donutsmp.net/ | Official public storefront only; not performance evidence. |
| S22 | https://www.minecraft.net/en-us/usage-guidelines | Current monetization/server guidance. |

