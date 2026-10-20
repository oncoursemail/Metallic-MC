// Package diagnostics recognizes bounded Minecraft/JVM log signatures without
// executing commands, uploading logs, or returning user-controlled log text.
// Findings are troubleshooting hypotheses, not proof of a crash's root cause.
package diagnostics

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	Schema       = "metallic.diagnostics/v1"
	MaxLogBytes  = 1 << 20
	MaxLogLines  = 10000
	MaxLineBytes = 64 << 10
	MaxFindings  = 16
	MaxEvidence  = 3
)

var (
	ErrLogTooLarge       = errors.New("diagnostics: log exceeds byte limit")
	ErrTooManyLines      = errors.New("diagnostics: log exceeds line limit")
	ErrLineTooLong       = errors.New("diagnostics: log line exceeds byte limit")
	ErrInvalidUTF8       = errors.New("diagnostics: log must be valid UTF-8")
	ErrInvalidSupervisor = errors.New("diagnostics: supervisor exit code is invalid")
)

// Request accepts a bounded log excerpt. Supervisor is deliberately excluded
// from JSON: HTTP callers must not manufacture trusted executor observations.
// The embedding service may attach observations from its own supervisor.
type Request struct {
	Log        string                 `json:"log"`
	Supervisor *SupervisorObservation `json:"-"`
}

// SupervisorObservation belongs to the same process generation as Log. Nil
// values mean unknown. The caller must verify its origin and association; this
// pure analyzer does not authenticate observations or inspect a container.
type SupervisorObservation struct {
	ExitCode           *int
	ContainerOOMKilled *bool
}

// Evidence contains a fixed signal name, never an excerpt. Line is one-based
// for log evidence and omitted for supervisor observations.
type Evidence struct {
	Source string `json:"source"`
	Line   int    `json:"line,omitempty"`
	Signal string `json:"signal"`
}

type Finding struct {
	Code           string     `json:"code"`
	Severity       string     `json:"severity"`
	Confidence     string     `json:"confidence"`
	Title          string     `json:"title"`
	Explanation    string     `json:"explanation"`
	Evidence       []Evidence `json:"evidence"`
	EvidenceCapped bool       `json:"evidenceCapped"`
	NextChecks     []string   `json:"nextChecks"`
}

type Report struct {
	Schema              string    `json:"schema"`
	InputBytes          int       `json:"inputBytes"`
	InputLines          int       `json:"inputLines"`
	Analysis            string    `json:"analysis"`
	SupervisorAvailable bool      `json:"supervisorAvailable"`
	Findings            []Finding `json:"findings"`
	Notes               []string  `json:"notes"`
}

type rule struct {
	code, severity, confidence, title, explanation string
	checks                                         []string
	match                                          func(string) bool
}

var rules = []rule{
	{"eula_startup_refused", "error", "high", "EULA startup check refused this launch",
		"The excerpt contains an EULA refusal signature. It does not establish whether the owner already accepted the EULA for another server or generation.",
		[]string{"Check that this server generation uses the owner's existing EULA acceptance record and intended runtime directory.", "Do not change legal acceptance automatically; correct a missing approved acceptance record through the manager."},
		func(s string) bool {
			return strings.Contains(s, "you need to agree to the eula") || strings.Contains(s, "you must agree to the eula")
		}},
	{"port_bind_failed", "error", "medium", "Listener could not bind",
		"A bind-failure signature can indicate an occupied port, an unavailable address, or a networking configuration error.",
		[]string{"Check the assigned listener address and port against this server's allocation.", "Ask the supervisor whether a previous generation still owns the listener; do not kill unrelated processes."},
		func(s string) bool {
			return containsAny(s, "failed to bind to port", "java.net.bindexception", "address already in use")
		}},
	{"java_runtime_version", "error", "high", "Java class format is unsupported by this runtime",
		"The JVM reports an unsupported class-file version. The log alone does not establish which installed Java executable or dependency should replace it.",
		[]string{"Compare the actual Java executable and version with the pinned server and plugin requirements.", "Test a supported runtime or compatible artifact in a copied checkpoint before restarting production."},
		func(s string) bool { return strings.Contains(s, "unsupportedclassversionerror") }},
	{"java_heap_pressure", "error", "high", "Java heap allocation or collection failed",
		"A Java heap or GC-overhead signature is present. This can result from sizing, load, or a leak and is distinct from a supervisor-reported container kill.",
		[]string{"Compare the configured heap with actual heap use, GC activity, and the container's total memory budget.", "Collect a bounded local memory profile while reproducing the issue; preserve headroom for native memory.", "Do not increase the heap beyond the allocation or collect a heap dump without enough protected storage."},
		heapMemory},
	{"java_native_pressure", "error", "medium", "Native memory, direct buffers, or thread allocation failed",
		"A native allocation signature is present. Memory pressure, address-space limits, or process/thread limits require separate supervisor evidence.",
		[]string{"Inspect native/direct-buffer usage, thread counts, and the assigned process and memory limits.", "Review the protected JVM fatal-error report and supervisor memory events if available.", "Do not assume that increasing the Java heap will solve native allocation failures."},
		nativeMemory},
	{"java_metadata_pressure", "error", "high", "JVM class metadata allocation failed",
		"A Metaspace or compressed-class-space signature is present. Repeated plugin loading or a configured limit can contribute; the excerpt does not prove either cause.",
		[]string{"Inspect class loading and the configured Metaspace or compressed-class-space limit.", "Use a clean restart and a checkpoint-based reproduction rather than hot-reloading plugins."},
		metadataMemory},
	{"java_memory_unspecified", "error", "low", "Java reported an unclassified memory error",
		"An OutOfMemoryError appears without a recognized subtype. It does not establish heap exhaustion or a container OOM kill.",
		[]string{"Read the associated exception detail in the protected local log and compare JVM and supervisor memory observations.", "Avoid changing memory limits until the failed allocation type is known."},
		func(s string) bool {
			return strings.Contains(s, "outofmemoryerror") && !heapMemory(s) && !nativeMemory(s) && !metadataMemory(s)
		}},
	{"plugin_dependency_missing", "error", "medium", "A required plugin dependency may be missing",
		"The log reports a dependency-resolution failure. The required dependency and compatible version need verification against the pinned plugin manifest.",
		[]string{"Inspect the affected plugin's declared dependencies and supported server versions.", "Stop and snapshot before changing plugins; test the dependency set together in an isolated copy.", "Use verified project artifacts rather than downloading a file named in untrusted log text."},
		func(s string) bool {
			return containsAny(s, "unknowndependencyexception", "missing required dependenc", "missing mandatory dependenc")
		}},
	{"class_linkage_failure", "error", "medium", "A class could not be loaded or initialized",
		"A class-loading signature may reflect a missing dependency, incompatible artifact, or earlier initialization failure. It does not identify a guilty plugin by itself.",
		[]string{"Review the earliest related exception and the complete dependency/version manifest.", "Stop and snapshot before a dependency-aware plugin isolation test in a copied server."},
		func(s string) bool { return containsAny(s, "noclassdeffounderror", "classnotfoundexception") }},
	{"tick_stall", "error", "medium", "The watchdog observed a stalled server tick",
		"A watchdog signature indicates a stalled tick. It does not distinguish plugin work, world loading, garbage collection, CPU throttling, or storage delay.",
		[]string{"Correlate the watchdog thread dump with CPU throttling, disk latency, and GC activity.", "Capture a bounded local spark profile while the stall is occurring; keep the report private by default.", "Compare the latest change with a known-good checkpoint before changing plugins or world data."},
		func(s string) bool {
			return strings.Contains(s, "a single server tick took") || (strings.Contains(s, "server has not responded for") && strings.Contains(s, "seconds"))
		}},
	{"storage_exhausted", "error", "high", "Storage allocation failed",
		"The log contains a disk-space or quota-exhaustion signature. Space, inode, and quota observations must identify the actual constrained volume.",
		[]string{"Check this server's writable volume for free bytes, free inodes, and quota exhaustion.", "Preserve a verified checkpoint before targeted cleanup; do not delete world or plugin files automatically.", "Check whether the last save completed before allowing the server to resume."},
		func(s string) bool { return containsAny(s, "no space left on device", "disk quota exceeded") }},
	{"filesystem_access_denied", "error", "medium", "A filesystem operation was denied",
		"A file-access denial can indicate ownership, mount, path, or policy configuration. Read-only managed compatibility files are intentionally protected.",
		[]string{"Check whether the denied operation targets tenant-writable data or the protected platform layer.", "Verify expected ownership and mount permissions for this server only.", "Do not disable sandbox controls or grant broad host permissions to suppress the error."},
		func(s string) bool {
			return containsAny(s, "java.nio.file.accessdeniedexception", "permission denied", "read-only file system")
		}},
	{"server_api_version", "error", "medium", "An artifact declares an unsupported server API or version",
		"A server/API compatibility rejection is present. The correct server, loader, and plugin combination requires checking the compatibility manifest.",
		[]string{"Compare the server, loader, and plugin API requirements with the selected compatibility profile.", "Test an approved compatible combination on a checkpoint; protocol translation cannot supply missing server APIs."},
		func(s string) bool { return containsAny(s, "unsupported api version", "unsupported minecraft version") }},
	{"client_protocol_version", "warning", "medium", "A client/server protocol mismatch was reported",
		"A version-related join rejection appears in the log. This can explain a failed join without explaining a server crash.",
		[]string{"Compare the joining client's edition and version with the tested compatibility matrix.", "Check the managed ViaVersion/ViaBackwards or Bedrock bridge profile and the corresponding join test."},
		func(s string) bool {
			return containsAny(s, "outdated client", "outdated server", "unsupported protocol version")
		}},
}

// Analyze is deterministic and has no filesystem, network, clock, or process
// side effects. Limits are checked before diagnosis; over-limit input is rejected
// instead of silently omitting the lines that might explain the failure.
func Analyze(req Request) (Report, error) {
	if len(req.Log) > MaxLogBytes {
		return Report{}, ErrLogTooLarge
	}
	if !utf8.ValidString(req.Log) {
		return Report{}, ErrInvalidUTF8
	}
	if req.Supervisor != nil && req.Supervisor.ExitCode != nil && (*req.Supervisor.ExitCode < 0 || *req.Supervisor.ExitCode > 255) {
		return Report{}, ErrInvalidSupervisor
	}
	lineCount := 0
	if req.Log != "" {
		lineCount = strings.Count(req.Log, "\n") + 1
		if strings.HasSuffix(req.Log, "\n") {
			lineCount--
		}
	}
	if lineCount > MaxLogLines {
		return Report{}, ErrTooManyLines
	}
	lines := strings.Split(req.Log, "\n")
	for i := 0; i < lineCount; i++ {
		if len(lines[i]) > MaxLineBytes {
			return Report{}, ErrLineTooLong
		}
	}
	report := Report{
		Schema: Schema, InputBytes: len(req.Log), InputLines: lineCount,
		Analysis: "signature-hypotheses", Findings: []Finding{},
		Notes: []string{"Log evidence may be incomplete, old, or forged; confidence describes a signature match, not a verified root cause.", "No raw log text is included and no files, settings, or processes were changed.", "An empty finding list does not establish that a server is healthy."},
	}
	indexes := make(map[string]int, MaxFindings)
	for i := 0; i < lineCount; i++ {
		line := normalizeLine(lines[i])
		for _, rule := range rules {
			if !rule.match(line) {
				continue
			}
			index, exists := indexes[rule.code]
			if !exists {
				index = len(report.Findings)
				indexes[rule.code] = index
				report.Findings = append(report.Findings, Finding{Code: rule.code, Severity: rule.severity, Confidence: rule.confidence, Title: rule.title, Explanation: rule.explanation, Evidence: []Evidence{}, NextChecks: append([]string(nil), rule.checks...)})
			}
			finding := &report.Findings[index]
			if len(finding.Evidence) < MaxEvidence {
				finding.Evidence = append(finding.Evidence, Evidence{Source: "log", Line: i + 1, Signal: rule.code})
			} else {
				finding.EvidenceCapped = true
			}
		}
	}
	if observation := req.Supervisor; observation != nil {
		report.SupervisorAvailable = observation.ExitCode != nil || observation.ContainerOOMKilled != nil
		if observation.ContainerOOMKilled != nil && *observation.ContainerOOMKilled {
			report.Findings = append(report.Findings, Finding{
				Code: "container_memory_kill", Severity: "error", Confidence: "high", Title: "Supervisor reported a container OOM kill",
				Explanation: "The supplied supervisor observation reports an OOM kill for this process generation. It does not identify a memory leak or prove that heap sizing alone caused it.",
				Evidence:    []Evidence{{Source: "supervisor", Signal: "container_oom_killed"}},
				NextChecks:  []string{"Check the supervisor's container memory events and peak total memory for this generation.", "Budget heap, native memory, direct buffers, and process overhead within the assigned allocation before changing limits."},
			})
		} else if observation.ExitCode != nil && *observation.ExitCode == 137 {
			report.Findings = append(report.Findings, Finding{
				Code: "process_killed_unknown", Severity: "error", Confidence: "low", Title: "Supervisor reported exit 137; cause is unresolved",
				Explanation: "Exit 137 commonly accompanies SIGKILL, but an exit status alone does not prove a signal or an OOM kill. Forced shutdown, administrative action, and other causes require separate evidence.",
				Evidence:    []Evidence{{Source: "supervisor", Signal: "exit_137"}},
				NextChecks:  []string{"Inspect the supervisor's termination reason, operation audit, and memory events for this exact generation.", "Do not automatically increase memory or restart a suspended server based on the exit code."},
			})
		}
	}
	return report, nil
}

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func heapMemory(s string) bool {
	return strings.Contains(s, "outofmemoryerror") && containsAny(s, "java heap space", "gc overhead limit exceeded", "requested array size exceeds vm limit")
}

func nativeMemory(s string) bool {
	return (strings.Contains(s, "outofmemoryerror") && containsAny(s, "native thread", "direct buffer memory", "cannot reserve")) ||
		(strings.Contains(s, "native memory allocation") && strings.Contains(s, "failed")) ||
		strings.Contains(s, "could not reserve enough space for object heap")
}

func metadataMemory(s string) bool {
	return strings.Contains(s, "outofmemoryerror") && containsAny(s, "metaspace", "compressed class space")
}

// normalizeLine removes ANSI CSI/OSC sequences and replaces other controls with
// a space. It never renders terminal escapes. Work is linear in the bounded line.
func normalizeLine(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) {
					c := s[i]
					i++
					if c >= 0x40 && c <= 0x7e {
						break
					}
				}
			} else if i < len(s) && s[i] == ']' {
				i++
				for i < len(s) {
					if s[i] == 7 {
						i++
						break
					}
					if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			} else {
				out.WriteByte(' ')
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			out.WriteByte(' ')
		} else {
			out.WriteRune(unicode.ToLower(r))
		}
	}
	return out.String()
}
