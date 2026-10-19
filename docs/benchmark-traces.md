# Raw benchmark traces

`metallic.benchmark-gate/v2` requires three JSONL files per repetition, using `metallic.benchmark-trace/v1`: `ticks`, `interactions`, and `correctness`. The existing `metallic-benchmark-gate` CLI imports them through their manifest descriptors. No additional process or network service is required to read them.

This is an **import format**, not a deployed recorder. The importer checks file identity, record structure, counts, timing arithmetic and agreement with the manifest. It does not establish that a claimed server, client or gameplay event produced the records. A pinned external harness still owns collection and provenance.

## Record framing and limits

- UTF-8 JSON, one object per line; LF or CRLF is accepted. Every line, including the last, must end with a newline.
- Exactly one header first, then samples or correctness checks, then exactly one completion footer. Empty lines and any content after completion are rejected.
- Object keys are exact and case-sensitive. Unknown keys, duplicate keys, nested objects/arrays where scalars are required, extra JSON values and unsupported schema versions are rejected.
- Nanosecond values, sequence numbers and counts are unsigned JSON integers. Fractional values, negative values, overflow and `null` are rejected where an integer is required.
- Maximum 4,096 bytes per line including its line ending, 16 MiB per file, 200,000 sample/check records per file, and 128 MiB of declared trace files per comparison.

The importer retains only a bounded duration array for exact percentile calculation. It parses and hashes the same bytes on the same already-confined file descriptor. The existing Linux path restrictions apply; no unverified platform fallback is added.

## Header

Every trace begins with these exact fields:

| Field | Meaning |
| --- | --- |
| `type` | `header` |
| `schemaVersion` | `metallic.benchmark-trace/v1` |
| `kind` | `ticks`, `interactions`, or `correctness` |
| `evidenceKind` | `measured` or `synthetic`; only `measured` is eligible for the gate |
| `runId` | Exact manifest run ID |
| `repetitionId` | Exact manifest repetition ID |
| `durationNs` | Measured window length, equal to the manifest's `durationSeconds * 1000000000`; warmup is excluded |

IDs use the same restricted syntax as the manifest. The header's duration must be positive and no more than 86,400 seconds. All three headers must bind to their own run and repetition. A synthetic trace cannot pass with a measured manifest, and changing an evidence label is not proof of authentic collection.

## Timing samples

Tick and interaction traces contain sample records with these exact fields:

| Field | Meaning |
| --- | --- |
| `type` | `sample` |
| `sequence` | Contiguous index starting at 1; duplicate, missing, skipped and reordered indexes are rejected |
| `elapsedNs` | Observation-completion timestamp relative to the start of the measured window, in `(0, durationNs]` |
| `durationNs` | Observed elapsed duration, from 1 through 60,000,000,000 ns |

`elapsedNs` must increase for ticks. For interactions it may stay equal when simultaneous completions share a clock reading, but must never decrease. Use a monotonic clock for the interval and sample durations; wall-clock adjustments must not produce negative or reordered durations. A completion can describe work that started before the window boundary; the pinned harness must use the same boundary convention for both engines.

Each tick trace must represent one consistently defined server or region scope. Do not combine simultaneous independent Folia region ticks into a purported single tick loop. Define the scope, instrumented duration, client actions, completion acknowledgments, timeout behavior and window-boundary handling in the matched workload/harness/gameplay contracts. The importer cannot discover an omitted region or client from a duration alone.

The following short example is deliberately **synthetic**, under the gate's minimum duration/sample count and unsuitable for a performance claim:

```jsonl
{"type":"header","schemaVersion":"metallic.benchmark-trace/v1","kind":"ticks","evidenceKind":"synthetic","runId":"example-run","repetitionId":"example-r1","durationNs":1000000000}
{"type":"sample","sequence":1,"elapsedNs":100000000,"durationNs":12000000}
{"type":"sample","sequence":2,"elapsedNs":200000000,"durationNs":20000000}
{"type":"sample","sequence":3,"elapsedNs":300000000,"durationNs":60000000}
{"type":"complete","sampleCount":3,"droppedSamples":0,"incompleteSamples":0,"durationNs":1000000000}
```

## Completion footer

The recorder writes the footer only after the **entire measured window has closed** and all its records have been flushed. A crash, interrupted export or missing final newline must remain an incomplete trace.

| Field | Requirement |
| --- | --- |
| `type` | `complete` |
| `sampleCount` | Positive actual sample/check count, equal to the parsed records and the manifest descriptor |
| `droppedSamples` | Required integer, exactly 0 to be eligible |
| `incompleteSamples` | Required integer, exactly 0 to be eligible |
| `durationNs` | Required, equal to the header's complete measured window |

Collectors must count queue overflow, failed serialization or other lost observations as dropped samples. Actions still awaiting a required completion when collection ends must be tracked as incomplete according to the fixed harness contract. Do not discard slow or timed-out actions and report only the fast survivors. A nonzero loss/incomplete count causes `insufficient_evidence`; there is no option to ignore it.

The footer documents the recorder's completion assertion. It can reveal a partial file or admitted loss, but it cannot prove that a malicious or defective recorder observed every event.

## Exact calculations and manifest agreement

For `n` durations, sort the unsigned nanosecond values and choose the one at the one-based rank `ceil(99 * n / 100)`. No interpolation is used. Thus the p99 of 101 ascending values `1..101` is `100`. Millisecond output is that integer nanosecond value divided by `1000000`.

For ticks, a duration **strictly greater than 50,000,000 ns** is a deadline miss. Exactly 50 ms is not a miss. `deadlineMissFraction` is the count of those misses divided by the actual tick sample count. This is a tick-duration threshold; it does not independently measure scheduled-start lateness, network RTT or player input latency.

Declared tick/interaction counts must equal the parser's counts. Declared p99 milliseconds must round to the same integer nanosecond value; this tolerates only sub-nanosecond summary rounding. The declared miss fraction must agree within an absolute tolerance of `1e-12`. The gate compares **recomputed values**, and returns them in `traceSummaries`; it never substitutes a favorable declared summary. A mismatch produces `insufficient_evidence`.

## Independent correctness records

The correctness file has the same header/footer framing, with `kind: correctness`. Its records use these exact fields:

```jsonl
{"type":"check","sequence":1,"elapsedNs":1000000000,"name":"inventoryConserved","passed":true}
```

It must contain exactly one explicit boolean result for each of:

1. `inventoryConserved`
2. `economyConserved`
3. `persistencePassed`
4. `transfersPassed`
5. `pluginCompatibilityPassed`

Sequences are contiguous starting at 1; timestamps are nondecreasing and within the completed window. The records may appear in any check-name order. Duplicate/unknown names, absent/null results or missing checks fail closed. The five parsed booleans must agree with the five manifest booleans. A consistent `false` result causes `failed_screening` even if every timing metric improves. Timing measurements never fill in a missing correctness result.

Each boolean remains an assertion supplied by the harness. Its gameplay contract should point to retained inventory/economy state, persistence/restore evidence, transfer outcomes and the tested plugin/API behavior. Importing five `true` values does not recreate or authenticate those checks.

## Migration from the earlier gate

The gate now requires `schemaVersion: metallic.benchmark-gate/v2`. Arbitrary text files accepted for identity screening by gate v1 are no longer eligible. Do not relabel old files as JSONL or infer records from their declared p99 summaries: a percentile does not reconstruct the underlying samples. Re-export retained raw data into this format with its original provenance, or collect a new run.

The outcome remains descriptive screening. No live load, successful client session, production deployment, statistical significance or comparative superiority is established by this import step.
