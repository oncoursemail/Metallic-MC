# Metallic Architect: launcher, self-hosting and network design

Status: private implementation foundation. Research checked 2026-10-19.
Authored through owner-directed ChatGPT work; this is not attributed to Star AI.

## Product contract

Metallic has one launcher with separately selected components: the original Metallic social game, Minecraft Java, and an optional Metallic Host service. Installing one does not imply installing all three. Minecraft still requires the player's own valid entitlement. Discord is optional identity linking and does not replace Minecraft authentication. The original Metallic game is a separate product, not a repackaged Rec Room executable.

Metallic Architect is our management software: identity integration, host enrollment, resource policy, catalog resolution and eventually isolated provisioning, protected routing, backup/recovery and discovery. Paper is the initial game runtime. This separation lets us build original features while maintaining plugin compatibility. A faster control API does not imply faster Minecraft ticks.

There are two hosting offers using the same management experience:

- **Use my hardware:** a server runs inside a supported, isolated host environment on the user's hardware. The user supplies CPU, RAM, disk, electricity and their home uplink; Metallic supplies account services, discovery and an authorized relay allowance.
- **Host with Metallic:** operator-managed capacity, traffic and operational support for a published price. Players never need to own the original Metallic game to play Minecraft.

The free tier is a product subsidy with quotas, not an assertion that upstream traffic or DDoS mitigation costs nothing.

## What this change actually implements

The Go controller gains an optional private Architect workbench and separately tested packages for Discord linking, host authorization policy, and read-only catalog plans. The existing shared telemetry credential is not used to enroll community hosts.

Discord linking uses the administrator's existing session as the trusted principal in this milestone. It is not a public multi-account identity service. A public launcher integration must wait for durable accounts, per-user sessions, recovery, consent and account lifecycle management.

Hosting authorization records can prepare pairing, per-host credentials, bounded server allocations and permitted loopback route plans. They do not create tunnels, install containers, start Java, allocate public IPs, accept players, enforce cgroups, or measure latency. In-memory hosting state is deliberately invalidated by a controller restart.

The catalog checks provider metadata and exact releases. An installation plan is not a download or a malware verdict. Unknown compatibility, unsupported download URLs or unresolved required dependencies remain blockers. No plugin JAR is executed by this controller.

No live protected edge, public DNS, Discord application, Microsoft application, billing system or production service is provisioned by this change.

## Protected self-hosting architecture

The intended packet path is:

1. An ordinary Minecraft client connects to a Metallic regional edge address.
2. An upstream network filters volumetric attacks before the public link is saturated.
3. A bounded Minecraft ingress service authenticates or routes the connection for a tenant.
4. An authenticated outbound tunnel from the user's host carries only that tenant's approved game traffic.
5. The local host supervisor connects to a literal loopback game port inside an isolated workload.

The host initiates the tunnel, which can work behind NAT and CGNAT without router port forwarding when outbound transport is available. Players should not need a VPN or install our host agent. Control-plane HTTPS and game traffic are separate services with separate credentials.

For the first self-hosted Java federation, prefer opaque TCP routing to independent online-mode servers. This keeps Minecraft authentication inside each tenant server without handing the community any network-wide forwarding secret. The launcher and directory can guide joins; supported Minecraft transfer can be evaluated for lobby transitions. Do not put a shared Velocity secret or Floodgate key on a user's PC.

The trusted operator-owned network can use Velocity, modern forwarding, Geyser and Floodgate with firewall-enforced backend access. Community hosts have a different trust boundary. Velocity behind Velocity is not a generally supported shortcut around that boundary.

Routes must bind host ID, owner, server ID, destination, allowed transport, expiry and generation. No arbitrary TCP proxy, LAN scanner, metadata-service access, SOCKS mode, public listen override or fallback to the user's public address. Revocation must reach the data plane and close existing sessions before production support can claim immediate disconnect.

Java handshake hostnames may route shared public addresses. Bedrock transport and authentication require their own tested design; a Java TCP ingress does not automatically provide Bedrock support. A shared Geyser lobby, per-tenant ports or dedicated addresses should be evaluated against the current Geyser transport documentation.

## Trust boundaries

| Boundary | Required behavior |
| --- | --- |
| Player to public edge | Connection, packet-size and rate budgets; protocol-aware admission; upstream scrubbing |
| Browser to account service | Real account sessions, recent authentication for sensitive changes, same-origin mutation defenses |
| Account to device enrollment | Single-use short-lived ticket; unique device credential; no owner field trusted from request |
| Host to server allocation | Exact ownership and quota checks; resource accounting under one lock or durable transaction |
| Relay to host | Authenticated transport, route-scoped permissions, expiry, revocation and bounded buffers |
| Plugin to host OS | Dedicated user plus enforced container/VM boundary; no access to home folders, controller secrets or other tenants |
| Host to network economy | Community hosts cannot mint trusted currency, assign roles, impersonate players or self-certify statistics |
| Catalog to installer | Fixed providers, exact immutable release, size/hash checks, dependency/compatibility/license checks |
| Launcher to release channel | Signed expiring metadata, rollback protection, OS signing, hash verification and atomic activation |

A community owner controls their machine. We cannot truthfully describe that machine as trusted or guarantee protection from its administrator. “IP protected” means the design avoids exposing a host address to ordinary players; the host ISP and selected relay provider still handle that connection.

Do not forward original player IPs to untrusted community backends by default. Resource-pack URLs, web maps, voice chat ports, query/RCON/JMX endpoints, crash logs, DNS records and direct health URLs can otherwise reveal an origin even when the main game connection uses a tunnel.

Java plugins execute code with the server process's permissions. Hash verification detects a changed download; it cannot establish benign behavior. The removal of Java's Security Manager reinforces that an in-process permissions setting is not a sufficient sandbox. Windows host support must use a real isolation boundary and a supported supervisor; starting Java as the user's logged-in desktop account is not the protected mode.

## Discord identity design

The current implementation requests only Discord's identify scope, uses authorization code plus S256 PKCE, validates a random state and browser-bound nonce, checks Discord's issuer, and revalidates the initiating server-side session before committing a link.

Flow state expires after five minutes and is claimed before token exchange to reject callback replay and concurrent redemption. A new flow supersedes the previous flow from the same session. Logout invalidates pending work, including an exchange already in flight. The same Discord identity cannot silently become linked to a different Metallic account.

The existing main admin cookie remains SameSite Strict. A separate short-lived HttpOnly, SameSite Lax flow cookie allows the top-level OAuth return. A success document provides a same-origin link back to the workbench. The provider grant is revoked after fetching identity, and neither access nor refresh tokens are stored.

The local file contains only account ID, Discord user ID, display username and link timestamp. This is a single-process private store with restrictive new-file permissions and atomic replacement; it is not a replicated identity database. Before public rollout, introduce transactional uniqueness, recovery policies, durable sessions and audit events. Do not make Discord mandatory for Minecraft participation.

## Launcher and install transaction

The launcher should keep separate records for desired components and verified installed components. Selecting a component creates a plan; only the native installer can attest that verified files were written. Shared Java/runtime dependencies use reference counts so removing the game does not break the host service or another Minecraft instance.

A production install transaction needs:

1. Resolve signed metadata for the chosen OS/architecture/channel and reject expired or rolled-back metadata.
2. Verify disk requirements, target ownership, free space and declared dependencies.
3. Download into a staging directory with bounded retries and resumable transfer.
4. Validate size and cryptographic digest before activation.
5. Reject archive path traversal, symlink escapes, device files and unexpected executables.
6. Atomically activate the verified version, retaining a rollback slot.
7. Persist the installed receipt only after successful activation.

Uninstall removes selected binaries and unused managed dependencies. Worlds, screenshots, saves and user configuration are preserved unless the user explicitly chooses a separate data removal action.

Minecraft login must use Metallic's own authorized Microsoft application registration and the system browser. Validate the full Microsoft/Xbox/XSTS/Minecraft entitlement flow using that client ID before release. Do not borrow another launcher's client ID, embed a confidential client secret in a desktop binary, or implement a login screen that collects Microsoft passwords. Upstream application approval requirements remain a release verification item.

Roblox can later be an optional external launch integration subject to platform support and distribution rules. Do not invent a redistributed Roblox build or report it installed from a browser-only selection.

## Paper, Folia and original server software

Paper is the first stable compatibility target. Official documentation currently maps Minecraft 1.20 through 1.21.11 to Java 21 and 26.1+ to Java 25. Resolve exact stable builds through Paper's current Fill v3 service, with an identifying User-Agent and a pinned checksum; avoid unattended production updates.

Folia is valuable for suitable geographically separated regional workloads but requires verified scheduler and plugin support. A Hangar PAPER label alone does not establish Folia safety. Crowded players in one active region still need measured tick budgets.

Original Metallic plugins can implement lobby menus, parties, discovery, matchmaking, moderation, events and server transfer, with narrow authenticated control APIs. Keep world simulation, shared account services and advertising decisions outside a single all-powerful plugin.

A Rust or C# engine rewrite is a separate compatibility and performance experiment. Current alternatives, including Pumpkin's PatchBukkit work and Minestom's custom-server model, deserve conformance testing. Language choice alone does not prove better CPU efficiency, less latency or complete Paper behavior.

Measure representative workloads: empty world, exploration/chunk generation, farms/entities, redstone, plugin-heavy survival, crowded lobby and regional minigames. Record tick-time p50/p95/p99, GC pauses, memory, bandwidth, join times and errors on identical worlds and hardware. Existing repository benchmark gates are the starting point; successful unit tests are not performance evidence.

## Catalog and custom plugin supply chain

Hangar offers project search, platform/version metadata, exact version records, SHA-256 file details and plugin dependencies. Modrinth provides immutable version records, loaders/game versions, file digests, environment compatibility and typed dependency relationships. Both providers require polite bounded requests and an identifying User-Agent.

Before an installer is enabled, additionally resolve required dependency closures, cycles, incompatibilities and name collisions. Pin each artifact and include its source, hash, byte size, license, supported runtime, Java requirement when known, and reason for inclusion. Treat absent or ambiguous server support as unresolved.

Do not scrape around third-party API keys, redistribution flags or external download policies. CurseForge can be a later authorized provider; it is not silently enabled in this milestone. Custom Metallic plugins need a review/signing/release process, reproducible provenance where feasible, supported API versions, permission documentation and rollback plans.

## Provider findings: free users versus operator cost

| Candidate | Evidence and implication |
| --- | --- |
| Playit | Outbound tunnels are a useful working precedent. Current terms require written consent for resale, white labeling and multi-tenant redistribution. A personal free tunnel is not authorization to build a hosting platform on it. |
| TCPShield | Current Free plan lists 1 TB monthly traffic, one network and three domains. Residential hosting normally needs a publicly reachable backend. Reseller rules and backend connectivity must be resolved for a multi-tenant product. |
| Cloudflare Spectrum | Minecraft Java support and DDoS services do not make it a Free-plan solution. Ordinary direct Spectrum TCP-to-Tunnel is unsupported; current docs describe an alternative L4 route using Load Balancer Private Network Load Balancing and a tunnel virtual network. That requires the relevant product entitlement and design. |
| Own edge plus WireGuard or another mature tunnel | Gives control of routing and host enrollment. Encryption and tunneling do not add upstream link capacity. Use authorized upstream scrubbing and measure actual regional cost. |

Do not claim a self-authored application filter absorbs an arbitrary volumetric attack. Once the incoming link is full, filtering on the home PC cannot recover that bandwidth. An economical pilot should start with one authorized protected region, explicit bandwidth/session quotas, per-tenant abuse controls and measured demand.

One sustained Mbit/s for 30 days is about 324 GB in decimal units. One decimal TB per month averages roughly 3.09 Mbit/s. These are unit conversions, not estimates of a Minecraft server's player capacity. Traffic accounting direction and overhead depend on the provider.

Latency includes player-to-edge, edge-to-host, queueing, encryption/processing and server ticks. Select a region by measurements on both links. There is no universal zero-latency route; an extra relay can help routing in some paths or make it worse in others.

## Discovery and advertising

The intended directory has two views of the same verified data: launcher browsing and a custom in-game server menu. Fields should include owner-managed description/tags, supported versions, region, language, join policy, verified reachability and a timestamped population sample. Avoid invented capacity, ping or online counts.

A host requests publication; the platform checks ownership, route health and content policy before making it discoverable. Paid placement is explicitly marked Sponsored. Keep organic ranking and sponsored placement separate. Rate-limit listing edits and join actions, and provide reporting, abuse review and appeals.

Minecraft Usage Guidelines apply to server monetization and promotion. Keep paid hosting distinct from competitive gameplay advantages; do not require buying the separate Metallic game or joining Discord to access ordinary Minecraft play. Cross-server economies, cosmetics and unrelated brand advertising need a policy review against the current guidelines before launch.

## Release sequence and acceptance evidence

1. **Private foundation:** tested account-link service, bounded host policy, catalog plans, component-selection UI. This change.
2. **One owned Linux host:** durable device identity, isolated non-root Paper workload, backup/restore, fixed loopback destination and authenticated outbound relay. Demonstrate revocation closing live traffic and origin leak checks.
3. **Protected regional pilot:** provider permission, DNS under Metallic's control, real upstream DDoS coverage, region selection and honest bandwidth quotas. Use provider-approved load testing only.
4. **Launcher release:** signed native updater, separately downloadable artifacts, Minecraft application/entitlement flow, Windows acceptance and recovery tests.
5. **Community beta:** durable multi-user authorization, quotas, abuse handling, moderated directory and custom in-game menu.
6. **Managed hosting:** reconciled metering, paid capacity, refund/cancellation policy and operational support.
7. **Engine optimization:** measured changes that pass gameplay/plugin conformance and reproduce on the actual hardware.

## Primary sources

- Paper Java requirements: https://docs.papermc.io/paper/getting-started/
- Paper Fill v3 downloads: https://docs.papermc.io/misc/downloads-service/
- Folia plugin support: https://docs.papermc.io/paper/dev/folia-support/
- Velocity backend security: https://docs.papermc.io/velocity/security/
- Paper plugin permissions: https://docs.papermc.io/paper/adding-plugins/
- Discord OAuth2 and PKCE: https://docs.discord.com/developers/topics/oauth2
- Microsoft authorization-code flow: https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow
- Hangar OpenAPI: https://hangar.papermc.io/v3/api-docs/public
- Modrinth version API: https://docs.modrinth.com/api/operations/getversion/
- TUF specification: https://theupdateframework.github.io/specification/latest/
- OpenJDK Security Manager removal: https://openjdk.org/jeps/486
- Playit service and terms: https://playit.gg/ and https://playit.gg/terms/
- TCPShield plans and reseller policy: https://tcpshield.com/plans and https://docs.tcpshield.com/miscellaneous/account-sharing
- Spectrum limitations, including L4 alternative: https://developers.cloudflare.com/spectrum/reference/limitations/
- Upstream DDoS architecture: https://developers.cloudflare.com/reference-architecture/diagrams/network/protecting-sp-networks-from-ddos/
- Minecraft Usage Guidelines: https://www.minecraft.net/en-us/usage-guidelines
