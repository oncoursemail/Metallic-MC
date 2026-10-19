package edgeaudit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvidenceFreshnessAndIntegrityFailures(t *testing.T) {
	cases := []struct {
		name, status, code string
		change             func(*Config, memoryArtifacts)
	}{
		{"observed too long ago", "unverified", "evidence_stale", func(c *Config, _ memoryArtifacts) {
			c.Evidence[0].ObservedAt = testNow.Add(-MaxEvidenceAge - time.Second).Format(time.RFC3339)
		}},
		{"expires at audit time", "unverified", "evidence_stale", func(c *Config, _ memoryArtifacts) { c.Evidence[0].ExpiresAt = testNow.Format(time.RFC3339) }},
		{"future observation", "blocked", "evidence_time_order", func(c *Config, _ memoryArtifacts) {
			c.Evidence[0].ObservedAt = testNow.Add(time.Second).Format(time.RFC3339)
		}},
		{"expiry before observation", "blocked", "evidence_time_order", func(c *Config, _ memoryArtifacts) {
			c.Evidence[0].ExpiresAt = testNow.Add(-2 * time.Hour).Format(time.RFC3339)
		}},
		{"invalid timestamp", "unverified", "evidence_time", func(c *Config, _ memoryArtifacts) { c.Evidence[0].ObservedAt = "yesterday" }},
		{"missing digest", "unverified", "evidence_digest", func(c *Config, _ memoryArtifacts) { c.Evidence[0].SHA256 = "" }},
		{"invalid digest", "unverified", "evidence_digest", func(c *Config, _ memoryArtifacts) { c.Evidence[0].SHA256 = strings.Repeat("z", 64) }},
		{"mismatched digest", "blocked", "evidence_digest_mismatch", func(c *Config, _ memoryArtifacts) { c.Evidence[0].SHA256 = strings.Repeat("0", 64) }},
		{"bytes changed", "blocked", "evidence_digest_mismatch", func(c *Config, a memoryArtifacts) { a[c.Evidence[0].Artifact] = []byte("changed") }},
		{"missing artifact", "unverified", "evidence_artifact_unavailable", func(c *Config, a memoryArtifacts) { delete(a, c.Evidence[0].Artifact) }},
		{"empty artifact", "unverified", "evidence_artifact_unavailable", func(c *Config, a memoryArtifacts) { a[c.Evidence[0].Artifact] = []byte{} }},
		{"oversized artifact", "unverified", "evidence_artifact_unavailable", func(c *Config, a memoryArtifacts) { a[c.Evidence[0].Artifact] = make([]byte, MaxArtifactBytes+1) }},
		{"path traversal", "blocked", "evidence_artifact_name", func(c *Config, _ memoryArtifacts) { c.Evidence[0].Artifact = "../secret.txt" }},
		{"absolute path", "blocked", "evidence_artifact_name", func(c *Config, _ memoryArtifacts) { c.Evidence[0].Artifact = "/tmp/secret.txt" }},
		{"windows path", "blocked", "evidence_artifact_name", func(c *Config, _ memoryArtifacts) { c.Evidence[0].Artifact = `..\secret.txt` }},
		{"non-ascii separator", "blocked", "evidence_artifact_name", func(c *Config, _ memoryArtifacts) { c.Evidence[0].Artifact = "evidence／secret.txt" }},
		{"http reference", "unverified", "evidence_reference", func(c *Config, _ memoryArtifacts) { c.Evidence[0].Reference = "http://example.invalid" }},
		{"credential reference", "unverified", "evidence_reference", func(c *Config, _ memoryArtifacts) { c.Evidence[0].Reference = "https://user:secret@example.invalid" }},
		{"url injection", "unverified", "evidence_reference", func(c *Config, _ memoryArtifacts) { c.Evidence[0].Reference = "https://example.invalid/\nInjected" }},
		{"duplicate evidence id", "blocked", "evidence_id", func(c *Config, _ memoryArtifacts) { c.Evidence[1].ID = c.Evidence[0].ID }},
		{"bad evidence kind", "blocked", "evidence_kind", func(c *Config, _ memoryArtifacts) { c.Evidence[0].Kind = "ddos_protected" }},
		{"wrong binding kind", "unverified", "evidence_binding", func(c *Config, _ memoryArtifacts) { c.Edge.ProviderEvidenceID = c.Edge.AssignmentEvidenceID }},
		{"missing binding", "unverified", "evidence_binding", func(c *Config, _ memoryArtifacts) { c.Boundaries.ExposureEvidenceID = "missing" }},
		{"too many evidence entries", "blocked", "evidence_limit", func(c *Config, _ memoryArtifacts) { c.Evidence = make([]Evidence, MaxEvidence+1) }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c, artifacts := consistentFixture("nethernet")
			tt.change(&c, artifacts)
			requireReport(t, Audit(c, testNow, artifacts), tt.status, tt.code)
		})
	}
}

func TestFreshnessBoundaryAndDigestCase(t *testing.T) {
	c, artifacts := consistentFixture("nethernet")
	c.Evidence[0].ObservedAt = testNow.Add(-MaxEvidenceAge).Format(time.RFC3339)
	c.Evidence[0].SHA256 = strings.ToUpper(c.Evidence[0].SHA256)
	requireReport(t, Audit(c, testNow, artifacts), "configuration_consistent", "")
	requireReport(t, Audit(c, testNow, nil), "unverified", "evidence_artifact_missing")
}

func TestDirectoryArtifactsConfinementAndBounds(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("review.txt", []byte("redacted local assertion"))
	reader := DirectoryArtifacts{Directory: dir}
	got, err := reader.ReadArtifact("review.txt")
	if err != nil || string(got) != "redacted local assertion" {
		t.Fatalf("regular file was not read correctly: %q %v", got, err)
	}
	write("large.txt", make([]byte, MaxArtifactBytes+1))
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../review.txt", "/review.txt", "./review.txt", `..\review.txt`, "", "missing.txt", "large.txt", "folder"} {
		if _, err := reader.ReadArtifact(name); err == nil {
			t.Errorf("unsafe or unavailable artifact %q was accepted", name)
		}
	}
	if _, err := (DirectoryArtifacts{}).ReadArtifact("review.txt"); err == nil {
		t.Fatal("implicit evidence directory was accepted")
	}
	if err := os.Symlink(filepath.Join(dir, "review.txt"), filepath.Join(dir, "link.txt")); err == nil {
		if _, err := reader.ReadArtifact("link.txt"); err == nil {
			t.Fatal("symlink artifact was accepted")
		}
	} else {
		t.Logf("platform did not permit creating a symlink; remaining confinement checks ran: %v", err)
	}
}
