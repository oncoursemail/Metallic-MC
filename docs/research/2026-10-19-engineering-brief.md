# Metallic MC and Star AI: engineering brief

Prepared for Alex Fisher. Research and limited read-only observations: 19 October 2026 UTC. Implementation author: ChatGPT, working against the existing private repositories. This brief distinguishes implemented candidates, observed facts, engineering proposals, and deployment prerequisites. Pricing is public advertising on the research date, not a purchase or an account-specific quote.

## 1. Direction and concrete deliverables

The strongest route is a protected, measurable Minecraft network with original games and a creator platform, supported by a more responsive Star AI. Metallic should own the game experience, creator workflow, admission and placement policy, observability, compatibility contracts, and recovery. Maintained upstream transports, cryptography, and runtime components provide the foundation. There is no credible code-only method that creates unlimited upstream bandwidth, removes propagation delay, or makes arbitrary plugins run concurrently while preserving every existing behavior.

This change set makes four pieces concrete. Star's existing public-page reader now applies its deadline to DNS and text extraction, runs blocking work in bounded dedicated pools, and preserves capacity ownership after cancellation. Metallic gains an offline edge configuration audit and a benchmark evidence-screening CLI. A responsive creator interface demonstrates world templates, draft creation/export, joining states, three original game concepts, and edition-specific menu layouts. The research appendices provide deeper network, engine, and platform analysis with primary-source links.

| Deliverable | State and practical value | Remaining boundary |
| --- | --- | --- |
| Star fetch responsiveness | Implemented candidate in the existing `fetch_page` path; 61 focused tests passed independently. | Two broader integration tests excluded in the partial inspection copy; no full-app or live speed result. |
| Edge configuration audit | Implemented offline CLI with strict parsing, transport/address/boundary checks and local evidence digests. | No provider purchase, network scan, firewall inspection or live mitigation verification. |
| Benchmark screen | Implemented matched-run and trace-identity checks; synthetic evidence cannot pass. | It does not collect traces, recompute asserted percentiles, authenticate the collector or certify a release. |
| Creator interface | Working local HTML prototype, desktop/phone QA, JSON draft export and functioning interactions. | It does not provision servers or implement Minecraft-native menus or playable games. |
| Architecture and research | Three detailed repository appendices plus this brief. | New runtime patches, orchestration services and production deployment remain staged work. |

The existing Metallic foundation is on `feat/foundation-control-plane`; its default branch contains only a README. Other open branches already propose control surfaces and moderation policy. This work adds distinct files and a separate preview, preserving those changes. Star already has resource planners, routing, caching, context and NUMA-related modules, plus current repair/research PRs. Creating another disconnected planner would add complexity; the fetch fix improves an actual shared call path.

## 2. What the machines actually show

Read-only samples around 05:03-05:12 UTC identify useful capacity constraints. These short observations are not a Minecraft capacity benchmark or a complete container inventory.

| Host | Observed resources | Implication |
| --- | --- | --- |
| `starai` | AMD EPYC 7451, 24 physical / 48 logical CPUs, four NUMA nodes; 128,574 MiB total RAM, about 45,882 MiB available in one sample. Root filesystem about 90% used, about 23 GB available. | Preserve headroom and investigate disk growth before adding large model/world artifacts. Forty-eight logical CPUs are not forty-eight independent physical cores. |
| `alexbuz1` | i7-9700 host, about 15,773 MiB RAM and 3,665 MiB available in one sample. Root filesystem about 25% used. Short CPU pressure sample around 7-10%; memory and I/O pressure were zero in that interval. | A useful candidate for separate workloads, with modest memory headroom at the observed moment. Actual game capacity needs measurements. |

Both machines had substantial swap occupancy, but the short sampling interval did not show sustained swap-in/out or memory pressure. Occupied swap alone is not evidence of current thrashing. Linux PSI measures resource-related stall time; combine it with workload latency, I/O and memory observations before changing allocations. [R01]

Star's main inference process already uses `--numa distribute`. Inspecting only its main thread initially looked restrictive, but all worker threads and memory placement showed work across the four NUMA domains. That is existing optimization to preserve. No CPU-affinity change, swap clearing, model restart, service restart or filesystem cleanup was performed. These actions would require a measured reason and a recoverable maintenance plan.

The accessible host namespace did not show the expected Java game listener during the sample. This does not prove that no game server exists in another container or namespace. Docker inventory on one host was denied by the account's socket permissions, and that boundary was respected. The actual public domains, router forwarding, provider account, Geyser build, client regions and loaded uplink performance remain unresolved deployment inputs. No secrets were inspected.

## 3. Star AI: improve responsiveness, then measured quality

### The integrated improvement

The old reader created a 15-second deadline, but awaited DNS outside it and parsed HTML on the event loop. A stalled resolver could keep a caller waiting beyond the stated budget; extraction could delay unrelated async work. The candidate patch propagates the same monotonic deadline through DNS, redirects, HTTP reading, decoding and extraction. Four dedicated DNS workers and two dedicated parser workers bound submitted blocking work; admission fails clearly when a pool is occupied. The underlying concurrent future retains its slot until the blocking operation actually finishes.

This ownership detail matters: cancelling a coroutine does not kill a running OS resolver or parser. Releasing its slot immediately would let successive timed-out calls accumulate hidden work. Python documents running-future cancellation and timeout cleanup behavior, and HTTPX's per-operation timeouts are distinct from one cumulative application deadline. [R02-R04]

The public-address checks, destination pinning, TLS SNI/Host handling, redirect checks, byte/type limits, and untrusted-page framing remain intact. The focused tests use mocked HTTP and deterministic blocking events. They establish deadline/cancellation behavior and continued event-loop progress; they do not measure real websites, model tokens per second, or a percentage increase in intelligence. An ordinary GIL-enabled Python worker thread also does not turn parsing into unrestricted CPU parallelism.

### The next quality and speed program

1. Record queue time, time to first useful output, input/prefill tokens, output tokens, tool latency, retry work, and final task success separately. A fast first token can still precede a slow or wrong completed task.
2. Route short classification, extraction and UI work to the configured fast model; reserve deeper reasoning for tasks that need it. Use uncertainty and evaluator outcomes to escalate. Avoid routing every message to every model.
3. Reserve foreground capacity. Pause or admit indexing, batch experiments and long research according to observed CPU, memory, disk and game-quality budgets. Scheduling policy must account for both Star and Metallic on shared machines.
4. Reuse stable prefixes and scoped retrieval results where their identities match. Cache keys need project/user scope, source revision/content, model/tokenizer version, policy and relevant settings. Revalidate time-sensitive evidence. Prefix reuse primarily reduces prefill work, not the cost of generating every new output token. [R05-R06]
5. Make research auditable: fetch source pages, attach observation dates, distinguish claims from inferences, record conflicts, and stop when key questions have enough evidence. More parallel searches do not automatically mean a better answer.
6. Use parallel agents for independent bounded investigations, with one synthesis owner. Measure the additional token/tool cost and latency. Published multi-agent research reports benefits and substantial overhead in its own workload; its results are not a Star benchmark. [R07]
7. Repair incrementally: keep the working checkpoint, identify the smallest failing boundary, modify it, run relevant checks and preserve rollback. Recreating a whole feature after a small error wastes time and increases regression risk.
8. Evaluate on fixed tasks with objective acceptance criteria, held-out cases, security-boundary checks and human GUI review. Star must not rewrite the tests or answer key to certify itself. Track regressions by task type and device as well as aggregate scores.

Before tuning inference concurrency, compare one and two simultaneous foreground requests with realistic prompts and sustained background load. Measure p95/p99 completion and total resource use, not only aggregate throughput. Test context and cache budgets rather than assuming maximum values help. Existing llama.cpp features are candidates to evaluate on the actual host; switching to vLLM or SGLang is not an automatic improvement for a CPU-oriented deployment. [R05-R06]

## 4. DDoS protection: where it must happen

Reverse DNS maps an IP address to a hostname. WireGuard authenticates and encrypts a tunnel and contains handshake-level denial-of-service safeguards. Neither supplies the upstream network capacity required to absorb a flood before it saturates the origin's access link. Metallic's practical design is upstream filtering, a protected public ingress, and an authenticated private path to the world servers. [R08-R09]

Run the client-facing Geyser and Velocity components at the protected edge where practical. They should advertise protected edge addresses. Carry accepted backend traffic over WireGuard or a suitable provider private transport, restrict backend listeners to the intended private peers, and apply the supported player-identity forwarding mode. Keep control APIs, RCON, panels, databases and tenant traffic in separate authenticated boundaries. Velocity modern forwarding does not replace a firewall. [R10]

The edge is not the only possible exposure. Review IPv4 and IPv6; A/AAAA/SRV records and prior direct hostnames; signaling/ICE/STUN candidates; map/status/voice services; resource-pack downloads; administrative endpoints; failover destinations; and egress from tenant plugins. Arbitrary code can call an attacker-controlled endpoint and reveal the host's public egress address. A tenant execution boundary therefore needs a deliberate egress policy as well as incoming-port rules. Docker-published traffic can bypass UFW's ordinary filtering path, so inspect the actual container-forwarding rules. [R11]

If a home address is already known, a new proxy cannot erase that knowledge. An attack can still target the access circuit directly. Persistent exposure may require ISP cooperation, an address change with a complete exposure review, or placing critical public gameplay/state in protected datacenter infrastructure. Local machines remain useful for private workloads, development, builds and measured game capacity; their RAM cannot compensate for a saturated ISP link.

### October 2026 Bedrock transition

Geyser's October 8 notice says Bedrock 26.60 is planned for October 27 and will stop using RakNet. Geyser builds from 1237 support NetherNet, with configuration needed according to that notice. Recheck actual releases before deployment. The target must support NetherNet TCP signaling and UDP gameplay. Geyser's built-in `both` mode additionally needs a separate UDP WebRTC port. [R12-R14]

HTTPS signaling and services such as NXS/Warden can help connection setup; signaling alone is not a gameplay relay or DDoS scrubber. Explicitly audit the advertised game endpoints. The current Geyser notice also distinguishes PROXY support on legacy/signaling paths from NetherNet gameplay support. An old Bedrock forwarding recipe is not enough to establish current source-IP handling or protection. [R12, R15]

### Realistic provider shortlist

| Option | Advertised on research date | Recommendation for Metallic |
| --- | --- | --- |
| TCPShield Free | $0; 1 TB/month; one network, three domains. Geyser is listed on Premium at $100/month, not the free tier. | Useful Java candidate. Residential-origin restrictions and the home-host guide matter. Confirm current NetherNet coverage separately. [R16-R17] |
| playit | Four free port allocations; current homepage marks NetherNet Premium. Premium advertises $3/month and 16 ports. | Possible personal alpha candidate. Written permission is required for hosted/managed/multi-tenant redistribution; it is not an automatically authorized free hosting backbone. [R18-R20] |
| OVHcloud US VPS | VPS-1 from $4.54/month: 2 vCores, 4 GB RAM, 500 Mbps. VPS-2 from $8.50/month: 4 vCores, 8 GB, 1 Gbps. Generic anti-DDoS is included. | A small protected-edge candidate to evaluate, not a confirmed NetherNet solution. The separate Game DDoS offering excludes VPS. Validate protocols, region, quotas, suspension policy, CPU and tenancy terms. [R21-R22] |
| Cloudflare Spectrum | Paid Java support, with advertised allowances and overage pricing; current limitations explicitly exclude Bedrock. | Useful web products can protect the portal/assets, but this is not the unified near-free Java/Bedrock answer. [R23] |

A proposed $10-20/month edge budget is a procurement target for a small alpha, not a verified all-in protection quote or the cost of operating a public hosting company. The initial choice should follow actual player geography and whole-path testing. A cheap nearby edge can be preferable to several distant chained proxies. The network appendix also evaluates NeoProtect, Path and TCPShield Sentry without inventing unverified prices.

For scale, negotiate explicit Java and NetherNet coverage, multi-tenant rights, clean-traffic pricing, quota behavior, abuse handling, support and failure policy. Do not disable protection or expose the origin to escape a quota. Two brands do not guarantee independent failure domains. DNS/ingress failover can help new joins; an existing TCP/WebRTC session or world state does not magically migrate when its edge fails.

### Where our own protection software can add value

Build deterministic, bounded admission around the protected ingress. Separate budgets for status queries, new authenticated joins, established sessions and each tenant prevent one workload from exhausting every queue. Validate supported identities and inexpensive protocol state before allocating expensive session resources, preserving Mojang/Geyser semantics. Use short internal capacity leases rather than a database lookup on every packet. Do not invent incompatible client challenges or new cryptography.

Keep telemetry bounded under high-cardinality traffic. Use sampled distributions and capped counters so the monitoring system cannot be exhausted by unique identities or addresses. Measure complete route quality, hold a session on its chosen route, and add hold-down periods before shifting new joins. Degrade expensive optional work first: pause exports and pregeneration, pace new joins and lower cosmetic traffic while preserving game rules. Version policies, canary changes, observe legitimate-join failures and restore the prior policy if quality degrades. Star can explain anomalies and propose reviewed changes; it should never sit in the real-time packet path.

The new edge-audit CLI is the first small part of this control layer. It refuses unknown/contradictory configurations and checks local evidence identities. Even its strongest result is `configuration_consistent`, with `protectionVerified: false` and `liveNetworkTested: false`. Hashing a provider screenshot identifies the bytes; it cannot establish that the provider protects the declared endpoints. Live network tests and provider confirmation remain necessary.

## 5. Our server software: one platform, three runtime lanes

Metallic can have a distinctive software platform without committing every world to a premature full rewrite. Use a common identity, world-lifecycle, menu, telemetry, placement and revision contract across three deliberately different execution lanes.

| Lane | Appropriate workload | Compatibility constraint |
| --- | --- | --- |
| Paper-compatible creator worlds | Familiar survival, ordinary plugins and the first curated templates. | Pin runtime/plugin builds. Paper 26.1+ currently requires Java 25; an old Java 21 recipe is not the baseline for current versions. [R24] |
| Audited Folia worlds | Distributed SMP/skyblock-style worlds whose plugins follow region ownership. | Widely separated work can execute concurrently; dense gatherings remain hotspots. Support flags alone do not prove correct plugin behavior. [R25-R26] |
| Metallic Arena on Minestom | Owned, bounded minigames with explicitly implemented mechanics. | Minestom is a framework, not a complete vanilla/Paper replacement. Bukkit plugins and all survival mechanics do not arrive automatically. [R27] |

The original engineering layer should expose entity/region-owned scheduling, immutable read snapshots, bounded asynchronous work and version-checked results applied on the owning thread. A plugin must not modify another region's mutable state because a worker happens to be available. Template manifests should specify supported API/runtime versions, allowed capabilities, content hashes and expected workload costs.

Optimize the measured hotspots: pregenerate bounded areas; separate chunk generation from loading; stage imports and saves; remove blocking database/HTTP work from tick paths; control entity tracking and cosmetic updates; reuse maps where semantics permit; bound queues and logs; and tune memory/GC only after observing pauses and allocation. An arbitrary synchronous Java callback cannot safely be preempted merely because it exceeded a policy budget. Curated plugins can cooperate with budgets; untrusted code needs a process/VM boundary and recovery strategy.

Preserve inventories, currency, claims and persistence through transfers. Use idempotent operations, explicit transaction identifiers and one current owner for a world volume. An expired lease must fence an old writer so a retry cannot start two processes on the same world. Backups need a separate failure domain and verified restore, not just a successful upload message. Do not downgrade a changed world format as though rollback were only swapping a JAR.

### What the benchmark gate establishes

The new CLI requires at least three matched baseline/candidate repetition pairs with explicit engine identities, fixed environment/workload/client/gameplay fingerprints, timestamps, warmup, measurement duration, summaries and local trace descriptors. Version 1 allows an engine-artifact change; changing hardware, settings and plugins simultaneously is a different experiment. It checks bounded files and their digests, and rejects incomplete, mismatched, future, invalid or explicitly synthetic evidence.

Its initial screening policy proposes at least 120 seconds warmup and 600 seconds measurement per repetition, 10,000 tick and 1,000 interaction samples, candidate p99 tick time no higher than 50 ms, deadline misses no higher than 1%, and p99 regression no higher than 5%. These are initial engineering thresholds, not measured service promises or a statistical power calculation. Passing screening does not mean a candidate is faster; per-pair deltas report that separately. It does not recompute the declared metrics from raw records or authenticate collection. A trusted collector and experiment review are the next dependency.

Use dense lobbies, distributed players, one deliberate hotspot, exploration bursts, complex mechanics, combat, creator edits, multi-tenant contention, joins/transfers and save/restore failures. Include active real/protocol clients and real-device playtests, with Java and supported Bedrock editions. Idle bot connections primarily exercise ingress and memory. A convincing engine result must preserve the same promised mechanics and resource envelope, report repeat variation, and show no new correctness failure.

## 6. Lower latency and better crossplay

Latency has several components: client input/rendering, propagation, route detours, queuing, gateway/translation work, server scheduling and game simulation. Zero latency cannot be achieved. The useful objective is low, stable tail latency under admitted load and immediate understandable feedback where the client protocol supports it.

Choose ingress and game placement from measured whole-path RTT, loss and jitter. A nearby proxy does little if it forwards to a distant overloaded world. Test loaded and idle uplink delay, then evaluate supported queue management and traffic scheduling on the actual router. Avoid assuming a tunnel MTU; verify large datagrams and long sessions through the selected IPv4/IPv6 path. A change that improves average ping while creating packet-loss spikes is a failed improvement.

Pregenerate likely destinations, load a party's map before transfer, and maintain a small measured warm pool for popular templates. A warm generic JVM does not instantly contain every persistent world. Tell players when they are queued, when a world is waking and when it is ready. For owned minigames, offer immediate permitted audiovisual acknowledgement and clear interactions, while the server retains authority over objectives, economy and outcomes. New stock-client prediction behavior cannot be assumed without a supported protocol or optional client component.

Build one logical menu/action schema, rendered as a modern Java dialog, a native Bedrock form and an inventory fallback for older supported Java clients. Paper and Geyser/Cumulus expose the relevant native surfaces. [R28-R29] Validate every action on the server against permission, context, nonce and revision; a visible button never grants a capability. Test touch target size, controller focus, keyboard navigation, text expansion, safe-area layout, reduced motion and slow pack downloads.

Use Floodgate's supported identity path and explicit Java/Bedrock account-linking rules. Separate identity namespaces to avoid accidental name collisions, and keep private forwarding keys at the correct boundary. Test anticheat against actual version/input behavior; do not create an unverified blanket exemption or assume the editions behave identically. Resource packs need tested edition-specific outputs, mappings and fallbacks. Console connection paths have platform constraints, so publish the supported journey instead of promising universal direct custom-server entry. [R30]

## 7. Creator platform and original games

The current competitor baseline is stronger than old comparisons suggest. Minehut advertises 1 GB RAM, 10 players and 10 GB storage with no playtime limit, and its updated policy allows five owned free servers with one online. MineKeep advertises the same basic free resource/player/storage limits, no playtime cap, daily backups, crossplay and discovery; paid plans also mention AI features. These are advertised features, not equal-hardware performance measurements. [R31-R33]

Metallic's better opportunity is the whole creator workflow: choose a tested template, customize a draft, preview a separate revision, invite collaborators, validate changes, publish, discover, diagnose, restore and export. Offer clear quality limits, understandable performance explanations and fair discovery for new creators. A repair assistant should propose a bounded change against a clone, show the evidence, and preserve rollback before changing a published world.

The standalone preview demonstrates four templates: Friends survival, Community SMP, Parkour studio and Arena workshop. The draft has an explicit non-provisioned state and can be exported as JSON. The joining sequence is deliberately simulated; the eventual backend must authenticate, deduplicate a wake request, reserve measured capacity, acquire/fence a volume lease, start one instance, pass game-aware readiness and transfer the player. Repeated requests must not create duplicate servers or charges. Empty worlds save safely before hibernation, and restored worlds are checked in a separate copy before promotion.

| Original concept | First playable scope | Why it is a useful first product |
| --- | --- | --- |
| Foundry Relay | One short industrial route, two cooperative roles, an energy-core objective and a clean round reset. | Tests team coordination and mixed-input readability within a bounded map/entity budget. |
| Rift Expeditions | A compact room set, one enemy family, a risk/reward branch and an extraction decision. | Tests party instances and progression without beginning with a huge persistent MMO. |
| Circuit Works | A constrained obstacle/logic palette, checkpoints, revision publishing and a timed run. | Proves the creator workflow, rollback and community discovery through a playable creation tool. |

These are original proposals, not uniqueness claims or finished games. Prototype Foundry Relay or Circuit Works first and choose production scope from playtests and player return behavior. Hypixel, Mineclub and DonutSMP are useful ambition references; no reproducible comparative performance evidence was established that would justify announcing Metallic superior to them.

### Trust, moderation and operating cost

Separate operator-owned games, curated creator worlds, arbitrary plugin hosting and external listed servers. Begin the free tier with curated templates/plugins. Arbitrary uploaded Java code must not inherit host mounts, privileged containers, host networking, Docker sockets, service credentials or neighboring tenant access. Evaluate strengthened isolation such as a dedicated VM/microVM pool before opening unrestricted uploads. Docker cgroups are resource controls, not proof that hostile code is harmless. [R34]

Use an existing supported panel API behind a narrow adapter where it already solves console/files/backups/schedules. Pterodactyl currently labels 1.x stable and 2.0 beta; Pelican's inspected release was a 1.0 release candidate, so do not describe it as a proven stable replacement. Agones becomes useful when Kubernetes fleet management is justified by actual operations; it cannot create physical capacity on a fixed host. [R35-R36]

Limit memory, CPU, disk, I/O, processes, packet rate, egress and log growth per tenant; keep operator reserve. Fair discovery should reward successful sessions and retention, not bot counts. Add roles, revocable invites, reports, appeals and reviewable moderation actions. Isolate test/community economies from trusted ranked rewards. Align hosting and monetization with current Minecraft rules, and budget for moderation, storage, backups, support and abuse as real operating costs. [R37]

The free tier is a funded entitlement, not zero cost. Its capacity is the minimum allowed by memory, CPU/tick quality, disk/IOPS, bandwidth, storage and the actual failure reserve. Sleeping worlds still consume storage and backup capacity. For scale intuition only, sustained 1 Mbps for 30 days is about 324 GB decimal; a 1 TB allowance averages only about 3.09 Mbps over that month. This is arithmetic, not a measured player traffic rate. Measure bytes per active player-hour and forecast spend before advertising capacity.

## 8. Delivery sequence and acceptance evidence

| Stage | Concrete work | Evidence required to advance |
| --- | --- | --- |
| A: reviewable foundation | Land reviewed fetch fix, offline audits, source ledger and creator preview; preserve existing work. | Relevant tests and review; no claim of live protection or gameplay. |
| B: protected private alpha | Select one nearby provider with explicit current Java/NetherNet and tenancy coverage; deploy one protected edge and private backend path. | Actual assigned endpoints, provider terms, firewall/IPv6/egress review, legitimate Java and Bedrock joins, MTU and loss checks. |
| C: complete creator slice | One curated template with create, invite, join, idle/save/sleep, restore and export. | Duplicate-wake and crash recovery tests, ownership fencing, readiness, quota isolation and independently restorable backup. |
| D: one original game | A bounded Foundry Relay or Circuit Works prototype with native menu adapters. | Mixed-device playtests, deterministic round reset, fair input handling and measured resource/tail-latency envelope. |
| E: controlled engine experiments | Profile actual workloads; compare matched Paper/Folia or owned Arena implementations. | Trusted raw collection, independent correctness results, repeated performance comparison and rollback rehearsal. |
| F: public creator beta | Expand only to measured capacity; introduce discovery, collaboration, moderation and cost reporting. | Sustained quality, bounded abuse, tenant isolation, support capacity and observed retention. |

These are dependency stages, not invented delivery dates. No reliable schedule can be derived without the actual team, provider readiness, uptime constraints, game scope and baseline workload.

Before calling the network protected, establish provider-supported transport coverage and a live private-origin boundary. Verify wrong forwarding secrets, direct-origin connections and untrusted metadata fail while legitimate clients succeed. Check resource-pack and auxiliary paths. Exercise an edge/tunnel failure on owned infrastructure, record reconnect behavior and ensure failover never reveals a raw home endpoint. Any traffic-stress exercise requires provider coordination and bounded authorization; no attack was run for this report.

Before claiming a lag-free experience at a published capacity, measure p50/p95/p99 simulation and interaction latency, queue delay, join success, translation overhead, packet loss, GC, disk stalls and noisy-neighbor behavior under the admitted workload. Test long-running saves, restoration and transfers. Publish the exact workload and supported editions, the limits and the measured result. A twenty-TPS average can hide a bad p99 pause, a failed join or one overloaded Folia region.

Before releasing a smarter/faster Star, compare task success and completed-task latency using fixed evaluation tasks and unchanged acceptance criteria. The current patch closes an observed waiting problem; it does not establish a model-quality leap. Further optimization should consume the same evidence framework instead of producing impressive but unverifiable percentage claims.

## 9. Validation receipt and deployment status

Star focused reader suite: **61 passed, 2 deselected**, independently rerun on Python 3.12.14 with HTTPX 0.28.1 and pytest 9.1.1. The two excluded existing agent tests require dependencies absent from the partial inspection copy. All HTTP/DNS work in this run was mocked. The normal approval reviewer permitted asyncio's local socket self-pipe; no sandbox-specific polling code was added to production.

Creator preview: Chromium checks at 1280 x 900 and 390 x 844 passed draft creation, literal user-text handling, JSON download, joining simulation/reset, concept navigation, menu selection/actions and keyboard tabs. No horizontal overflow, page errors or external requests were observed; rendered views were inspected. This validates the local preview, not game-client behavior.

Metallic Go validation: the complete `go test -race ./...` suite passed on Go 1.27.2, and the edge packages passed again after the independent-review diagnostic fix. `go vet ./...` and `go build -buildvcs=false ./...` passed. VCS stamping is disabled only because this hydrated inspection snapshot is not a Git checkout; the initial build with automatic stamping failed to obtain VCS status. The included incomplete edge example and synthetic benchmark example are intentionally ineligible. No production performance result or protection result has been filled with illustrative data.

No production service, firewall, tunnel, DNS record, model configuration or running world was changed. No provider was purchased. The first deployment decision is one protected ingress near the actual origins/player population with explicit current NetherNet and community-hosting rights. This brief supplies a concrete shortlist, requirements, implementation candidates and acceptance evidence; it does not represent an already operating network.

## 10. Primary-source register

Sources were inspected on 19 October 2026. Provider pages describe offerings rather than independently measured Metallic performance. Current documentation should be rechecked at deployment, especially the upcoming Bedrock transition. The three companion research appendices contain additional primary references and narrower findings.

- R01: Linux pressure-stall information — https://docs.kernel.org/accounting/psi.html
- R02: Python async tasks, timeout and cancellation — https://docs.python.org/3/library/asyncio-task.html
- R03: Python concurrent futures — https://docs.python.org/3/library/concurrent.futures.html
- R04: HTTPX timeouts — https://www.python-httpx.org/advanced/timeouts/
- R05: llama.cpp server capabilities — https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md
- R06: vLLM automatic prefix caching — https://docs.vllm.ai/en/latest/features/automatic_prefix_caching/
- R07: Anthropic multi-agent research system — https://www.anthropic.com/engineering/multi-agent-research-system
- R08: Reverse DNS — https://www.cloudflare.com/learning/dns/glossary/reverse-dns/
- R09: WireGuard protocol — https://www.wireguard.com/protocol/
- R10: Velocity security — https://docs.papermc.io/velocity/security/
- R11: Docker packet filtering/firewalls — https://docs.docker.com/engine/network/packet-filtering-firewalls/
- R12: Geyser NetherNet migration notice — https://geysermc.org/blog/preparing-for-nethernet/
- R13: Geyser setup — https://geysermc.org/wiki/geyser/setup/
- R14: Mojang NetherNet onboarding — https://mojang.github.io/bedrock-protocol-docs/guides/nether-net-onboarding-guide/
- R15: Geyser HTTPS signaling — https://geysermc.org/wiki/geyser/nethernet-https/
- R16: TCPShield plans — https://tcpshield.com/plans
- R17: TCPShield home-hosting guidance — https://docs.tcpshield.com/miscellaneous/protect-a-home-hosted-server
- R18: playit current offerings — https://playit.gg/
- R19: playit Premium — https://playit.gg/support/playit-premium/
- R20: playit terms — https://playit.gg/terms/
- R21: OVHcloud US VPS plans — https://us.ovhcloud.com/vps/
- R22: OVHcloud Game DDoS scope — https://us.ovhcloud.com/security/game-ddos-protection/
- R23: Cloudflare Spectrum limitations — https://developers.cloudflare.com/spectrum/reference/limitations/
- R24: Paper requirements — https://docs.papermc.io/paper/getting-started/
- R25: Folia architecture — https://docs.papermc.io/folia/reference/overview/
- R26: Paper/Folia plugin support — https://docs.papermc.io/paper/dev/folia-support/
- R27: Minestom framework — https://github.com/Minestom/Minestom
- R28: Paper dialogs — https://docs.papermc.io/paper/dev/dialogs/
- R29: Geyser/Cumulus forms — https://geysermc.org/wiki/geyser/forms/
- R30: Floodgate API — https://geysermc.org/wiki/floodgate/api/
- R31: Minehut plans — https://www.minehut.com/server-plans
- R32: Minehut updated free limits — https://support.minehut.com/hc/en-us/articles/53703725480851-Free-Server-Limits-and-Deletion-Plan
- R33: MineKeep plans/features — https://minekeep.net/
- R34: Docker security model — https://docs.docker.com/engine/security/
- R35: Pterodactyl release channels — https://pterodactyl.io/
- R36: Agones scheduling and autoscaling — https://agones.dev/site/docs/advanced/scheduling-and-autoscaling/
- R37: Minecraft usage guidelines — https://www.minecraft.net/en-us/usage-guidelines

## Companion documents

- `2026-10-19-network-protection.md`: deeper provider, transport, origin-exposure, routing and failure analysis.
- `2026-10-19-engine-crossplay.md`: runtime contracts, profiling, workloads, client compatibility and engine experiment design.
- `2026-10-19-creator-platform.md`: competitor evidence, lifecycle, product scope, isolation, moderation and capacity economics.
- `../edge-audit.md` and `../benchmark-gate.md`: exact CLI schemas, use, behavior, tests and limitations.
- `../../tools/creator-preview/README.md` (from the repository root, `tools/creator-preview/README.md`): interactive preview scope and integration contract.
