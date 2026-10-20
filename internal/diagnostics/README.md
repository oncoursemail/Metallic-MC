# Metallic diagnostics

`Analyze(Request) (Report, error)` is an offline, deterministic log-signature
analyzer. It runs no commands, reads no paths, makes no network calls, uploads no
logs, and applies no fixes. It is a first diagnostics component rather than a
complete debugger or a guarantee that a crashing server can be repaired.

## Input and trust

`Request.Log` contains a bounded excerpt. Limits are 1 MiB of UTF-8, 10,000 lines,
and 64 KiB per line. A terminal newline does not add an extra empty line. Invalid
UTF-8 and over-limit input produce typed errors with a zero report. There is no
silent truncation: callers should explain that a narrower local excerpt is needed.

The optional `SupervisorObservation` accepts an exit code in the range 0–255 and
an optional container OOM flag. The embedding service is responsible for binding
these observations to the right server and process generation and obtaining them
from its trusted executor. `Request.Supervisor` has `json:"-"`, so an untrusted
JSON request cannot manufacture that evidence. HTTP handlers should additionally
reject unknown JSON fields. Logs saying `OOMKilled=true` or `exit=137` do not create
supervisor evidence.

The function has no authentication boundary of its own. Mutable request data,
including the supervisor pointers, must not be changed concurrently by callers.
Independent calls are safe concurrently.

## Output

The v1 report returns at most 16 findings with at most 3 evidence entries each.
Findings follow first matching line order, with a fixed rule order for ties;
supervisor evidence follows log findings. Evidence uses fixed signal identifiers
and one-based line numbers, never raw log excerpts, class names, paths, hosts,
player names, IPs, or tokens extracted from the input. Next checks are fixed,
conservative text. An `evidenceCapped` flag indicates additional matching lines.
ANSI CSI/OSC sequences are removed for matching; remaining Unicode control and
format characters are replaced with spaces. No terminal escapes are rendered.

Recognized signatures cover EULA startup refusal, bind failures, unsupported Java
class versions, Java heap/GC pressure, native/direct-buffer/thread allocation,
class metadata exhaustion, unclassified memory errors, plugin dependencies,
class loading/initialization, watchdog stalls, storage exhaustion, filesystem
denials, unsupported server APIs, and client protocol mismatches. Supervisor OOM
kills are separate from JVM exceptions. Exit 137 alone is explicitly unresolved;
it is not automatically classified as an OOM kill, or even proof of a signal.

Confidence describes how specifically a signature identifies a symptom. It does
not mean a root cause was independently verified. Logs can be stale, partial,
quoted, localized, or forged. A malicious plugin can print any of these strings.
A player join failure can coexist with a healthy server. An empty finding list is
not a health certificate. Nothing here establishes tenant isolation, malware
absence, or a reason to suspend/delete a server.

The analyzer deliberately does not identify one plugin as guilty from a stack
frame. A missing class can result from several causes. Fix proposals require
inspection of the pinned dependency/runtime manifest and a controlled test. Stop
and snapshot before plugin/world changes; do not disable sandbox protections,
download URLs from logs, delete files, or increase memory automatically.

## Evidence and extension

The core behavior is informed by primary documentation, researched 2026-10-20:

- [Paper troubleshooting](https://docs.papermc.io/paper/basic-troubleshooting/):
  stack traces, dependency-aware plugin isolation, watchdogs, and backup advice.
- [Paper profiling](https://docs.papermc.io/paper/profiling/): spark is bundled from
  Paper 1.21 and profiling should occur while the problem happens.
- [spark commands](https://spark.lucko.me/docs/Command-Usage): local profile output
  is available through `profiler stop --save-to-file`.
- [JVM class-format errors](https://docs.oracle.com/en/java/javase/26/docs/api/java.base/java/lang/UnsupportedClassVersionError.html).
- [Oracle memory troubleshooting](https://docs.oracle.com/en/java/javase/21/troubleshoot/troubleshooting-memory-leaks.html):
  heap, class metadata, and native allocation failures have different causes.
- [Docker events](https://docs.docker.com/reference/cli/docker/system/events/):
  runtime OOM and process-exit events are distinct observations.

Future work includes protected evidence retrieval, a manifest change timeline,
real supervisor integrations, local profiling, and checkpoint-based reproduction.
The static analyzer has none of those execution capabilities.

Run the meaningful regression suite with:

```sh
go test -race ./internal/diagnostics
go test ./internal/diagnostics -run '^$' -fuzz FuzzAnalyzeBounded -fuzztime 5s
```

Tests cover symptom distinctions, forged supervisor log claims, JSON trust
boundaries, exit-code uncertainty, bounded input/evidence, ANSI controls,
secret non-disclosure, detached results, determinism, and concurrent use.
