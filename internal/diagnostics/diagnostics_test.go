package diagnostics

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestRecognizedSignatures(t *testing.T) {
	tests := []struct{ name, line, code, confidence string }{
		{"eula", "You need to agree to the EULA in order to run the server.", "eula_startup_refused", "high"},
		{"bind", "java.net.BindException: Cannot assign requested address", "port_bind_failed", "medium"},
		{"bytecode", "java.lang.UnsupportedClassVersionError: private.Class", "java_runtime_version", "high"},
		{"heap", "java.lang.OutOfMemoryError: Java heap space", "java_heap_pressure", "high"},
		{"gc", "java.lang.OutOfMemoryError: GC overhead limit exceeded", "java_heap_pressure", "high"},
		{"array", "java.lang.OutOfMemoryError: Requested array size exceeds VM limit", "java_heap_pressure", "high"},
		{"thread", "java.lang.OutOfMemoryError: unable to create native thread", "java_native_pressure", "medium"},
		{"native", "Native memory allocation (malloc) failed to allocate 123 bytes", "java_native_pressure", "medium"},
		{"direct", "java.lang.OutOfMemoryError: Cannot reserve 22 bytes of direct buffer memory", "java_native_pressure", "medium"},
		{"metadata", "java.lang.OutOfMemoryError: Metaspace", "java_metadata_pressure", "high"},
		{"compressed", "java.lang.OutOfMemoryError: Compressed class space", "java_metadata_pressure", "high"},
		{"unknown_memory", "java.lang.OutOfMemoryError: unclassified", "java_memory_unspecified", "low"},
		{"dependency", "org.bukkit.plugin.UnknownDependencyException: private dependency", "plugin_dependency_missing", "medium"},
		{"class", "java.lang.NoClassDefFoundError: private/Type", "class_linkage_failure", "medium"},
		{"initialization", "java.lang.NoClassDefFoundError: Could not initialize class private.Type", "class_linkage_failure", "medium"},
		{"watchdog", "[Watchdog] The server has not responded for 10 seconds! Creating thread dump", "tick_stall", "medium"},
		{"single_tick", "A single server tick took 60.00 seconds", "tick_stall", "medium"},
		{"space", "java.io.IOException: No space left on device", "storage_exhausted", "high"},
		{"quota", "java.io.IOException: Disk quota exceeded", "storage_exhausted", "high"},
		{"permissions", "java.nio.file.AccessDeniedException: /private/path", "filesystem_access_denied", "medium"},
		{"readonly", "java.io.IOException: Read-only file system", "filesystem_access_denied", "medium"},
		{"api", "java.lang.IllegalArgumentException: Unsupported API version 99.0", "server_api_version", "medium"},
		{"protocol", "Disconnecting player: Outdated client!", "client_protocol_version", "medium"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report, err := Analyze(Request{Log: "Startup\n" + test.line + "\n"})
			if err != nil {
				t.Fatal(err)
			}
			if report.Schema != Schema || report.Analysis != "signature-hypotheses" || report.InputLines != 2 || report.SupervisorAvailable || len(report.Findings) != 1 {
				t.Fatalf("unexpected report: %+v", report)
			}
			finding := report.Findings[0]
			if finding.Code != test.code || finding.Confidence != test.confidence || finding.Evidence[0].Line != 2 || finding.Evidence[0].Source != "log" || len(finding.NextChecks) == 0 {
				t.Fatalf("unexpected finding: %+v", finding)
			}
		})
	}
}

func TestSupervisorObservationsRemainDistinctFromLogClaims(t *testing.T) {
	killed := 137
	yes, no := true, false
	tests := []struct {
		name string
		obs  *SupervisorObservation
		want string
	}{
		{"missing", nil, ""},
		{"empty", &SupervisorObservation{}, ""},
		{"exit_only", &SupervisorObservation{ExitCode: &killed}, "process_killed_unknown"},
		{"not_oom", &SupervisorObservation{ExitCode: &killed, ContainerOOMKilled: &no}, "process_killed_unknown"},
		{"oom", &SupervisorObservation{ExitCode: &killed, ContainerOOMKilled: &yes}, "container_memory_kill"},
		{"oom_only", &SupervisorObservation{ContainerOOMKilled: &yes}, "container_memory_kill"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report, err := Analyze(Request{Log: "Untrusted log: supervisor OOMKilled=true exit=137", Supervisor: test.obs})
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if len(report.Findings) != 0 || report.SupervisorAvailable {
					t.Fatalf("log created a trusted observation: %+v", report)
				}
				return
			}
			if len(report.Findings) != 1 || report.Findings[0].Code != test.want || report.Findings[0].Evidence[0].Source != "supervisor" || report.Findings[0].Evidence[0].Line != 0 || !report.SupervisorAvailable {
				t.Fatalf("unexpected supervisor result: %+v", report)
			}
		})
	}
	var request Request
	if err := json.Unmarshal([]byte(`{"log":"plain","Supervisor":{"ExitCode":137,"ContainerOOMKilled":true}}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.Supervisor != nil {
		t.Fatal("JSON supplied a trusted supervisor observation")
	}
}

func TestHeapAndContainerOOMAreIndependentEvidence(t *testing.T) {
	yes := true
	report, err := Analyze(Request{Log: "java.lang.OutOfMemoryError: Java heap space", Supervisor: &SupervisorObservation{ContainerOOMKilled: &yes}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 2 || report.Findings[0].Code != "java_heap_pressure" || report.Findings[1].Code != "container_memory_kill" {
		t.Fatalf("independent evidence collapsed: %+v", report)
	}
}

func TestNoInputTextLeavesAnalyzer(t *testing.T) {
	secrets := []string{"PRIVATE_TOKEN_7281", "198.51.100.42", "tenant-secret.example", "/hidden/operator/path", "PlayerSecretName"}
	log := "java.lang.NoClassDefFoundError: " + strings.Join(secrets, " ") + "\x1b[31m\n"
	report, err := Analyze(Request{Log: log})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("report leaked input text %q", secret)
		}
	}
	if strings.Contains(string(encoded), "\\u001b") {
		t.Fatal("report leaked a terminal escape")
	}
}

func TestTerminalControlsAndUnicode(t *testing.T) {
	log := "\x1b]0;fake title java.lang.OutOfMemoryError: Java heap space\a" +
		"\x1b[31mjava.lang.UnsupportedClassVersionError\x1b[0m: 私的クラス\r\n"
	report, err := Analyze(Request{Log: log})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Code != "java_runtime_version" {
		t.Fatalf("terminal title was treated as log text: %+v", report)
	}
	for _, log := range []string{"\x1b]unterminated", "\x1b[", "\x1b", "\x00\u202e\u200b", "普通のログ", "\x1b]hidden\x1b\\ordinary"} {
		if _, err := Analyze(Request{Log: log}); err != nil {
			t.Fatalf("valid bounded text rejected: %v", err)
		}
	}
}

func TestLimitsAndInvalidInput(t *testing.T) {
	badExit := 256
	tests := []struct {
		name string
		req  Request
		want error
	}{
		{"bytes", Request{Log: strings.Repeat("x", MaxLogBytes+1)}, ErrLogTooLarge},
		{"lines", Request{Log: strings.Repeat("\n", MaxLogLines+1)}, ErrTooManyLines},
		{"line_bytes", Request{Log: strings.Repeat("x", MaxLineBytes+1)}, ErrLineTooLong},
		{"utf8", Request{Log: "bad\xfftext"}, ErrInvalidUTF8},
		{"exit_code", Request{Supervisor: &SupervisorObservation{ExitCode: &badExit}}, ErrInvalidSupervisor},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report, err := Analyze(test.req)
			if !errors.Is(err, test.want) || !reflect.DeepEqual(report, Report{}) {
				t.Fatalf("got report=%+v err=%v", report, err)
			}
		})
	}
	for _, log := range []string{"", strings.Repeat("\n", MaxLogLines), strings.Repeat("x", MaxLineBytes), strings.Repeat(strings.Repeat("x", MaxLineBytes)+"\n", 15) + strings.Repeat("x", MaxLineBytes-15)} {
		if _, err := Analyze(Request{Log: log}); err != nil {
			t.Fatalf("boundary rejected (bytes=%d): %v", len(log), err)
		}
	}
}

func TestEvidenceBoundedAndDeterministic(t *testing.T) {
	request := Request{Log: strings.Repeat("java.lang.NoClassDefFoundError: private.Type\n", 10)}
	first, err := Analyze(request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Analyze(request)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("not deterministic: %v", err)
	}
	if len(first.Findings) != 1 || len(first.Findings[0].Evidence) != MaxEvidence || !first.Findings[0].EvidenceCapped {
		t.Fatalf("evidence was not capped: %+v", first)
	}
	for i, evidence := range first.Findings[0].Evidence {
		if evidence.Line != i+1 {
			t.Fatalf("bad line evidence: %+v", evidence)
		}
	}
	first.Findings[0].NextChecks[0] = "mutated caller value"
	third, err := Analyze(request)
	if err != nil || !reflect.DeepEqual(second, third) {
		t.Fatal("caller mutated static rule data")
	}
}

func TestUnknownLogIsNotHealthyClaim(t *testing.T) {
	report, err := Analyze(Request{Log: "Server terminated unexpectedly"})
	if err != nil || len(report.Findings) != 0 || report.Findings == nil || len(report.Notes) == 0 {
		t.Fatalf("bad unknown report: %+v %v", report, err)
	}
}

func TestConcurrentAnalysis(t *testing.T) {
	request := Request{Log: "java.lang.OutOfMemoryError: Java heap space\nNo space left on device"}
	want, err := Analyze(request)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			got, err := Analyze(request)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("concurrent result differs: %v", err)
			}
		}()
	}
	group.Wait()
}

func FuzzAnalyzeBounded(f *testing.F) {
	for _, seed := range []string{"", "bad\xfftext", "\x1b[31mjava.lang.OutOfMemoryError: Java heap space\x1b[0m", "\x1b]title\x1b\\line", "\x00\u202e\n", "No space left on device"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, log string) {
		report, err := Analyze(Request{Log: log})
		if err != nil {
			if !reflect.DeepEqual(report, Report{}) {
				t.Fatal("partial result on rejected input")
			}
			return
		}
		if len(report.Findings) > MaxFindings || report.InputBytes > MaxLogBytes || report.InputLines > MaxLogLines {
			t.Fatal("limit exceeded")
		}
		for _, finding := range report.Findings {
			if len(finding.Evidence) > MaxEvidence {
				t.Fatal("evidence limit exceeded")
			}
			for _, evidence := range finding.Evidence {
				if evidence.Source != "log" || evidence.Line < 1 || evidence.Line > report.InputLines {
					t.Fatal("invalid evidence")
				}
			}
		}
		again, err := Analyze(Request{Log: log})
		if err != nil || !reflect.DeepEqual(report, again) {
			t.Fatal("not deterministic")
		}
	})
}
