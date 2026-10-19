# Metallic MC: network protection and latency research

Engineering research and source ledger. Checked 2026-10-19 UTC. This is a design recommendation and evidence ledger, not a deployed-protection report. Official project/provider documentation is the factual basis; architecture proposals and acceptance thresholds below are engineering judgments. See the companion engineering brief for the limited read-only host observations; this document does not establish the live network configuration or performance.

## Decisions that materially change the plan

1. Protect traffic upstream of the residential connection. A local firewall can protect applications but cannot recover capacity after attack traffic has saturated the ISP circuit. Use a provider that absorbs/filter attacks before delivering accepted traffic to Metallic; carry the accepted traffic to private origins through an authenticated tunnel.
2. Build Metallic's policy, admission, isolation, routing, diagnostics, and rollout software. Keep mature transport cryptography and purchased/shared upstream capacity underneath it. Reverse DNS maps an IP to a name; WireGuard encrypts/authenticates a tunnel; neither supplies a global scrubbing network. WireGuard does have meaningful handshake-level DoS defenses, which is different from preventing a volumetric flood from reaching an ISP circuit. [Reverse DNS](https://www.cloudflare.com/learning/dns/glossary/reverse-dns/) and [WireGuard protocol](https://www.wireguard.com/protocol/).
3. Treat current Bedrock as NetherNet, with TCP signaling and UDP gameplay. The common older UDP-only recipe is no longer an adequate forward-looking design. Official Geyser documentation now requires both protocols on the Bedrock port for built-in signaling. `transport: both` additionally requires a separate UDP `webrtc-port`. [Geyser setup](https://geysermc.org/wiki/geyser/setup/).
4. Make origin exposure an explicit failure. DNS records, IPv6, signaling/ICE advertisements, raw administration ports, resource-pack URLs, voice services, web maps, and outbound connections from arbitrary community plugins all need consideration. An edge-only DNS record is insufficient if another feature reveals/reaches the origin.
5. Do not claim zero latency, limitless free service, immunity to every attack, or that provider marketing bandwidth is a measured guarantee for Metallic. Establish workload-specific p95/p99 results before claiming superiority.

## Urgent compatibility fact: October 2026 NetherNet migration

Geyser's project lead published an October 8, 2026 migration notice. It says Bedrock 26.60 is planned for October 27 and will stop connecting over RakNet. Geyser builds from 1237 support NetherNet, but the notice says the mode must currently be enabled. This is project guidance about an upcoming release; the future release date should be rechecked at deployment. A RakNet fallback is a transitional compatibility option, not the basis for a long-lived free tier. [Preparing for NetherNet](https://geysermc.org/blog/preparing-for-nethernet/).

HTTPS signaling is a useful quality improvement: it removes the trust-on-first-use prompt when clients connect with the certificate's domain. Geyser can serve TLS itself, or Caddy/nginx can terminate signaling. The HTTPS proxy does not relay gameplay. Avoid HTTP/3 on a UDP port reserved for NetherNet; certificate renewal also requires a supported reload/restart procedure. A DNS challenge can avoid exposing a home HTTP validation port. [Geyser HTTPS guide](https://geysermc.org/wiki/geyser/nethernet-https/).

Warden provides NXS external signaling and routing, and explicitly says that it neither relays gameplay nor conceals the game server IP. Its public service is in active development, primarily intended for personal servers, and has no SLA; commercial use should be discussed with the operator. Accordingly, Warden must not be described as free DDoS protection. [Warden](https://warden.cloud/).

There is a concrete address-leak prevention mechanism for NXS: explicit `bedrock.signaling.nxs.advertise-addresses` specifies the complete endpoint set and skips automatic STUN discovery. It expects numeric IP:UDP-port endpoints, not hostnames. Every advertised endpoint must belong to the protected edge. An IPv4 entry does not automatically add IPv6. Configure this only after confirming the actual protected UDP listener is reachable; declaring an address does not establish forwarding. [Warden NAT documentation](https://warden.cloud/troubleshooting/nat/).

Built-in signaling has its own `bedrock.signaling.builtin.advertise-addresses` list (`-DgeyserAdvertiseAddresses`); external signaling uses the NXS list instead. Review all enabled modes, including hybrid. The public WebRTC address/port can differ from a listener but the declaration does not configure forwarding. Geyser's documentation also says NetherNet cannot bind to loopback; a loopback private Java backend is a different role. [Geyser system properties](https://geysermc.org/wiki/geyser/geyser-command-line-arguments-and-system-properties/).

For the lowest-risk architecture, run Geyser and the client-facing Velocity layer on the protected edge, so the client-facing transport only discovers edge addresses. Place the persistent Java game worlds on private local origins. If Geyser must remain local, require tested source/destination-preserving forwarding or a supported relay and audit every advertised candidate. Do not assume that enabling Proxy Protocol for old RakNet automatically handles NetherNet signaling and WebRTC.

## Current options and price evidence

These are public advertised prices, not purchases or account-specific quotes. Region, duration, taxes, offers, address allocations, and current checkout terms may change the final charge. The TCPShield public plans page carries an older copyright footer; the current documentation links to it and its billing panel, so confirm checkout before committing funds.

| Option | Publicly verified price/limits | Appropriate role and remaining question |
| --- | --- | --- |
| TCPShield Free | $0/month; 1 TB monthly bandwidth; one network; three domains | Useful Java entry protection. Not a free Bedrock plan. Domain/tenant limits matter for community hosting. |
| TCPShield Pro | $25/month; 5 TB; one network; ten domains | More Java capacity; not the advertised Geyser tier. |
| TCPShield Premium | $100/month; unlimited advertised bandwidth; three networks; 25 domains; Geyser listed | Obtain explicit current NetherNet TCP/signaling/UDP support confirmation before launch. Existing Geyser setup docs describe legacy-style Bedrock forwarding. |
| playit free | Free Java, Bedrock listing and UDP; four port allocations per support documentation | Current homepage separately marks NetherNet as premium. Do not equate its general free Bedrock listing with free NetherNet support. |
| playit Premium | Advertised $3/month; 16 ports; regional tunnels; external domain support | Low-cost personal-server prototype route. Provider permission is required before making its infrastructure part of Metallic's hosted/multi-tenant offering. |
| OVHcloud US VPS-1 | Starting $4.54/month; 2 vCores, 4 GB RAM, 40 GB NVMe, 500 Mbps, included IPv4/IPv6 and generic anti-DDoS | Candidate small protected edge; benchmark Geyser/Velocity workload. It is not the separate Game DDoS product. |
| OVHcloud US VPS-2 | Starting $8.50/month; 4 vCores, 8 GB RAM, 75 GB NVMe, 1 Gbps | More headroom for an edge, still generic anti-DDoS. Need terms/protocol confirmation and actual route testing. |
| NeoProtect GameShield | Permanent free Basic Java shield; paid Bedrock/Geyser tiers; exact price/limits only in order panel | Alternative Java edge or provider comparison; do not invent an older $/GB price. NetherNet-specific support not established by inspected marketing. |
| Cloudflare Spectrum | Java offering on paid plans; 5 GB Pro or 10 GB Business monthly allowance and $1/GB overage advertised | Poor fit for high-volume, near-free gameplay; its limitations explicitly state Bedrock unsupported. |
| Path remote protection | No current public fixed price verified | Later procurement/partner option. Its documented tunnel transports are GRE, IPv4 IPIP and IPv6 IP6TNL. BGP deployments have ASN/prefix/ROA requirements. |
| TCPShield Sentry Tunnel | Price not verified | VXLAN-based accepted-traffic delivery for general TCP/UDP; investigate when operating many ports/tenants. Do not call it free or encrypted based on inspected description. |

Price and feature evidence: [TCPShield plans](https://tcpshield.com/plans); [TCPShield Geyser](https://docs.tcpshield.com/premium-features/geyser); [playit homepage](https://playit.gg/); [playit premium](https://playit.gg/support/playit-premium/); [OVHcloud US VPS](https://us.ovhcloud.com/vps/); [NeoProtect GameShield](https://neoprotect.net/gameshield); [Spectrum offering](https://www.cloudflare.com/application-services/products/cloudflare-spectrum/); [Path requirements](https://support.path.net/hc/en-us/articles/20514056134295-Technical-Requirements); [Sentry Tunnel](https://docs.tcpshield.com/vxlan/sentry-tunnel-features).

### Restrictions that prevent attractive but incorrect recommendations

**playit is not an automatically authorized hosting backbone.** Its February 20, 2026 terms require prior written consent for resale/redistribution, including hosted, managed, bundled, white-label and multi-tenant services. They also disclaim guaranteed availability. This directly applies to the Minehut/Minekeep-style part of the vision, even when the end-user tier is free. Do not route Star AI's fleet-control system through it either without confirming the broad remote-control restrictions in the same terms. [Terms](https://playit.gg/terms/).

playit's September 16 support article describes switching back to RakNet as temporary while the operator works on the free experience; at the time of that article, TCP+UDP required Premium. The current homepage still explicitly places NetherNet under Premium. Do not promise an announced future free feature as shipped. [NetherNet article](https://playit.gg/support/minecraft-bedrock-nethernet/).

**OVH's game firewall is distinct from its VPS anti-DDoS.** The provider's US Game page explicitly excludes the VPS range. Its inspected game list identifies Bedrock with RakNet cookie support, and its Java entry describes the Query protocol, not a blanket guarantee against all Java login/application attacks. NetherNet was not present in the inspected global Game page. Obtain a compatible profile or layer an appropriate application gate; do not enable an old RakNet-only allowlist against new WebRTC traffic. [OVH Game protection](https://us.ovhcloud.com/security/game-ddos-protection/).

**Cloudflare DNS and Tunnel are separate from public game protection.** Spectrum's current limitations explicitly exclude Bedrock, prohibit UDP fragmentation, and describe specific limitations for connecting Spectrum directly to Cloudflare Tunnel. Ordinary orange-cloud HTTP proxying is not a native Java/UDP game relay. General custom TCP/UDP Spectrum applications require Enterprise with a paid add-on. Use Cloudflare web products for the dashboard, resource-download and API surfaces within their terms; don't pretend those automatically carry the game port. [Limitations](https://developers.cloudflare.com/spectrum/reference/limitations/) and [protocols per plan](https://developers.cloudflare.com/spectrum/protocols-per-plan/).

**TCPShield's cap has an outage consequence.** It describes alerts near quota, an altered MOTD after hitting quota, and domain disablement at 200% or five days after the initial 100% crossing. Its accounting counts ingress to its network from both players and servers. Track clean and billed traffic, predict exhaustion, and initiate an upgrade or orderly capacity restriction before the threshold. Never silently bypass protection to escape a quota. [Billing](https://docs.tcpshield.com/billing).

## Suggested staged deployment

### Stage 0 — offline validation and measured baseline

Collect the actual public-domain inventory, existing forwarding/firewall rules, IPv4/IPv6 status, uplink/downlink under load, client regions, router capabilities, and protected-provider account/plan evidence. Inventory Geyser transport and advertised candidates. Record expected inbound ports and private backend routes. Produce a configuration report that says `configuration_consistent` or `blocked/unverified`; an offline file cannot prove live DDoS resistance.

The preflight should reject missing provider evidence, stale observations, unsupported transport combinations, arbitrary advertised IPs, public origins, unsafe administration exposure, and unverified IPv6. It should not query arbitrary hostnames, scan targets, open router ports, or assert that a provider contract exists just because a name is written in JSON.

### Stage 1 — affordable personal alpha

For a strictly personal, small alpha, compare TCPShield Free for Java and playit's currently available NetherNet option for Bedrock, with the latter's cost/terms constraints clearly understood. This is a test path, not the final community-hosting foundation. Minimize relay chaining because every detour can add delay and another dependency. Keep home inbound game ports closed where outbound tunnels provide connectivity.

An alternative, and the cleaner path toward a creator platform, is one small protected VPS carrying Geyser/Velocity and a WireGuard tunnel to local origins. Optional Java-specific managed filtering can sit before that edge if its terms and measured route are acceptable. The VPS is the public listener; the world server is not. Reserve enough memory for native buffers/OS and load-test measured player traffic before selecting its size.

### Stage 2 — public network and creator servers

Buy/contract explicit Java and NetherNet protection with tenancy permitted, predictable clean-traffic pricing, failure behavior, incident escalation, and suitable locations. Use two independent failure domains when the budget can support them. Two brands backed by the same upstream are not necessarily independent. Route new sessions to healthy edges; drain existing ones. Expect reconnects when a stateful edge dies. A second proxy cannot recreate the gameplay session/world state of a crashed server.

Give each tenant private networking and resource quotas. Only trusted gateway identities should reach backend ports. Community-supplied arbitrary plugins should not inherit host network access, cloud credentials, host mounts, Docker sockets, or unrestricted access to adjacent tenants. Use curated plugins/templates for the easiest free tier; arbitrary code needs stronger isolation and abuse controls. Separate official ranked games from experimental/community workloads so one tenant cannot consume the entire network or tick budget.

### Stage 3 — stronger origin resilience

If sustained attacks target a previously exposed residential address, the upstream proxy cannot erase that knowledge. Work with the ISP on address change or upstream filtering, and move critical public gameplay/state to protected colocation/datacenter infrastructure if the circuit remains a recurring failure point. Additional local CPU and RAM do not increase the ISP's physical access capacity. The user's local machines can continue to provide development, builds, simulation, warm capacity, and workloads appropriate to their measured connectivity.

## Security boundaries that the implementation must preserve

**Authenticate each hop.** Velocity modern forwarding uses a MAC and secret to authenticate player information, but PaperMC explicitly says it does not replace a firewall. Backend offline mode is appropriate only behind the authenticated proxy boundary. The public Java edge retains online authentication; Floodgate is the supported Bedrock identity path, with its keys kept private. [Velocity security](https://docs.papermc.io/velocity/security/) and [forwarding](https://docs.papermc.io/velocity/player-information-forwarding/).

**Preserve and constrain source-IP metadata.** PROXY protocol and `X-Forwarded-For` are metadata, not cryptographic proof by themselves. Accept them only from explicitly trusted edge peers/interfaces and use the applicable application's support. Never enable them on a publicly reachable origin and then trust arbitrary client-supplied values. Do not blindly disable all Geyser rate limits as a performance fix; legacy proxy integrations require particular trusted-peer configuration.

**Inspect container forwarding, not just host INPUT.** Docker's official documentation says published container traffic can bypass UFW's normal INPUT/OUTPUT filtering. Wings/container allocations therefore need a rule model that covers the actual forwarding path and IPv6, verified independently. Do not flush Docker's generated chains as a shortcut. [Docker firewall documentation](https://docs.docker.com/engine/network/packet-filtering-firewalls/).

**Prevent secondary origin leaks.** Audit A/AAAA/SRV and historical direct hostnames, web maps, status APIs, public dashboards, resource-pack/skin downloads, voice endpoints and outbound tenant traffic. Egress from untrusted server code can disclose the home IP to an endpoint the tenant controls, even when all incoming gameplay is tunneled. Use a controlled egress gateway or isolated workload location where that threat exists. Cloudflare documents how a DNS-only sibling record can reveal an otherwise proxied origin. [Exposed IP documentation](https://developers.cloudflare.com/dns/manage-dns-records/troubleshooting/exposed-ip-address/).

**Keep protocol security fixes current.** Geyser's published 2025 incident response describes application-level DoS weaknesses fixed in later builds; its earlier RakNet response added early verification and rate limits. This supports maintained upstream versions plus compatibility testing, rather than a permanent fork that stops receiving fixes. The application and its state machine must be defended even behind network scrubbing. [Geyser 2025 response](https://geysermc.org/blog/early-packet-handling-attack/) and [RakNet response](https://geysermc.org/blog/raknet-amplification-attack/).

## Reduce latency where the actual delay occurs

Measure client-to-edge, edge-to-origin, server tick/queue time and client rendering separately. A close edge cannot cancel a long edge-to-world path. Anycast's route selection is not a guarantee of the geographically closest or lowest-latency path. playit's own documentation describes route variation and shows why regional ingress and the agent's chosen egress both matter. [playit latency guide](https://playit.gg/support/how-to-lower-ping/).

Engineering priorities:

- Select the edge from measured whole-path p95/p99 latency and loss for the actual player regions, not just an ICMP measurement from the server.
- Put match instances near the players when the world/game design permits it. A globally shared, strongly consistent world remains constrained by distance and synchronization; shard separate matches or worlds instead of promising impossible instantaneous global state.
- Pace initial chunk delivery, large resource transfers and backups so they do not form a standing queue in the home upload. Serve static packs separately with versioned hashes; keep latency-sensitive gameplay off large-download queues.
- Evaluate SQM using CAKE or FQ-CoDel at the controlled bottleneck and classify before opaque tunnel encryption where necessary. If many sessions are encrypted inside one tunnel, a router may otherwise see one flow. FQ-CoDel is documented to reduce queue buildup and isolate sparse traffic; it does not manufacture bandwidth or remove propagation delay. [IETF RFC 8290](https://www.rfc-editor.org/rfc/rfc8290.html).
- Test tunnel MTU rather than pasting 1420 everywhere. Account for actual outer IPv4/IPv6 headers, PPPoE and nested encapsulations. Avoid fragmentation for gameplay UDP; allow necessary path-MTU control messages. MSS clamping affects TCP, not UDP datagram size. [IETF UDP usage guidelines](https://www.rfc-editor.org/rfc/rfc8085.html).
- Use Linux native proxy transport/encryption/compression where supported. Keep native/direct-memory and heap budgets separate; never assign all container RAM to Java heap. Benchmark compression and packet batching against CPU and network utilization. [Velocity tuning](https://docs.papermc.io/velocity/tuning/).

### A bandwidth calculation to make the free limit concrete

This is arithmetic, not a measured Minecraft bandwidth estimate. Sustained 1 Mbps for 30 days transfers about 324 GB (decimal). Thus 1 TB corresponds to only about 3.09 Mbps averaged over the whole month. If a hypothetical workload averaged 100 kbps per player with 100 concurrent players around the clock, it would use about 3.24 TB before unrelated downloads and overhead. Actual per-player traffic varies strongly with chunk movement, view distance, compression and content. Measure it and forecast cost per player-hour.

## Metallic-specific innovations worth implementing

These proposals are feasible product/software work. They are not claimed discoveries or substitutes for upstream capacity.

1. **Protocol-aware preflight:** manifest each service's transport, public endpoint, private route, trusted metadata source and evidence date. Detect NetherNet/RakNet port collisions and protected-address drift before publishing.
2. **Join admission budgets:** allocate bounded per-edge/per-tenant login capacity and a reserved budget for established sessions. Use short internal capacity leases and local caches; avoid a database round trip on every packet. Signed internal tickets do not require inventing a new client protocol.
3. **Automatic degradation with clear limits:** slow new joins, pause expensive map exports/pregeneration and reduce cosmetic traffic before sacrificing established sessions. Preserve the promised game rules; never silently alter competitive hit detection or world semantics.
4. **Regional route selection:** use measured complete-path quality, provider health and backend capacity for new sessions. Include hold-down timers so small changes do not cause oscillation. DNS or NXS admission decisions do not move existing sessions.
5. **Bounded telemetry:** use counters, sketches and sampling with hard memory budgets. A flood of unique source addresses must not turn the monitoring system itself into an unbounded allocation.
6. **Reversible defense policies:** version policies, evaluate proposed changes against known-good traces and real join success, canary them on a limited route, and automatically restore the prior policy if false-positive or latency limits regress. Emergency upstream controls remain deterministic.
7. **Star AI incident assistant:** explain anomalies and prepare reviewed changes from aggregated telemetry; keep LLM inference out of the packet path. Limit its credentials by service and keep a factual action/evidence log.
8. **Tenant health budgets:** expose meaningful gauges—CPU throttling, tick delay, packet queue, memory pressure, disk latency, origin/edge reachability and quota projection—so creators know what needs fixing instead of seeing a misleading universal "lag" score.
9. **Content-aware scheduling:** prewarm a requested game template, verify the map snapshot, then admit the party together when the instance is ready. This improves perceived speed and prevents a rush of partially initialized sessions.
10. **Independent rescue entry:** maintain a separately hosted status/control surface and protected fallback entry address; never publish the home IP as a failover target. Read-only status should survive the game edge failing.

## Acceptance gates before calling a setup production-ready

These are proposed initial targets, to be tuned after baseline measurement; none are achieved results.

| Area | Gate |
| --- | --- |
| Contract/protocol coverage | Provider plan and tenancy permission established; current NetherNet signaling and gameplay supported explicitly; quota/failure policy recorded. |
| Origin confidentiality | No unintended public A/AAAA/SRV, advertised ICE/STUN origin address, resource URL or administrative path; private backend listeners; controlled egress for untrusted tenants. |
| Identity | Wrong forwarding secret, untrusted PROXY metadata and direct-origin player connections fail; legitimate Java/Floodgate identity succeeds. |
| Bedrock join matrix | Test supported current clients across desktop/mobile/consoles where permitted, both common residential NAT and mobile paths; verify HTTPS prompt behavior, packs, transfer, MOTD, reconnection and the selected transport. |
| MTU/path | Large gameplay datagrams and long sessions succeed on the tunnel, including outer IPv6 where used; no recurring black-hole timeouts. |
| Latency baseline | Publish p50/p95/p99 RTT, jitter and loss separately from tick latency for each region; target small measured edge overhead rather than an absolute 0 ms claim. |
| Loaded uplink | Aim initially for no more than 10–20 ms additional p95 queue delay during permitted expected bulk traffic; investigate router capacity if impossible. |
| Operational reserve | Forecast under configured bandwidth quota, retain native/heap/OS memory and CPU headroom, and verify recovery without disabling the firewall. |
| Capacity | Increase legitimate, owned test load in steps to the planned cohort; record objective loss/latency/tick limits and roll back on crossing. No arbitrary "thousands of players" promise. |
| Failure recovery | Kill a test edge/tunnel, verify new-join rerouting and documented reconnect behavior; restore state/backups without unsafe public-origin fallback. |
| Attack validation | Only provider-coordinated, authorized exercises with pre-agreed ceilings; include application admission/false-positive review. Do not launch a public flood to prove a marketing claim. |

## Evidence still needed from the live environment

Actual ISP uplink and location; actual domain/IPv6/port exposures; existing remote administrative routes; active game/Geyser builds; user client/region distribution; protected-provider account and plan; complete service terms for creator hosting; NetherNet filtering support from candidate providers; real p95/p99 measurements. These unknowns are why no offline validator or research report can honestly say the live Metallic server is already DDoS protected.
