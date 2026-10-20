# Platform foundation implementation

Date: 2026-10-20. This is source-level implementation evidence, not a claim of
deployed Minecraft hosting or a completed desktop launcher. The full product
decision is in [architecture.md](architecture.md). Machine-readable test evidence
is recorded in [validation.json](validation.json).

## Delivered components

| Component | Implemented behavior | Important boundary |
| --- | --- | --- |
| `internal/platform/lifecycle` | Typed owner/moderator planning; local/hosted and private/public distinctions; revision and generation guards; separate boot/admit; expiring proof checks; containment, checkpoint and quarantine ordering | Pure proposals; no executor, durable records or RPC capability; `execution_enabled:false` |
| `internal/platform/syncplan` | Bounded portable inventory; traversal/link/special-file/case/prefix rejection; conventional full-world selection; known-name exclusions; byte totals; canonical review digest; generation conflict | Metadata only; no actual file hashing, upload, extraction, completeness proof or content inspection; `executable:false` |
| `internal/platform/population` | Opaque scoped gateway-source capabilities; monotonic full snapshots; current generation deduplication; retained replay protection; detached results; null totals when coverage is unknown | Trusted in-process inputs only; no real authentication adapter, gateway ingestion endpoint, persistent authority or human/bot proof |
| `internal/diagnostics` | Bounded deterministic Minecraft/JVM signature analysis; supervisor evidence kept separate; fixed descriptions and evidence line numbers; safe next checks | Hypotheses, no raw-log output, external upload, auto-repair or causal certainty |
| `cmd/metallic-doctor` | Offline log-file or standard-input analysis with JSON report | Does not launch Minecraft, access cloud services or alter files |
| Private Server Studio | Authenticated mode descriptions, first-sync inventory preview and chosen-excerpt diagnosis at `/architect/platform` | Development workbench; no invented games/servers/counts and no desktop-client launch |

The new screen is an additive route from the existing Architect workbench. It
does not replace the prior dashboard or choose between the owner's earlier
alternative designs. The two discovery panels describe the target order and
explicitly show that live game releases/listings are not connected.

## Run the actual offline tool

From a normal repository checkout:

```sh
go run ./cmd/metallic-doctor -log /path/to/latest.log
```

Or build it once and pass a local excerpt through standard input:

```sh
go build -o metallic-doctor ./cmd/metallic-doctor
metallic-doctor < /path/to/excerpt.log
```

The input limit is 1 MiB, 10,000 lines and 64 KiB per line. At most 16 findings
and three evidence positions per finding are returned. Oversized/invalid input
is rejected rather than silently truncated. Exit 0 means analysis completed,
including a report with no recognized signatures; it does not certify health.
Exit 2 means input, argument, read or output failure. The report contains no raw
log fragments. Confidence describes signature evidence, not proven root cause.

The analyzer distinguishes heap/GC exhaustion, native/direct-buffer/thread
allocation, metadata pressure, an unspecified JVM memory error and a trusted
supervisor's container OOM report. Exit 137 alone remains an unexplained exit
status; it does not prove a particular signal or an OOM. Supervisor fields are
excluded from JSON; HTTP callers cannot
declare trusted host observations.

## Private HTTP surface

Set `METALLIC_ARCHITECT_ENABLED=true`, keep the controller bound to its private
interface, sign in, and open `/architect/platform`. Existing secure-cookie/origin
requirements apply; see [Architect setup](../architect-workbench.md).

All endpoint suffixes below use `/api/v1/architect/platform/`:

| Method and suffix | Result |
| --- | --- |
| `GET capabilities` | Explicitly separates implemented planners from absent runtime, auth, directory and population integrations; four mode descriptions |
| `POST sync/plan` | First-import inventory validation at remote generation zero; current draft policy ID and no fabricated acceptance/checkpoint evidence |
| `POST diagnose` | Analyze the deliberately submitted log excerpt on this controller; response contains no raw excerpts |
| `POST lifecycle/plan` | Plan against an explicitly configured trusted state reader; returns 503 when no authoritative reader exists |

Every route requires the private administrator session. POST also requires
`X-Metallic-Intent: architect`, accepted Origin and non-cross-site fetch context.
Lifecycle planning requires authentication within 15 minutes. JSON media type,
unknown fields and trailing values are checked. Each request is bounded to
2 MiB, with at most two platform analyses running concurrently; a busy controller
returns 429 and Retry-After. The diagnostic package applies its tighter log limits.
Responses use the controller's no-store and CSP protections.

The browser sends a log excerpt only when Analyze is pressed. Choosing a file
first reads it into the browser's textarea. The endpoint does not persist it or
call an external analyzer. An infrastructure proxy/logger must separately avoid
recording request bodies; the controller cannot control an arbitrary deployment's
logging configuration. Use the CLI for a completely offline workflow.

The sync endpoint accepts `syncplan.Request` directly: `target_mode`,
`source_layout`, `base_remote_generation`, `world_roots` and `entries`.
Each entry has `path`, `kind`, `size` and `sha256`. This endpoint does not accept a
destination server ID because it only previews a prospective first import; it
cannot authorize a subsequent update. Nonzero remote generations return 409.
It intentionally supplies no policy acceptance, checkpoint or isolation proof.

The lifecycle endpoint accepts only `server_id`, `action`, `request_id` and
`expected_revision`. It validates intent before calling the trusted adapter,
derives the owner principal from the session, replaces evaluation time with the
controller clock, and never accepts a principal, state, profile or attestation
from request JSON. The public HTTP surface does not grant moderator actions.

`ConfigurePlatformState(PlatformStateReader)` is a startup extension point for a
future authoritative store. The default has no reader. Existing BYOH host
enrollment/allocation records are deliberately **not** converted into evidence
of a managed runtime, current process phase, accepted policy or active generation.
The read adapter alone still enables no execution. The tests supply controlled
fixtures; those fixtures are not installed in the actual application.

## Caller responsibilities and critical limits

Lifecycle `Evidence` is immutable, non-JSON and evaluated at captured trusted
time. `evaluated_at` makes that historical context explicit. Reusing the evidence
repeats a historical evaluation. The executor must re-read policy, refresh proofs,
compare the revision and physically fence old writers at use time. A returned
allowed proposal is not an execution lease or a signed authorization.

Inventory path validation is not safe filesystem access. The import executor
still needs rooted/descriptor-relative operations, bounded extraction, verified
content hashes and actual stopped/checkpoint state. Known-name exclusions do not
detect arbitrary hidden secrets, malicious renamed binaries or hostile ZIP data.
The review digest has no tenant/server authorization binding and must never be
used directly as a server operation credential.

Population observations are bounded-age account-session evidence. A fresh lease
can still contain a just-disconnected account, so the observed value is not a
mathematical lower bound or a human count. A future adapter must verify the
canonical session authority, exclude pending/lobby/synthetic connections as
appropriate, establish genuine observation freshness and fence real sessions
when a source is retired. The package does none of Minecraft authentication,
account linking, bot classification or transport authentication itself.

The desktop launcher, signed updater, installation planner, protected game pack,
live compatibility service, container/microVM executor, console/file broker,
durable operations, moderated directory and online-count adapter remain future
integration work. The policy revision is a draft, not a recorded acceptance by
future customers. Previously recorded owner Minecraft EULA acceptance is separate.

## Review findings addressed

- Repeated transfers originally allowed obsolete per-source observation slices
  to multiply memory use. Superseded entries are now removed from the previous
  source; a regression verifies the aggregate retention bound.
- The original population comment called partial observations a lower bound.
  Documentation and API comments now describe their actual bounded-age meaning.
- A failed hosted process could previously produce a restart plan without proof
  its old writer stopped. Recovery now revokes, isolates, stops and verifies the
  writer, then checkpoints before advancing generation and booting.
- Lifecycle shape validation now happens before an adapter sees a server ID.
- Evaluation timestamps and historical-evidence semantics are explicit.
- HTTP concurrency is bounded as well as body size; client evidence fields and
  foreign ownership are tested at the authenticated adapter boundary.

Independent review of the sync planner found no blocking defect in its declared
metadata-only contract. This is source review, not an external penetration test.

## Validation and deployment status

The final local verification uses Go 1.27.2 on Linux amd64 against the repository's
Go 1.23 module baseline. The source-only checkout was independently verified
against the 140 blobs of canonical main `81e44c76d9670a3994825fc0139982ea492f3684`
before edits. Build uses `-buildvcs=false` here because this recovered checkout
does not provide ordinary local VCS metadata; the change is delivered through
GitHub using the actual canonical parent, not a synthetic local history.

Final verification passed all 23 Go packages under the race detector: 247
top-level tests, 782 ordinary subtests and 31 fuzz targets/seed cases. The
repository's 29 Python pilot tests and six service-bundle tests also passed.
Go vet, all-package build, Go formatting and JavaScript syntax checks passed.
Browser fixture checks covered the actual HTML/CSS/JavaScript at desktop and
mobile widths, including both forms and invalid JSON; the API responses in that
browser check were fixtures. HTTP handlers were exercised separately by real
Go handler tests. These are not live hosting or Minecraft end-to-end results.

The bounded campaigns run during development are recorded separately from final
regression/race tests. There is no claim of extended fuzzing, production capacity,
live gameplay, cross-tenant exploit testing or installed launcher authentication.
Hosted CI status must be reported separately from local tests; prior hosted runs
failed before test execution and must not be described as green.

This milestone modifies repository source only. It starts no remote runtime,
opens no public listener, changes no DNS/firewall and does not touch legacy Wings.
See validation.json for exact completed checks and any environment limitation.
