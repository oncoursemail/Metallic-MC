# Server lifecycle policy

This package produces deterministic, typed operation proposals. It creates no
process, container, tunnel, file, shell, listener, scheduler job or durable server
record. Every decision includes `execution_enabled:false`; `allowed:true` means
only that the supplied authoritative facts permit the proposed sequence.

## Trusted adapter boundary

Use `NewEvidence(TrustedState)` followed by `Plan(Request, Evidence)`. `Evidence`
has private fields, defensively copies its attestation slice and rejects JSON
unmarshalling. Its zero value is invalid. `TrustedState` is an adapter input, not
a tenant request schema. Authenticating a caller does not make facts they submit
about a host, plugin or approval trustworthy.

Call `ValidateRequest(Request)` before looking up trusted server evidence in an
HTTP adapter. The shared validator rejects malformed and unsupported operation
intent without a state lookup. It checks shape, not identity authentication or
server authorization; `Plan` calls it again and then checks the owner/role scope.

The controller supplies `Request.Principal` from authentication, retrieves the
server's owner and state from authoritative storage, verifies attestations, and
sets `TrustedState.Now` using its clock. The requesting owner may provide the
server ID, action, stable request ID and expected revision. An authenticated
moderator role must be established independently; it is never a request switch.
An existing BYOH allocation does not establish managed-runtime authority or a
running instance and must not be relabeled as one.

Minimal authoritative placeholder state can use `Phase: Unprovisioned`,
`Generation: 0`, a nonzero revision and no attestations. Hosted start then returns
specific blockers. `Unknown` deliberately blocks runtime start/stop/restart/
hibernate/admission planning because the prior writer is not understood.

Owner/principal IDs are exact, bounded printable UTF-8 identities of at most 128
bytes, including email-shaped IDs. Server IDs are at most 64 ASCII bytes; epoch,
policy revision and operation IDs are at most 128. These IDs start alphanumeric
and then permit alphanumerics, period, hyphen, underscore and colon. No ID is a
filesystem path. Malformed inputs return `ErrInvalidInput`. Policy denials return
a decision with fixed blocker codes and no steps. Foreign owners/resources do
not receive authority epoch, generation or captured evaluation-time metadata.

## Supported authorities and modes

Execution location (`Local`, `Hosted`) and visibility (`Private`, `Public`) are
independent. Hosted private servers still require hosted-use rules and the
approved managed runtime. A local private server requires no Metallic policy
acceptance or managed plugins and has no platform route. Its owner operations
only delegate to the separate local launcher; the cloud has no local executor.

An `Owner` may plan start, stop, restart, hibernate, export and delete for their
own server. A participating server's owner may also admit or unpublish it. A
`PlatformModerator` may only suspend or reinstate a hosted server or a local
public participant. Moderator authority does not grant file, console, export,
owner deletion or local private control. A broader administrator deletion role
is deliberately not implemented by relabeling a moderator.

Suspending a local public server revokes its Metallic route, listing and scoped
credentials. It never emits a stop, file deletion, checkpoint or quarantine step
for the local machine. This cannot prevent its owner from operating independently
outside Metallic. Hosted suspension isolates and stops the managed runtime and
quarantines retained data. Start, restart and admit are denied while suspended.
Reinstate only clears suspension and keeps admission closed; it never starts or
publishes a server automatically.

## Start and admission are separate

Hosted start/restart needs current policy acceptance, a current `ManagedProfile`
observation matching `ManagedProfileDigest`, and a selected
`CompatibilityProfileDigest`. This means the trusted runtime adapter validated
the prepared profile, sandbox prerequisites and compatibility bundle. It does
not mean Minecraft is already healthy. Start boots in isolation and waits for
fresh evidence. It never opens a route or publishes a listing.

A `Failed` observation can mean a failed health check or partial restart while a
writer remains alive. Hosted start from `Failed` therefore revokes admission,
isolates runtime networking, stops the old instance and verifies writer exit
before checkpointing, advancing generation and booting a replacement. Failure to
prove exit must block the sequence in an eventual executor.

`Admit` requires an observed `Running` phase and current gateway and compatibility
observations. Hosted instances additionally need current runtime readiness bound
to the managed profile. Public instances additionally require current moderation
clearance. Private hosted admission emits an allowlist-route step; public
admission emits public-route then listing steps. A public local tunnel uses
edge compatibility evidence without pretending the local machine is managed.

Five attestation kinds exist: `ManagedProfile`, `RuntimeReady`,
`CompatibilityReady`, `GatewayReady`, `ModerationClear`. At most one of each is
accepted. Their validity window is positive and at most five minutes. A proof is
current only when its epoch, server ID, server revision and generation exactly
match and `IssuedAt <= Now < ExpiresAt`. Profile proofs also match the selected
SHA-256 digest. The digest is metadata, not authentication: the adapter must
verify provenance/signatures/transport and the meaning of the observation.

The five-minute ceiling is a conservative planning bound, not a measured
revocation SLO. An execution adapter must shorten leases where required, refresh
facts at actual use time, enforce clock discipline and close live channels after
revocation. This stateless planner does not maintain a monotonic clock, acquire
leases, verify signatures or establish a live network session.

`evaluated_at` is the captured trusted `Now` from `NewEvidence`, not the time at
which `Plan` happens to be called. Reusing the same immutable evidence repeats a
historical evaluation, including its original timestamp. A caller must construct
fresh evidence with current authoritative time and observations to evaluate
current readiness. The timestamp is not an execution lease or a valid-until
promise. Unauthorized decisions leave it at the zero time.

## Fencing and data retention

Every result is bound to an authority epoch, expected server revision and current
runtime generation. Runtime replacement/containment proposals carry a next
generation; overflow is denied. The executor must compare-and-swap the exact
revision, persist operation identity and retry receipt, recheck policy, and act
only on the registered instance. Request IDs here are labels; no idempotency
receipt store is implemented. A stale or replayed proposal is not authority.

Hibernation is orderly stop, proof that the writer exited, a durable verified
checkpoint and released compute. Restart checkpoints only after writer exit.
This is not in-memory JVM/VM suspension or a live-world synchronizer. A generation
number alone does not fence filesystem writes. The executor must actually stop
and fence the old process/storage before a replacement can write.

Hosted deletion first tombstones and revokes the server, then isolates/stops it
and quarantines data. No permanent purge step exists. Retention, irreversible
purge and administrative deletion need separate policy and implementation.
Local deletion delegates to a local trash operation. Export selects an existing
owner snapshot excluding platform secrets; it does not restart a suspended
server, prove that a snapshot exists, or bypass later retention/evidence policy.

## Verification

`go test -race ./internal/platform/lifecycle` checks owner and moderator scoping,
local private independence, route-only public tunnel suspension, policy and
attestation freshness/binding, two-stage boot/admission, stale revision and
overflow rejection, stop-before-checkpoint ordering, quarantine-only deletion,
containment before restarting a failed writer, shared request validation,
captured evidence-time semantics, malformed/JSON evidence rejection and copied
concurrent planning. These tests
verify policy decisions, not runtime isolation, storage fencing or gameplay.
