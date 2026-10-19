# BYOH enrollment and route policy

## Status

The `internal/hosting` package is a bounded in-memory policy service. It records ownership, enrollment, allocations and short-lived route decisions. It does not install an agent, reserve physical hardware, start Minecraft, open a listener, create a tunnel, relay packets, publish listings or prove reachability.

Allocations report `policy_registered` and successful decisions report `policy_allowed`. Neither label means a live server or route exists. The feature-gated controller uses its private administrator as the owner; this is not public multi-user identity.

## API

| Operation | Go signature |
| --- | --- |
| Defaults | `DefaultConfig() Config` |
| Construction | `NewService(Config) (*Service, error)` |
| Pair | `IssuePairingTicket(ownerID string) (PairingTicket, error)` |
| Redeem | `RedeemPairingTicket(ownerID, ticket, hostName string) (Enrollment, error)` |
| Hosts | `ListHosts(ownerID string) ([]Host, error)` |
| Revoke | `RevokeHost(ownerID, hostID string) error` |
| Allocate | `AllocateServer(ownerID string, request AllocationRequest) (ServerAllocation, error)` |
| Servers | `ListServers(ownerID string) ([]ServerAllocation, error)` |
| Route | `PlanRoute(ownerID, hostCredential string, request RouteRequest) (RouteDecision, error)` |

Every owner argument is supplied by trusted controller context, never request JSON. The private HTTP redemption adapter supplies the configured administrator internally; its ticket authorizes the machine request. A public service must derive the owner from a durable ticket record.

Operations are safe for concurrent use. Listings return copies sorted by ID. Classify errors with `errors.Is`: `ErrInvalidInput`, `ErrUnauthorized`, `ErrNotFound`, `ErrExpired`, `ErrRevoked`, `ErrLimitExceeded`, `ErrConflict`.

## Credentials

Pairing tickets contain 256 random bits from `crypto/rand`, base64url encoded with prefix `mtp_`. They default to five minutes and cannot be configured longer. Incorrect owners cannot consume them; successful redemption is atomic and rejects replay even under concurrent requests.

Redemption generates a separate 256-bit `mhc_` bearer credential and a random host ID. Its default lifetime is 24 hours, with a configurable maximum of 30 days. Raw secrets are returned only when created; the service retains SHA-256 digests. Validly formatted secret digests are compared in constant time. Neither secrets nor digests appear in inventories.

Keep tickets and credentials out of logs, traces and URLs, and protect remote transport with TLS. A telemetry token, Velocity forwarding secret or Floodgate key does not authorize host policy. Revocation is owner-scoped and idempotent, and prevents new allocations and route decisions immediately. Existing live connections cannot be closed by this package because no data plane exists. There is no credential refresh or rotation endpoint yet.

## Allocation bounds

Requests contain `host_id`, `server_id`, `resources`, `game_port`. Resource fields `cpu_cores`, `memory_mb`, `disk_mb` and `player_slots` are positive integers. Memory/disk values are interpreted by this preview's UI as MiB. Defaults are aggregate per-host ceilings of 16 CPU cores, 32,768 MiB memory, 524,288 MiB disk and 200 player slots. These are policy limits, not hardware measurements or enforced quotas.

Server IDs are 1–64 ASCII letters/digits/periods/underscores/hyphens beginning with a letter or digit. Host names are at most 80 bytes and owner IDs at most 128 bytes. Empty, padded or control-character text is rejected.

Server IDs cannot be reassigned and two allocations on one host cannot claim the same port. The host must be owned, active and unexpired. Aggregate resource accounting is locked with allocation and uses subtraction to avoid hostile integer overflow.

## Route restrictions

Requests contain `host_id`, `server_id`, `target` and `ttl_seconds`. The individual bearer credential, owner, host and existing allocation must match.

Only literal `127.0.0.1` and `::1` are allowed, using the exact port registered to that server. Allowed registration ports are 19132–19133 and 25565–25574. The conventional RCON port 25575 is excluded, along with ordinary admin/web ports. The UI currently offers the Java range.

Hostnames, LAN/public addresses, wildcard addresses, other 127/8 aliases, IPv4-mapped IPv6, zone identifiers, URLs, paths and unregistered ports are rejected without DNS or network access. The positive decision TTL defaults to at most five minutes and cannot outlive the credential.

An allowed port does not prove which process owns it. Before real tunneling, the host service must establish that the destination belongs to the Minecraft child it created and supervises. A decision is not a signed, reusable network capability. A future relay must authenticate and check current ownership, destination, expiry and revocation at use time.

## Memory and restart behavior

Defaults allow 64 outstanding tickets, 128 host records and 1,024 server allocations. Configuration hard caps are 16,384 tickets, 4,096 hosts and 65,536 allocations, plus explicit resource maxima.

Expired tickets are pruned on issuance. Expired/revoked hosts and their allocations remain bounded records and still consume capacity. There is no deletion, durable audit log or persistence. Restart discards all records, so old credentials fail and hosts must enroll again. This limitation appears in the workbench.

Public rollout requires durable identity and revocation, credential rotation, isolated managed processes, local resource enforcement, authenticated transport, live session revocation, abuse controls and backups.

## Verification

Run `GOMAXPROCS=2 go test -p 2 -race ./internal/hosting`. The tests cover expiry/replay, incorrect owners and credentials, copied secret-free listings, allocation conflicts, aggregate bounds, overflow, revocation, exact TTL boundaries, hostile destinations, record limits and concurrent redemption/allocation/listing/routing. These checks verify policy, not network protection or host isolation.
