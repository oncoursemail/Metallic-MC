# Offline edge configuration audit

`metallic-edge-audit` checks a proposed ingress configuration and local evidence files before an operator uses that configuration. It catches contradictions and missing evidence. It does **not** provision protection, resolve DNS, connect to a server, inspect the running firewall, contact a provider, generate traffic, or prove that an attack will be mitigated.

The strongest result is `configuration_consistent`. Every report still contains `protectionVerified: false` and `liveNetworkTested: false`. Provider and exposure assertions remain supplied by the operator. A matching SHA-256 digest establishes that the file matches the recorded bytes; it cannot establish that the file is authentic, complete, truthful, or relevant to the declarations.

## Run the incomplete example

From the repository root:

```sh
go build -o /tmp/metallic-edge-audit ./cmd/metallic-edge-audit
/tmp/metallic-edge-audit -config examples/edge-audit/incomplete.json
```

The supplied example intentionally has no known origin, provider, assigned edge addresses, boundary review, or evidence. It must report `blocked` and exit `1`. The listed ports demonstrate the NetherNet transport shape; they are not a discovered deployment. Do not turn unknown assertions into `true` or `false` simply to clear findings.

Copy the example into an operator-controlled, untracked folder. Populate it from actual deployment records and a fresh review. Add local, redacted evidence snapshots and then run:

```sh
/tmp/metallic-edge-audit -config /path/to/manifest.json -evidence-dir /path/to/evidence
```

Keep API tokens, WireGuard private keys, Floodgate keys, forwarding secrets, passwords, and account credentials out of both the manifest and evidence artifacts. Provider references must not contain URL credentials. The checker never requests credentials or downloads a referenced URL.

| Exit | Meaning |
| --- | --- |
| `0` | Declarations and supplied evidence satisfy this offline profile; also used by `-help`. |
| `1` | A JSON report describes a `blocked` or `unverified` configuration. |
| `2` | Usage, input parsing, local file access, or output failed. No successful audit result is produced. |

Malformed input is reported on standard error. A parsed manifest produces a JSON report on standard output. The report includes the exact input file's SHA-256 digest so a reviewer can identify the snapshot that was checked.

## Transport scope

Schema version 1 describes **one public ingress profile and one private backend endpoint**. Use separate manifests for Java and Geyser. The checker does not detect collisions between separate manifests or between containers; deployment review must still check the complete listener inventory.

| `transport` | Required public TCP ports | Required public UDP ports | `webrtcPort` | Gameplay endpoint inventory |
| --- | --- | --- | --- | --- |
| `java` | `gamePort` | None | `0` | `gamePort` |
| `nethernet` | `gamePort` | `gamePort` | `0` | `gamePort` |
| `legacy` | None | `gamePort` | `0` | `gamePort` |
| `both` | `gamePort` | `gamePort` and a different `webrtcPort` | A different valid port | Both port values |

This follows Geyser's built-in signaling profile: NetherNet uses TCP for signaling and UDP for gameplay. Geyser's `both` mode keeps RakNet on the game port and needs a separate UDP WebRTC port. These requirements were checked against the [official Geyser setup guide](https://geysermc.org/wiki/geyser/setup/) on October 19, 2026. The [October 8 migration notice](https://geysermc.org/blog/preparing-for-nethernet/) describes a planned October 27 RakNet client transition; verify the actual client and Geyser release before deployment. `legacy` generates an informational transition finding and must not be treated as support for every future client.

External NXS signaling, HTTPS termination on another port, detailed port translation rules, and additional voice, Query, or web services need a separately reviewed topology; this first schema does not model them fully. Do not declare a fake TCP gameplay listener to force an external-signaling arrangement through this profile. [Geyser's HTTPS guidance](https://geysermc.org/wiki/geyser/nethernet-https/) distinguishes signaling from the UDP gameplay path.

`advertisedGameplay` is a combined inventory for this audit, **not a Geyser configuration fragment**. In `both` mode it includes the RakNet endpoint and the NetherNet gameplay endpoint. Never copy its entire two-port list blindly into `bedrock.signaling.nxs.advertise-addresses`, which represents the relevant NXS gameplay advertisements. See [Warden's NAT guidance](https://warden.cloud/troubleshooting/nat/) for explicit advertisements and the limitations of automatic STUN discovery. Warden signaling itself does not relay gameplay or conceal the advertised game endpoint; see [Warden's service description](https://warden.cloud/).

Built-in signaling has a separate `bedrock.signaling.builtin.advertise-addresses` setting, also configurable with `-DgeyserAdvertiseAddresses`. External signaling uses the `nxs` setting instead. Review every enabled signaling mode, as described in [Geyser's system properties](https://geysermc.org/wiki/geyser/geyser-command-line-arguments-and-system-properties/); changing one list does not prove that another advertisement path is closed.

## Addresses and exposure boundaries

`origin.address` must be a numeric RFC1918 address, IPv6 ULA, or loopback address, with a port from 1 through 65535. It represents the private backend interface, for example the destination reached through an authenticated private tunnel. A public, link-local, unspecified, IPv4-mapped, zoned, CIDR, or hostname origin is rejected. A private address in a manifest does not establish that the host lacks another public interface.

`origin.port` need not equal `gamePort`: Geyser can terminate the public Bedrock transport at the protected edge and then connect to a private Java backend on a different port. The audit checks the declared backend's private address and port validity; it does not implement or verify protocol conversion, route reachability, source preservation, or a port-forwarding rule. The exposure-review evidence must cover the actual path. A Geyser instance running at the private origin also needs its own transport listeners and advertisement review; this manifest does not discover them.

`edge.protectedAddresses` contains 1–16 exact public numeric addresses that the operator declares are assigned to the protected edge. `advertisedGameplay` contains 1–32 address/port pairs. Every advertised address must match that set after numeric normalization, and every port must match the selected transport. Multiple IPv4 and IPv6 edge addresses are supported. Each address needs coverage in the assignment evidence; the program checks membership but cannot inspect a provider account or establish ownership.

Hostnames are deliberately excluded. The checker performs no DNS lookup and does not assume that a hostname's current or future A/AAAA/SRV answers point to a protected address. An operator must separately review DNS records, all address families, game advertisements, resource-pack URLs, maps, status pages, historical origin leakage, and any failover address. A forgotten IPv6 or alternate service route can defeat an otherwise correct IPv4 design.

The address policy is conservative: it excludes documentation, private, shared CGNAT, benchmark, multicast, reserved, transition, and selected special-purpose ranges, and only accepts IPv6 within `2000::/3` after exclusions. It is not a live allocation database. Some special-purpose addresses with global exceptions are intentionally rejected. Review [IANA's IPv4](https://www.iana.org/assignments/iana-ipv4-special-registry/) and [IPv6](https://www.iana.org/assignments/iana-ipv6-special-registry/) registries when maintaining this policy; do not widen it merely to make sample addresses pass.

Required boundary assertions use booleans; omission or `null` means **unknown** and prevents a consistent result.

| Declaration | Required value | Review it against |
| --- | --- | --- |
| `directOriginReachable` | `false` | Router, cloud and host rules; all public interfaces and origin ports. |
| `ipv6Reviewed` | `true` | Actual IPv6 exposure or evidence that IPv6 is disabled throughout the path. |
| `publicAdministration` | `false` | SSH, RCON, Wings, panels, databases and management interfaces for this protected service. |
| `resourcePackLeaksOrigin` | `false` | Pack URLs, redirects and download hosting. |
| `auxiliaryServicesLeakOrigin` | `false` | Maps, status services, Query, DNS siblings, debug endpoints and advertised fallback routes. |
| `authenticatedForwarding` | `true` | Edge-to-origin peer authentication and applicable game identity forwarding. |
| `firewallRestrictsPeers` | `true` | Rules that accept backend traffic only from the intended private ingress peers. |
| `signalingProtected` | `true` for NetherNet | Both signaling reachability and its applicable provider coverage. |
| `automaticOriginDiscovery` | `false` for NetherNet | Explicit protected gameplay advertisements and no origin-revealing discovery path. |
| `tenantEgressControlled` | `true` for community hosting | Outbound isolation, so a tenant plugin cannot reveal the private host's public egress address. |
| `edge.communityHostingPermitted` | `true` for community hosting | Provider terms or written permission covering the intended hosted service. |

For Java behind Velocity, use the applicable authenticated forwarding mode together with network isolation. Modern forwarding is not a replacement for a firewall; [Velocity's security guide](https://docs.papermc.io/velocity/security/) documents that boundary. If an upstream sends PROXY protocol, trust it only from intended peers. An unauthenticated PROXY header is not proof of identity.

On hosts using Docker or Wings, inspect the rules that apply to published container ports. [Docker documents that its port publishing can bypass UFW's usual INPUT/OUTPUT path](https://docs.docker.com/engine/network/packet-filtering-firewalls/). This checker cannot read or validate those live rules.

## Provider evidence

`edge.provider` and `edge.plan` must name the actual provider and product. `edge.coveredTransports` lists unique concrete values from `java`, `nethernet`, and `legacy`; a `both` service needs both `nethernet` and `legacy`. Generic “UDP” or “Bedrock” marketing is insufficient to infer NetherNet support. These fields record assertions, not a vendor certification performed by this tool.

Three evidence kinds must be bound to their corresponding fields:

| Kind | Referenced by | Snapshot contents to review |
| --- | --- | --- |
| `provider_coverage` | `edge.providerEvidenceId` | Provider/plan, protocols and ports, region, mitigation scope, attack and traffic limits, suspension policy, billing, and community hosting permission where relevant. |
| `edge_assignment` | `edge.assignmentEvidenceId` | Assigned ingress IPs and ports for this account/service and their mapping into the private path. |
| `exposure_review` | `boundaries.exposureEvidenceId` | The reviewed manifest, IPv4/IPv6 exposure, actual routing and firewall evidence, forwarding authentication, signaling advertisements, management and tenant egress boundaries. |

Each evidence entry requires these exact keys:

| Key | Rule |
| --- | --- |
| `id` | A unique identifier of at most 80 ASCII letters, digits, dots, underscores or hyphens, starting with a letter or digit. |
| `kind` | One of the three kinds above. |
| `reference` | A credential-free HTTPS provider or review reference, at most 2,048 bytes. It is not fetched. |
| `observedAt` | RFC3339 timestamp; no future observation. |
| `expiresAt` | RFC3339 timestamp after observation and strictly after the audit time. |
| `artifact` | One simple filename in the explicitly supplied evidence directory, at most 128 ASCII characters. No path separators or traversal. |
| `sha256` | Exactly 64 hexadecimal characters describing the saved artifact bytes. |

Evidence must have been observed no more than seven days before the audit time and must not have expired. Seven days is this preflight's review policy, not a provider SLA or a guarantee that infrastructure cannot change earlier. Re-review immediately after a provider, address, transport, firewall, credential, plugin or topology change. The checker uses the local clock; clock accuracy and provenance remain the operator's responsibility.

There may be at most 16 evidence entries. Each file must be regular, nonempty and at most 1 MiB. Missing files, missing timestamps, expired observations, absent provider bindings and unknown assertions prevent consistency. A digest mismatch or a contradictory boundary blocks the configuration. An entry that satisfies the checks is labeled `integrity_checked_assertion` with provenance `operator_supplied_not_independently_verified`.

Do not reuse unrelated snapshots just because their hashes validate. The program does not parse or authenticate the contents of PDF, text or image evidence. A human review must establish that each snapshot supports the current provider, addresses, ports and boundary declarations. Store the exact reviewed manifest alongside the exposure review and preserve any independent approval outside this tool.

## Parser and filesystem limits

The manifest must be exactly one UTF-8 JSON object, at most 64 KiB. Duplicate keys, escaped duplicates, unknown fields, case aliases, trailing documents, excessive nesting, excessive value counts, wrong primitive types, and `null` values outside boolean assertions are rejected. Empty arrays and absent fields remain subject to the semantic audit; missing evidence never produces a successful result.

The CLI rejects symlink and non-regular manifest files. Evidence names cannot leave the supplied directory, and evidence symlink entries are rejected. Reads have size limits. Run the audit over an immutable, operator-controlled local snapshot: these safeguards are not a filesystem sandbox against a hostile process changing directory components or replacing files concurrently. No network filesystem mount, file reader or provider URL is created by this command.

## Operational completion beyond this check

Passing the preflight is one review input. Deployment still requires a provider-approved configuration, confirmed upstream TCP/UDP mitigation, actual private-origin isolation, current Geyser/Velocity versions, legitimate join tests across the supported client matrix, tunnel MTU and loss measurements, observed latency and tick-time baselines, and a rollback path. Exercise failover on owned infrastructure without attack traffic. Provider edge failure generally affects established sessions; a second endpoint alone does not migrate a running world or client session.

A reverse DNS record supplies a name. WireGuard supplies an authenticated encrypted tunnel. Neither manufactures upstream scrubbing capacity or removes propagation delay. Metallic's custom work belongs in a verifiable control layer—strict configuration, bounded admission, explicit advertisements, evidence review and measured failover—combined with an upstream that actually provides the protection required for the selected transport.
