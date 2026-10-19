# Architect validation — 19 October 2026

## Result and scope

The private Architect workbench, identity service, host policy package and plugin catalog adapters passed the checks below. The companion Metallic launcher selection planner was checked in the original Metallic repository. These results establish a development foundation; they do not establish a production hosting service or a performance advantage over another Minecraft server.

Validation used an isolated checkout based on main commit `cc1e9f91b5d17e8ddb242bc09a812de1ed59f22a`. Go 1.27.2 ran on Linux with `GOMAXPROCS=2`. Existing services and production configuration were not changed. This work was performed by ChatGPT.

## Automated checks

| Check | Result |
| --- | --- |
| `go vet ./...` | Passed |
| `go test -json -p 2 -race -count=1 ./...` | Passed: 11 packages with tests, 108 top-level tests and 380 subtests; zero failed tests |
| `go build ./cmd/...` | Passed |
| `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/metallic-control` | Passed as a cross-build; not executed on Windows |
| `git diff --check` | Passed |
| Companion launcher `node --test services/launcher/tests/*.test.mjs` | Passed: 21 tests, zero failures |

The full race-enabled Go run took about four minutes. The existing benchmark evidence fixtures account for most of that duration. Their synthetic parser and screening tests are not gameplay benchmarks.

The new tests cover OAuth replay and concurrent callback handling; browser/session binding; expiry and account uniqueness; persistence errors; fixed-origin Discord requests; private authorization and CSRF checks; the feature flag; pairing and credential revocation; resource accounting and route target restrictions; fixed-provider metadata requests; exact-release compatibility and dependency blockers. Fixture tests do not replace a live OAuth application test or third-party API monitoring.

## Browser and live catalog acceptance

Playwright 1.64.1 with Chromium 156 exercised the actual local controller and launcher HTTP servers. All 14 acceptance checks passed, with zero uncaught browser JavaScript errors:

1. Sign in to the controller, follow the feature-gated Architect link, and see the correct Discord setup-required state.
2. Create and redeem a single-use host ticket; clear its secret from the workbench.
3. Save a server allocation from the browser through the real controller API.
4. Receive a scoped route policy while capabilities continue to report that execution and public relay are unavailable.
5. Query the live Hangar and Modrinth services and receive actual projects.
6. Render live catalog results in the workbench.
7. Keep an exact live release blocked when its environment metadata is unknown.
8. Fit the Architect layout at 1440px and 390px without horizontal overflow.
9. Revoke a host in the browser and reject its former credential.
10. Select launcher components independently; add Java only with Minecraft.
11. Persist launcher preferences without manufacturing downloaded or installed inventory.
12. Fit the launcher at 1440px and 390px without horizontal overflow.
13. Save and restore an empty launcher selection.
14. Complete without uncaught browser JavaScript errors.

Live searches returned HTTP 200 and ten matching results from each catalog. A live Modrinth LuckPerms release (`b0mk8uS6`, `v5.5.71-bukkit`) reported an unknown release environment even though its project aggregate reported server-only. Resolution correctly remained blocked. This is an observation from 19 October 2026, not a promise about future metadata.

Live testing exposed two issues that were corrected before the final run: Modrinth's search-index plugin classification differs from its returned legacy `project_type`, and long release metadata needed wrapping on narrow screens.

Desktop and mobile screenshots were inspected. The test servers were stopped after acceptance. No real Discord account was linked, plugin artifact was downloaded, Minecraft process was started or public port was opened.

## Release gates that remain

- Register Metallic-owned Discord and Microsoft applications and validate real consent, logout, revocation and account recovery flows.
- Build a public multi-account identity service with durable ownership, authorization, auditing, retention and recovery rules. The workbench currently has one private administrator.
- Replace the in-memory host registry with durable records, credential renewal and signed route capabilities. Implement an authenticated outbound tunnel and its regional relay.
- Enforce actual host isolation, process identity, network policy, quotas and revocation in the native agent. A loopback port policy is not an operating-system sandbox.
- Implement verified native downloads, signed update metadata, durable receipts, staging, atomic install/rollback and Minecraft ownership authentication.
- Add catalog caching, provider-wide rate budgets and bounded retry/backoff before a public catalog rollout. Resolve full dependencies and validate isolated installation before executing third-party plugins.
- Select and contract for upstream DDoS protection and reseller rights; measure real player latency, packet loss, tick behavior and failure recovery.
- Implement the public server directory, in-game browser, moderation, truthful health reporting and clearly labeled sponsorships.

The Windows cross-build does not establish Windows host sandbox support. Hash validation does not establish plugin trust or malware safety. No DDoS attack simulation, saturation test or player-capacity benchmark was performed.

For design and source references, see [Architect platform](architect-platform.md). For running the private workbench, see [Architect workbench](architect-workbench.md).
