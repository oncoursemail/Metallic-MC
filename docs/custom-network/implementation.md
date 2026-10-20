# Custom network core: implementation and verification

**2026-10-20 UTC. Status: C0 offline foundation implemented.** This is original
Go source in Metallic-MC, not a deployed gateway or a complete replacement for
Velocity, LuckPerms or TAB. No comparative gameplay performance result exists.
The owner-directed [architecture](architecture.md), [primary-source research](research.md)
and [experiment specification](benchmarks.md) define the next work.

## What is executable now

| Package | Implemented behavior | Integration still required |
| --- | --- | --- |
| [`network/policy`](../../internal/network/policy/README.md) | Complete bounded document validation; immutable canonical snapshots; contextual/temporary membership and rules; acyclic role inheritance; deny precedence; explanations; exact version matching; conservative scheduled boundaries | Authenticated context/publisher, durable administration, freshness lease/minimum revision, LuckPerms import and semantic comparison, live enforcement |
| [`network/session`](../../internal/network/session/README.md) | Deterministic ownership state machine; unique epoch/incarnation contract; generation fencing; reserve/freeze/claim/cancel/expire; bound destination/token/deadline; retained bounded history; monotonic supplied time | Online identity, caller serialization, CSPRNG tokens, transactional persistence/receipts/outbox, actual checkpoint saving, world-writer fencing and client transfer |
| [`network/presentation`](../../internal/network/presentation/README.md) | Policy-filtered player lists; deterministic order; literal text bounds; immutable view/session/context/policy versions; remove-before-upsert plans; replay/rollback/known-time rejection | Trusted fact assembly and monotonic connection clock, live revocation/freshness, Minecraft packet encoder/writer, full TAB surfaces, entity visibility, protocol/edition layouts |
| [`network/wire`](../../internal/network/wire/README.md) | Bounded Java length/compression envelopes; encoded+decoded buffer admission before allocation; resumable temporary budget pressure; exact inflation/stream checks; shared budget state; terminal read-error classification | Listener, online login/auth/encryption, packet/state semantics, socket deadlines, CPU/concurrency admission, complete RSS/cgroup accounting, backend switching and Bedrock |

The four packages use the Go standard library and existing module only. No
upstream proxy/plugin implementation was copied into them. The upstream corpus
manifest is metadata used to identify the research evidence, not vendored code.

## Run the integrated scenario

From the repository root with Go 1.23+:

```sh
go run ./cmd/metallic-core-lab
```

The command opens no sockets, starts no Minecraft or database process, and does
not write a world. It uses a fixed synthetic clock, identities, context and token.
The output declares `simulation: true` and `productionReady: false`; a failed
invariant exits nonzero. The [recorded output](core-lab-result.json) is a
deterministic example produced by this command, not live telemetry.

Its **19 checks** connect the packages through one scenario: a hub moderator
inherits a member grant; a contextual deny and wrong policy version reject
authority; a scheduled denial expires queued output; policy revision 2 revokes
a command and hides a player; privacy removal precedes other updates; replay and
rollback cannot overwrite the new model. A destination cannot claim before the
source freezes, freeze removes the current owner, and the new generation fences
the old source. Only the reserved destination can claim, and a duplicate claim
is rejected. The destination reconstructs its arena context, receives only its
arena membership, and builds a new session view. A separate two-frame fixture
demonstrates budget backpressure without consuming the pending body or losing
frame alignment.

The final synthetic state is policy revision **2**, session generation **2**,
owner **arena:boot2**, context revision **3**, realm **arena**, with only
**builder** visible. These names and counters are fixture data.

## Verification actually performed

The machine-readable [validation record](validation.json) contains toolchain,
commands, counts, scope and hashes of the changed source/test files.

| Check | Observed result |
| --- | --- |
| Whole-repository `go test -race -count=1 -p 2 -json ./...` | PASS: 18 packages, 189 top-level unit tests, 647 unit subtests, 14 fuzz-seed subtests across 3 fuzz targets; zero failed or skipped |
| Final lab correction | Its race test and vet passed again after destination context/report coherence was corrected; 19 integrated checks pass |
| `go vet ./...` and `go build ./...` | PASS; final lab also built/executed after its last correction |
| Private pilot Python suite | PASS: 29 tests, including refusal to launch known-unplayable transport even after EULA acceptance |
| Existing private service-bundle suite | PASS: 6 tests |
| Formatting and whitespace | Go formatting clean; `git diff --check` clean |
| Earlier bounded context fuzz campaign | 134,798 executions in a five-second target; no failure |
| Earlier bounded framing fuzz campaign | 178,390 executions in a ten-second target; no failure |

The fuzz campaigns preceded the narrow review corrections. Final corrected
packages passed the race/unit/regression suite; no claim is made that the exact
final revision received another full fuzz campaign. The session tests also run
10,000 deterministic commands across five seeds. These are bounded validation
activities, not a formal proof of every possible execution.

Tests used Go **1.27.2**, Linux/amd64, with `GOMAXPROCS=2`. The module still targets
Go 1.23; no new third-party Go dependency was introduced. Existing HTTP tests used
temporary local loopback listeners. This did not expose a public service.

The final lab-only rerun is reported separately because its destination context
fixture was corrected after the full suite had begun. Counts above describe the
whole-suite run, and the targeted rerun covers the final changed executable.
Package tests do not establish an upstream plugin API contract, an actual
Minecraft login, distributed freshness, persistence, or production readiness.

### Independent review changes

Review found and corrected concrete issues before delivery:

1. A copied `Budget` could duplicate its counters while sharing a notification
   channel. It now shares one private accounting state; reservations retain that
   state even if a handle is reassigned. Nil/zero handles fail cleanly.
2. A reader-originated or wrapped/joined `ErrBudget` could look retryable even
   after the decoder became terminal. Reader failures now remain terminal with
   diagnostic text and cannot masquerade as retained-header admission pressure.
3. Empty candidate sets could skip request validation, and malformed context or
   unsupported clocks could produce a successful empty view. Shared validation
   now runs before iteration; invalid candidate requests also fail explicitly.
4. The presentation README overstated clock history enforcement. The contract
   now requires a caller-monotonic clock and precisely describes target-build and
   known-expiry checks. Snapshot identification is still not a freshness lease.
5. The demo carried the source realm context into the destination. It now
   reconstructs destination facts and derives the final report from that view.
6. Architecture wording that implied an implemented policy publisher now
   describes that publisher as future work.

## Pilot correction and owner acceptance

The exact pinned Geyser artifact and source were inspected. Its loopback check
warns that clients cannot establish NetherNet gameplay there; it does not make
startup fail at that check. The evidence and hashes are in
[research section 3](research.md#3-bedrock-exact-pin-two-paths-and-dated-plans).

This change makes `tools/pilot.py run` and `preflight` reject the known-unplayable
profile before Java executes. `validate` can report valid configuration while
explicitly returning `launch_ready: false` and launch blockers; `probe` cannot
turn a signaling-port success into a crossplay success. The only implemented
network profile remains loopback. There is no unreviewed wildcard-bind bypass.

The owner's **“Confirm I accept”** was recorded as Minecraft EULA acceptance in
the new private comparison runtime's operator receipt. Its two runtime EULA
files are true. Source generation still defaults to unaccepted terms; acceptance
is an owner action, not a default imposed on future users.

The private comparison runtime remains stopped. Brain 1's inspected account has
no available Docker/Podman, Java on PATH, or noninteractive administrator access.
Metallic-MC's required project container and reachable private non-loopback ICE
interface have not been provisioned. The previously staged immutable release
was not silently overwritten with this research branch. A future deployment must
select the updated guarded source and implement/verify the new transport profile.

Metallic's other project remains separately scoped. Old Servers/Wings on Brain 2,
public listeners, firewall rules and domain routing were not changed. The old UI
and three alternative designs remain available for owner selection.

## What C1 and C2 must deliver next

C1 needs a durable authority transaction and authenticated publication path,
minimum-revision/freshness enforcement, operation receipts, trusted context
issuers, and a constrained container executor under the approved project
boundary. Prove crash/retry/restart behavior before live player state depends on
it. A control-plane database fence still cannot fence an open Minecraft world
directory.

C2 needs a real single-version Java Gateway and backend adapter: online login,
encryption, compression, configuration/play, normal movement/inventory/chat and
one authenticated backend switch. Use actual clients and the unchanged baseline
to find differences. Then attach Authority/Presence in shadow mode, implement
private Bedrock transport, and run the declared comparative experiments.

Hosted CI and a target-host deployment are separate evidence. Local passing
tests do not turn a provider account/runner failure into a successful CI run or
remove the current host-provisioning blockers. No public capacity, no-lag,
high-availability or superiority claim is made by C0.
