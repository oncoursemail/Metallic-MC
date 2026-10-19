# Validation receipt: research foundations

Date: 19 October 2026 UTC. Author: ChatGPT.

## Baselines and scope

- Metallic parent: `74b38332ac28034065ef1e39cd9d9ff7819ab26c`, branch `feat/foundation-control-plane`.
- Star private parent: `0d7bfdb838435221ab60ecd83687e896d34b568e`, branch `main`.
- Star page-reader original blob: `c6d9e687397f4ab28a6a2b96586b70c54df845e1`.
- Both targets remain private repositories. The public Star distribution repository is untouched.
- No existing control design or moderation branch was rewritten. Existing Star fixtures and hydrated reference files were not modified in the proposed change.

## Metallic checks executed

Toolchain: official Go 1.27.2, Linux amd64; standard library only.

```text
go test -race ./...                       PASS
go vet ./...                             PASS
go build -buildvcs=false ./...            PASS
```

The complete race suite covered the existing agent, control and resource-planner packages plus the two new internal packages and CLIs. The edge packages were rerun with the race detector after the independently found diagnostic-escaping issue was fixed, and passed.

The hydrated source snapshot does not have its own working Git metadata. An initial `go build ./...` failed while obtaining VCS status; disabling automatic VCS stamping allowed the build to complete. This is recorded rather than presenting the first build as successful. Normal repository CI should retain its normal metadata behavior.

The new examples deliberately exercise refusal: incomplete edge declarations produce `blocked`, and the synthetic benchmark produces `insufficient_evidence`. Neither is a production configuration or a measured performance result. All test data is local. No traffic load, port scan or deployment was performed.

## Star checks executed

Python 3.12.14, HTTPX 0.28.1, pytest 9.1.1. The focused invocation is recorded in `Star-AI-Private/docs/STAR_FETCH_RESPONSIVENESS_20261019.md`.

```text
61 passed, 2 deselected
```

Root independently repeated the suite. Tests use mocked HTTP, fixed DNS answers and deterministic blocking events. The two existing agent integration cases were excluded because the inspection snapshot lacks the full dependency tree; the full Star suite, live research workflow, evaluation gate and release process were not run. Existing evaluation fixtures were preserved.

The managed filesystem sandbox denied asyncio's local socket self-pipe. A standalone standard-library reproducer established the environmental cause; the normal approval reviewer allowed the offline test run with that local operation. Production code has no polling workaround for the sandbox.

## Creator preview checks executed

Chromium, 1280 x 900 and 390 x 844, reduced-motion preference enabled. All three views were captured and layout inspected. Draft creation, user text containing angle brackets, download and parse of JSON, all simulated joining states/reset, all three original-game concept details, each client menu layout, action descriptions, and arrow/Home/End tab navigation were exercised. No page errors, unexpected external requests or horizontal viewport overflow were observed.

This is a browser prototype check, not a Java/Bedrock client compatibility test. The UI does not allocate resources, run games, connect to Minecraft or verify a protected route.

## Independent review

A second reviewer inspected the edge parser, policy, evidence files, filesystem behavior, CLI, tests and official NetherNet documentation. The reviewer found one issue: decoded control characters in an unknown JSON field name could reach standard error unescaped. The diagnostic now uses quoted output, a regression was added, and the reviewer independently repeated the reproduction. No successful JSON output was possible from that malformed input, before or after the fix.

Root inspected the Star ownership/deadline changes, benchmark validation and scoped trace reading, reviewed the declared limitations, and ran the checks above. This is code review and local validation, not a security audit certification or provider endorsement.

## Remaining release evidence

Review the draft PRs, execute the full application's required checks in its complete checkout, then test a staging integration. Production network approval needs actual provider protocol/tenancy coverage, assigned endpoints, origin and egress isolation, supported client joins, route/MTU/loss observations and failure rehearsal. Performance claims need independently collected matched traces and correctness results; file digests alone do not establish truth.
