# Offline benchmark evidence screening

The benchmark gate screens an imported comparison of two engine artifacts under matched conditions. It reads bounded local JSONL traces, recomputes sample counts, nearest-rank p99 and tick-duration deadline misses, verifies file identity, and reports per-repetition outcomes. It runs no bots, sends no network requests, changes no server configuration and approves no deployment.

A result of `passed_screening` means the supplied evidence passed the proposed descriptive checks. It does **not** prove statistical significance, authentic collection, production readiness, DDoS protection or superiority to another Minecraft network. `productionReady` is always false. Counts and timing summaries must now agree with the raw records. An external measurement harness must still establish that those records are authentic and that its independent correctness assertions follow from actual gameplay.

## Run

The verifier targets Linux. Its traversal uses directory file descriptors and `O_NOFOLLOW` for every path component, rejecting symlink files and directories. Other operating systems fail closed with `insufficient_evidence`; they can still parse the manifest and JSONL schemas.

From the repository root:

```sh
go build -o /tmp/metallic-benchmark-gate ./cmd/metallic-benchmark-gate
/tmp/metallic-benchmark-gate \
  --input /path/to/comparison.json \
  --artifact-root /path/to/immutable-run-artifacts
```

Use `--input -` to read JSON from stdin. `--artifact-root` must be explicitly supplied before any screening pass. Only paths within that root are used for traces. No paths are inferred from the JSON file's location. The verifier does not download missing files or follow URLs.

| Exit code | Meaning |
| --- | --- |
| 0 | `passed_screening`; inspect the limitations and descriptive deltas |
| 1 | `insufficient_evidence` or `failed_screening`; reasons are in JSON output |
| 2 | Invalid CLI/input JSON, I/O failure reading the manifest, or output failure |

`go run` adds its own wrapper around a program's exit status. Use the built executable when a calling script needs the codes above.

## Input contract

The exact, case-sensitive JSON shape is defined by exported structs in `internal/benchgate/gate.go`. Unknown fields, duplicate object keys, trailing JSON, non-finite numbers, oversized collections and more than 512 KiB of manifest data are rejected. There is no input policy override.

Top-level fields are:

| Field | Requirement |
| --- | --- |
| `schemaVersion` | Exactly `metallic.benchmark-gate/v2`; legacy summary-only v1 cannot pass |
| `evidenceKind` | `measured` for eligibility; synthetic, unknown or missing markers cannot pass |
| `declaredChanges` | Exactly `["engineArtifact"]` in v2 |
| `baseline`, `candidate` | Distinct run IDs, engine identity, controlled dimensions, and 3–30 repetitions each |

The engine object requires `name`, `version` and a lowercase `sha256`. Baseline and candidate must have different engine hashes. Engine identity is deliberately separate from `match.configurationSHA256`, so the intended engine change does not invalidate its own comparison. Only engine artifact changes are admitted by this schema. Changes to JVM, plugin set, workload, rules or configuration require their own experiment design and a future schema, rather than disguising them as an engine change. The v2 change introduces mandatory raw-trace recomputation; it does not broaden the allowed experimental changes.

### Matched dimensions

Every run's `match` object contains these hashes, all required and identical between baseline and candidate:

| Hash | What the external harness should record |
| --- | --- |
| `hardwareSHA256` | CPU/topology, memory, storage, resource quotas/affinity and relevant host conditions |
| `runtimeSHA256` | OS/kernel, JVM build, flags and runtime/instrumentation settings |
| `worldSHA256` | Initial world/template snapshot, pregeneration state and required assets |
| `workloadSHA256` | The scripted behavior, seed, rates, distribution and phase definition |
| `harnessSHA256` | Measurement/load-harness code and collection configuration |
| `configurationSHA256` | Effective shared engine/game configuration, excluding engine binary identity |
| `pluginsSHA256` | Required plugin artifacts, versions and configuration manifest |
| `clientMatrixSHA256` | Device/client cohort manifest and validation plan |
| `gameplayContractSHA256` | Required mechanics, visual workload and behavioral invariants |

These source hashes are **imported assertions**. This tool does not recompute them from a world directory, live host, engine JAR or configuration. The harness must retain canonical source manifests and their provenance for review. A syntactically valid digest does not prove that the controlled dimension was actually held constant.

`activePlayers` is a positive count up to 100,000. The count is a bounded schema value, **not** a supported capacity claim. `clients` contains 1–32 explicit cohorts whose counts sum to `activePlayers`. Each cohort has `edition`, `version`, `transport`, `input`, and `players`. Editions are `java` with transport `tcp`, or `bedrock` with `nethernet`/`raknet`; inputs are `keyboard-mouse`, `controller`, or `touch`. Version strings and the complete cohort distribution must match. Ordering of cohorts is immaterial. Duplicate cohorts are rejected.

For a crossplay performance claim, actually include representative Bedrock cohorts and device validation. A Java-only comparison remains Java-only. Current NetherNet compatibility requires more than a successful signaling request; the external harness must measure an actual gameplay session. This gate does not probe transports.

### Repetitions and collection windows

Each repetition has a globally unique `id`, a `pairId` used once in each run, and `startedAt` as a UTC RFC3339 timestamp ending in `Z`. `startedAt` includes warmup. The collection window ends after `warmupSeconds + durationSeconds`. All windows must have completed and must not overlap on the matched hardware. This prevents simply reusing a collection window under another repetition ID, although it cannot prove real independent collection.

Baseline and candidate must provide the same pair IDs, measured duration and warmup per pair. A repetition's `tickSamples` and `interactionSamples` must equal their trace descriptors' counts and the actual parsed record counts. Each trace is capped at 200,000 samples. Many ticks within one run are not statistically independent experimental repetitions.

`metrics` requires explicit `p99TickMs`, `p99InteractionMs`, and `deadlineMissFraction`. Latencies must be finite, positive and at most 60,000 ms. The deadline-miss fraction must be in [0,1]. Missing/null values are distinct from a genuine zero fraction. Imported p99 values must round to the recomputed integer nanosecond value; the deadline fraction must agree within an absolute tolerance of `1e-12`. Comparisons always use the recomputed values, not the declared summaries. Average TPS alone cannot satisfy the schema.

`correctness` requires explicit results for `inventoryConserved`, `economyConserved`, `persistencePassed`, `transfersPassed`, and `pluginCompatibilityPassed`. The separate correctness trace must include exactly one explicit result for each check and agree with the manifest. Missing or inconsistent results make evidence insufficient. A matching false result in either run fails screening. Timing results never infer gameplay correctness. Define the required plugin/API behavior in the matched gameplay contract; a pluginless custom arena cannot claim Paper compatibility merely by setting a flag.

### Trace artifacts

Every repetition requires three descriptors: `traces.ticks`, `traces.interactions`, and `traces.correctness`. Each has `path`, `sha256`, `sizeBytes`, and `sampleCount`. Correctness must record exactly five named checks. Paths must be relative, use `/`, and contain no `.`/`..` components, drive letter, URL or backslash. Each descriptor path must be distinct across repetitions.

The verifier opens files beneath the explicit artifact root, accepts regular files only, checks exact nonzero size and SHA-256, and detects size/mtime changes while reading. Parsing and hashing use the same open descriptor in one pass. It rejects missing files, symlinks, directories and FIFOs. A single trace is capped at 16 MiB and the declared total at 128 MiB; hashing is streamed. Keep evidence immutable during import. The supported target is normal local storage; the verifier does not provide a wall-clock guarantee for a hung filesystem.

All three traces must use the documented [`metallic.benchmark-trace/v1` JSONL format](benchmark-traces.md): a bound header, contiguous sequence numbers, ordered timestamps, and a completion footer with explicit zero dropped and incomplete samples. Missing final newlines, partial files, malformed records, unsupported schemas, duplicate/unknown fields, wrong identities/windows and count disagreements fail closed. Exactly five independent correctness records are required.

The successful evidence mode is `trace_metrics_recomputed`. `traceSummaries` reports the parsed counts, exact p99 nanoseconds, tick-duration misses and independent correctness assertions. Authentic collection still cannot be established merely by changing an evidence marker to `measured`. No live measurement collector is installed by this importer.

## Proposed policy

These defaults are design proposals for preliminary screening. They are not benchmark results or service promises.

| Check | Default |
| --- | --- |
| Repetitions per run | At least 3, at most 30 |
| Measured time per repetition | At least 600 seconds, at most 86,400 |
| Warmup per repetition | At least 120 seconds, at most 7,200 |
| Raw tick samples per repetition | At least 10,000 |
| Raw interaction samples per repetition | At least 1,000 |
| Candidate p99 tick duration | At most 50 ms |
| Candidate deadline-miss fraction | At most 1% |
| Candidate p99 tick regression per matched pair | At most 5% relative to baseline |
| Candidate p99 interaction regression per matched pair | At most 5% relative to baseline |
| Correctness | All five required assertions true in both runs |

Every matched pair is evaluated; a good run cannot average away a bad one. Baseline and candidate results must use the same definition of tick and interaction, including hotspot/region scope. Run separate comparisons for dense lobbies, spread SMP, exploration, crowded hotspots, combat, creator edits, lifecycle and multi-tenant contention. Do not pool dissimilar workloads into one flattering result.

`tickDeltaMs` and `interactionDeltaMs` are candidate minus baseline; negative is an improvement in the recomputed p99. The output reports `tickImproved` and `interactionImproved` independently. A small regression within tolerance can pass screening and will still be reported as a regression. Passing is not itself a “faster” claim. No confidence interval or hypothesis test is calculated; repeated runs and external statistical analysis remain necessary.

## Example and tests

`examples/benchmark-gate/synthetic.json` is an explicitly synthetic shape example. Its hashes, metrics and sample counts are invented for illustration. No trace files are supplied. It must report `insufficient_evidence` and cannot establish any Metallic performance result. A real collector should create its own manifest and actual trace files.

```sh
/tmp/metallic-benchmark-gate \
  --input examples/benchmark-gate/synthetic.json \
  --artifact-root examples/benchmark-gate
go test ./internal/benchgate ./cmd/metallic-benchmark-gate
```

The tests create isolated synthetic files to exercise nearest-rank arithmetic, deadline boundaries, record ordering/completion, malformed and oversized traces, raw/summary disagreement, independent correctness failures, engine-only comparisons, future/overlap rejection, tampering, symlink traversal, directories, FIFO nonblocking behavior and CLI exit status. They test the importer, not Minecraft capacity.
