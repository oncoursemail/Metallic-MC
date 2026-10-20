# Metallic-MC implementation and validation — 2026-10-20

This change completes the private pilot tooling and restart-recovery implementation built on the existing control foundation. It is not a claim that the public Minecraft network, custom gameplay, customer hosting, launcher or protected edge is finished or deployed.

## Implemented and integrated

| Area | Result | Practical boundary |
| --- | --- | --- |
| Project identity | Canonical Metallic-MC naming and project/network research integrated from PRs 27 and 24 | Metallic-RC, Metallic-RB and Metallic-PX retain their separate identities |
| Architect | Private workbench, Discord identity adapter, scoped host pairing, allocation/route policy and catalog inspection from PR 26 | Disabled by default; single administrator; policy does not start games or tunnels |
| Durable host registry | Optional Linux store persists registrations, credential digests, revocations and allocation plans | Pending pairing tickets, sessions and live telemetry remain ephemeral |
| Safe recovery | Exclusive writer lock, strict bounded state decoding, private modes, atomic synchronized updates and failure rejection | Not encrypted, immutable or replicated; replaying an old valid snapshot can restore old authority |
| Controller lifecycle | SIGTERM/interrupt drains active requests before releasing the store, with a fixed deadline | Actual boot recovery still needs an installed service manager and a host exercise |
| Discord transport | Parsed HTTPS and exact-origin checks, scoped identity linking, bounded cleanup after malformed token responses | An owner-controlled dedicated application and a real linking test are still needed |
| Minecraft pilot | Pinned Velocity + Geyser + Floodgate, two Paper backends, guarded fetch/validation/admission/start/stop/whitelist CLI | No EULA accepted, Minecraft JVM started or real player authenticated during this implementation |
| Pilot backup | Stopped-only compressed snapshots, world-lock retention, per-file and archive hashes, new-directory restore | Fixture roundtrip verified; real-world gameplay/save/recovery remains an acceptance gate |
| Service bundle | Digest-pinned binaries, separate controller/metrics users and secrets, bounded restart/resources, retained previous release and rollback | Optional dedicated Linux/systemd deployment; it does not replace the required separate project containers |
| Placement policy | I/O pressure and observation-only Wings signals integrated from PR 11's backend changes | Read-only admission preview; it does not enforce Minecraft resource reservations |
| Moderation | Pure authorization policy from PR 23, including protection of Old Servers and denial of destructive operations | No live sanction or enforcement API is enabled |
| Browser behavior | Accurate persistence status, usable mobile sign-out, one resource listener after repeated navigation | Existing UI alternatives remain pending owner selection |

Implementation guides: [private pilot](private-pilot.md), [hosting persistence](hosting-persistence.md), [service recovery](service-recovery.md), [Architect workbench](architect-workbench.md), and [moderation policy](adr/0003-moderation-policy.md).

## Verification completed

Validation used an isolated Linux workspace, Go **1.27.2**, Python **3.12.14**, and local loopback access. Test concurrency was limited. Credentials were generated only for temporary fixtures and were not printed or included in source.

| Check | Result |
| --- | --- |
| `go vet ./...` | Passed |
| `go test -race -p 2 -count=1 -json ./...` | **13 packages; 136 top-level tests; 489 subtests; zero failures; zero skips** |
| Controller race tests after final embedded UI fixes | Passed for `internal/control` and `cmd/metallic-control` |
| `go build ./cmd/...` | Passed |
| Windows amd64 controller cross-build | Passed with the final UI assets; not executed on Windows |
| Pilot safety, lifecycle and backup suite | **28 tests passed** |
| Service preparation and configuration safety suite | **6 tests passed** |
| systemd unit syntax | Passed on temporary copies using `/usr/bin/true` only as the verification executable |
| Four official entrypoint JAR downloads | Downloaded and verified against exact SHA-256 pins; configuration/artifact validation passed |
| Real local Chromium browser acceptance | **17 checks passed** at 1440 × 1000 and 390 × 844; no page exceptions or external browser requests |
| Patch whitespace | `git diff --check` passed |

The browser run completed at **2026-10-20T01:13:55Z**. It exercised actual admin login, empty-fleet reporting, navigation and resource selection, unavailable Discord configuration, persistent-mode status, pairing ticket creation and hiding, host redemption, allocation creation, controller process restart, old-session rejection, recovered inventory, revocation, mobile sign-out and logout access removal. Desktop and mobile screenshots were visually inspected. No design alternative was selected through this test.

The Go recovery tests also verify credential revocation across repeated restarts, failed commits, exclusive locking, corrupt/malformed state, and rejection of restored online telemetry claims. The pilot tests use small Python subprocess fixtures and sample world bytes. They include broken-console cleanup, partial startup cleanup, orphan/locked-world refusal, retaining POSIX world locks until archive completion, hard-link rejection, failed checksum cleanup, exact format bounds, corruption and hostile archive paths. They do not simulate a successful Minecraft login.

Reproduce the Python checks with:

```bash
python3 -B -m unittest discover -s tools -p pilot_test.py -v
python3 -B -m unittest discover -s infra/systemd -p '*_test.py' -v
```

Both Python suites are included in the repository's existing GitHub Actions workflow. Earlier hosted runs did not execute: the inspected job had no steps and no assigned runner. The prior account billing/spending-limit block has not been repaired or worked around by changing repository privacy, permissions, workflow gates or billing. Local test results are reported separately from hosted CI.

## Deployment evidence and remaining gates

Read-only checks on **starai / Brain 1** during this session confirmed access to the machine and a running system systemd manager. The account has no working user systemd bus and cannot obtain noninteractive administrator access. Docker and Podman are not installed there, and Java is not on its PATH. Existing work in the earlier checkout was preserved.

Metallic and Metallic-MC must retain **separate project containers**. This release does not install bare-host services as a substitute for that boundary. Host/runtime setup and appropriate administrator access are therefore actual deployment gates. No service installation, port publication, domain change or Minecraft launch was performed by this change. Old Servers/Wings on alexbuz1 were not changed, stopped or attached to another manager.

The next live acceptance requires a designated isolated Linux runtime with Java 25 and verified resource headroom; the owner's Minecraft EULA decision; authenticated Java and Bedrock players; and a stopped-world restart/restore exercise. A public pilot additionally needs a protected edge that supports the actual Java TCP and Bedrock TCP/UDP paths, authenticated routing, firewall/origin verification, and measured load evidence. A successful TCP probe, HTTP health response or unit test cannot establish these properties.

The three alternative UI concepts and old interface remain available in the existing design PRs. They are not silently replaced by this implementation. Public accounts, a native host executor/installer, managed tenancy, launcher installation/authentication, a shared public directory, game-specific content and moderation enforcement remain separate unfinished product work. No performance improvement, capacity, DDoS resilience or grief-proof recovery is claimed without the corresponding real measurements and acceptance.
