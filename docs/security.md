# Security baseline — Metallic network

**Threat model:** a malicious or compromised player, plugin, mod, Bedrock bridge, proxy, admin session, host/node agent, or game-server tenant; account impersonation; player-data theft; RCON exposure; denial of service; disk exhaustion; supply-chain updates. Scope is defensive hardening of a network we control.

## Public exposure boundaries

**Never expose:** SSH, RCON, backend Minecraft ports, database ports, Redis, Docker socket, Metallic Control, control-agent API, file storage, or instrumentation endpoints directly to arbitrary players. Public ingress should be a dedicated DDoS-mitigated gateway with only the necessary Java TCP and Geyser Bedrock TCP/UDP protocols exposed.

With *locally hosted hardware*, downstream DDoS mitigation cannot prevent flooding of a residential uplink if your true origin IP leaks and is attacked directly. A router firewall cannot handle more malicious traffic than an ISP connection can absorb. Use an upstream mitigation partner or edge that shields your public address; protect origin IP, prohibit direct origin access, and ask the ISP about upstream filtering or business-grade transit. Avoid internet-facing port forwards to the raw Minecraft servers.

TCPShield documents support for certain home-hosted servers but explicitly does not guarantee performance on residential connections. Its optional Cloudflare network documents Java-only support; Bedrock must be routed separately on a compatible edge. Cloudflare Spectrum supports general TCP and UDP under appropriate plans, and protected private origins under some configurations. Validate *current* plan entitlements and supported Geyser protocols before paying for or advertising protection.

## Java + Bedrock trust boundaries

1. Validate the player at a public proxy with online-mode authentication; configure Floodgate correctly for Bedrock identities.
2. Use **Velocity modern forwarding**, with a secret stored outside Git, for supported modern backends.
3. **Still firewall backends**: forwarding MACs do not replace ACLs; only the proxies can reach backend servers.
4. Configure Geyser/Floodgate at the proxy, keep Floodgate key material private, and test Java + Bedrock join/quit/transfer paths.
5. Log only the information required for moderation and incident response. Keep retention bounded.

## Metallic Control prototype

- Defaults to `127.0.0.1:8080`; it is **not** a public HTTPS server.
- User login requires a nontrivial admin password and is rate-limited (five wrong attempts per source address every five minutes, in memory).
- Session IDs are 256-bit random opaque tokens, held only in process memory and sent as HttpOnly + SameSite Strict cookies. Set `METALLIC_COOKIE_SECURE=true` at an HTTPS deployment.
- Agents require a shared secret bearer token. Use HTTPS for traffic between machines. The agent refuses non-local HTTP by default.
- Heartbeats use constrained JSON; 64 KiB maximum request bodies, validated node IDs and metric ranges.
- No process creation, arbitrary shell commands, remote file writes, instance startup/shutdown, Docker socket mounting or privileged actions are exposed by the foundation API.
- Secrets are not checked into Git. `.env.example` contains placeholders only. Do not turn example strings into real secrets.

- The optional Star AI context endpoint requires a **different** explicitly provisioned read-only bearer token, returns a redacted live status view, and is disabled by default. It contains no start/stop API, credentials, private node addresses or player data.
- Resource Manager computes advisory placement previews from actual reported telemetry; it does not reserve capacity, stop Star AI, or enforce operating-system limits.

### Known limitations — **do not expose publicly yet**

- In-memory sessions, telemetry and login throttling disappear after restart. The login limiter is per source address; a distributed source can evade it. A future identity provider should provide MFA, centralized session invalidation, audit logs and role/permission policies.
- A shared agent token has broad ingestion privileges. Replace it with per-node enrollment, rotation, short TTLs, mTLS or signed messages, revocation and ingest rate limiting.
- HTTPS termination must be configured by an operator and its reverse proxy must be trusted. The dashboard doesn't itself serve TLS, enforce HSTS, or provide a complete production security architecture.
- Telemetry is informational only; no authoritative billing, auto-recovery or placement decisions should be based on it yet.
- A service `reachable` check is a TCP handshake, not proof that login, plugins, routing, tick loop or world data are healthy.

## Future public hosting: stronger isolation

When Metallic hosts player-owned servers, third-party server JARs and plugins must be treated as **untrusted code**. Do not run them under the control-plane account. Use dedicated namespaces/containers or stronger isolation with CPU/memory/PID/disk/network limits; deny privileged containers, host networking, host path mounts and Docker socket. Apply egress controls, SFTP containment, per-tenant secrets, quotas, abuse monitoring, malware scanning and isolation regression tests. Public hosting is a separate product with larger risk; delay it until the internal network works reliably.

## Backup and incident policy

- Encrypt and backup world data, PostgreSQL state, purchase/permission records, configuration and secrets; keep *off-machine* versioned backups.
- Snapshot before Minecraft version upgrades. Newer world formats may not downgrade; test a restore.
- Add monitoring for disk saturation, player auth failures, failed joins, packet floods, MSPT and proxy/backend crash loops.
- Require 2-person or multi-step approval for dangerous cluster operations (mass deletion, world resets, production updates) after a team exists.

### Sources

- https://docs.papermc.io/velocity/security/
- https://docs.papermc.io/velocity/player-information-forwarding/
- https://geysermc.org/wiki/floodgate/setup/
- https://geysermc.org/wiki/geyser/setup/
- https://docs.tcpshield.com/miscellaneous/protect-a-home-hosted-server
- https://docs.tcpshield.com/premium-features/cloudflare-network
- https://developers.cloudflare.com/spectrum/get-started/
