package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/edgeaudit"
)

var commandTestNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func TestIncompleteExampleProducesBlockedReportAndNonzeroExit(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"-config", "../../examples/edge-audit/incomplete.json"}, &out, &errOut, commandTestNow)
	if code != 1 || errOut.Len() != 0 {
		t.Fatalf("exit=%d, stderr=%s", code, errOut.String())
	}
	var report edgeaudit.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "blocked" || report.ProtectionVerified || report.LiveNetworkTested || len(report.ConfigurationSHA256) != 64 {
		t.Fatalf("incomplete example misrepresented: %+v", report)
	}
}

func TestCLIRejectsInvalidInputWithoutSuccessReport(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"duplicate": `{"schemaVersion":1,"schemaVersion":1}`,
		"unknown":   `{"protected":true}`,
		"trailing":  `{} {}`,
		"oversized": "{}" + strings.Repeat(" ", edgeaudit.MaxConfigBytes),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			if code := run([]string{"-config", path}, &out, &errOut, commandTestNow); code != 2 || out.Len() != 0 || errOut.Len() == 0 {
				t.Fatalf("exit=%d, stdout=%s, stderr=%s", code, out.String(), errOut.String())
			}
		})
	}
	for _, args := range [][]string{nil, {"-unknown"}, {"-config", filepath.Join(dir, "missing.json")}, {"-config", dir}, {"-config", "../../examples/edge-audit/incomplete.json", "unexpected"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut, commandTestNow); code != 2 || out.Len() != 0 {
			t.Fatalf("args=%v, exit=%d, stdout=%s", args, code, out.String())
		}
	}
}

func TestCLIHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-help"}, &out, &errOut, commandTestNow); code != 0 || !strings.Contains(errOut.String(), "evidence-dir") {
		t.Fatalf("exit=%d, stderr=%s", code, errOut.String())
	}
}

func TestCLIQuotesUntrustedDiagnosticKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-characters.json")
	input := []byte(`{"\u001b[31mAUDIT_OK\u001b[0m\r\nFORGED":true}`)
	if err := os.WriteFile(path, input, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"-config", path}, &out, &errOut, commandTestNow); code != 2 || out.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q", code, out.String())
	}
	diagnostic := errOut.String()
	if strings.ContainsAny(diagnostic, "\x1b\r") || strings.Count(diagnostic, "\n") != 1 {
		t.Fatalf("untrusted key introduced terminal or log control characters: %q", diagnostic)
	}
	if !strings.Contains(diagnostic, `\x1b`) || !strings.Contains(diagnostic, `\r\n`) {
		t.Fatalf("expected escaped diagnostic key: %q", diagnostic)
	}
}

func TestCLIRejectsSymlinkConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("platform did not permit creating a symlink: %v", err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"-config", link}, &out, &errOut, commandTestNow); code != 2 {
		t.Fatalf("symlink configuration returned %d", code)
	}
}
