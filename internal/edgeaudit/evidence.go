package edgeaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const MaxEvidenceAge = 7 * 24 * time.Hour

var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}$`)
var safeArtifact = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// ArtifactReader supplies local bytes only. Audit will never fetch Reference.
type ArtifactReader interface {
	ReadArtifact(name string) ([]byte, error)
}

// DirectoryArtifacts reads simple file names from an explicitly chosen folder.
// It rejects symlink entries and non-regular files. This is not a sandbox for
// hostile concurrent filesystem mutation; run audits on an immutable snapshot.
type DirectoryArtifacts struct{ Directory string }

func (d DirectoryArtifacts) ReadArtifact(name string) ([]byte, error) {
	if !safeArtifact.MatchString(name) {
		return nil, fmt.Errorf("artifact must be a simple file name")
	}
	if d.Directory == "" {
		return nil, fmt.Errorf("evidence directory is not supplied")
	}
	path := filepath.Join(d.Directory, name)
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("artifact is unavailable")
	}
	if !before.Mode().IsRegular() || before.Size() > MaxArtifactBytes {
		return nil, fmt.Errorf("artifact must be a regular file of at most %d bytes", MaxArtifactBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("artifact cannot be opened")
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, fmt.Errorf("artifact changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxArtifactBytes+1))
	if err != nil || len(data) > MaxArtifactBytes {
		return nil, fmt.Errorf("artifact cannot be read within its size limit")
	}
	return data, nil
}

type EvidenceResult struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	DigestMatched bool   `json:"digestMatched"`
	Fresh         bool   `json:"fresh"`
	Provenance    string `json:"provenance"`
}

func (r *Report) inspectEvidence(entries []Evidence, now time.Time, reader ArtifactReader) map[string]Evidence {
	valid := make(map[string]Evidence)
	seen := make(map[string]bool)
	if len(entries) == 0 {
		r.add("unverified", "evidence_missing", "evidence", "Supply current provider, endpoint assignment, and exposure-review evidence.")
	}
	if len(entries) > MaxEvidence {
		r.add("error", "evidence_limit", "evidence", "Too many evidence entries.")
		return valid
	}
	for i, e := range entries {
		field := fmt.Sprintf("evidence[%d]", i)
		result := EvidenceResult{ID: e.ID, Status: "unverified", Provenance: "operator_supplied_not_independently_verified"}
		start := len(r.Findings)
		if !safeID.MatchString(e.ID) || seen[e.ID] {
			r.add("error", "evidence_id", field+".id", "Evidence IDs must be valid and unique.")
		}
		seen[e.ID] = true
		if e.Kind != "provider_coverage" && e.Kind != "edge_assignment" && e.Kind != "exposure_review" {
			r.add("error", "evidence_kind", field+".kind", "Unknown evidence kind.")
		}
		u, err := url.Parse(e.Reference)
		if err != nil || len(e.Reference) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(e.Reference, "\r\n\t") {
			r.add("unverified", "evidence_reference", field+".reference", "Supply an HTTPS provider or review reference; it will not be fetched.")
		}
		observed, obsErr := time.Parse(time.RFC3339, e.ObservedAt)
		expires, expErr := time.Parse(time.RFC3339, e.ExpiresAt)
		if obsErr != nil || expErr != nil {
			r.add("unverified", "evidence_time", field, "Evidence needs RFC3339 observedAt and expiresAt timestamps.")
		} else if observed.After(now) || !expires.After(observed) {
			r.add("error", "evidence_time_order", field, "Evidence cannot be future-dated or expire before observation.")
		} else if !expires.After(now) || now.Sub(observed) > MaxEvidenceAge {
			r.add("unverified", "evidence_stale", field, "Evidence expired or was observed more than seven days ago.")
		} else {
			result.Fresh = true
		}
		digest, digestErr := hex.DecodeString(e.SHA256)
		if digestErr != nil || len(digest) != sha256.Size || len(e.SHA256) != sha256.Size*2 {
			r.add("unverified", "evidence_digest", field+".sha256", "Supply a complete SHA-256 artifact digest.")
		}
		if !safeArtifact.MatchString(e.Artifact) {
			r.add("error", "evidence_artifact_name", field+".artifact", "Artifact must be a simple file name, with no directory or traversal component.")
		} else if reader == nil {
			r.add("unverified", "evidence_artifact_missing", field+".artifact", "No local evidence reader is available.")
		} else {
			data, readErr := reader.ReadArtifact(e.Artifact)
			if readErr != nil || len(data) == 0 || len(data) > MaxArtifactBytes {
				r.add("unverified", "evidence_artifact_unavailable", field+".artifact", "Evidence artifact is missing, empty, non-regular, or exceeds its size limit.")
			} else {
				actual := sha256.Sum256(data)
				if digestErr == nil && len(digest) == sha256.Size && len(e.SHA256) == sha256.Size*2 {
					if strings.EqualFold(e.SHA256, hex.EncodeToString(actual[:])) {
						result.DigestMatched = true
					} else {
						r.add("error", "evidence_digest_mismatch", field+".sha256", "Evidence bytes do not match the supplied digest.")
					}
				}
			}
		}
		if len(r.Findings) == start {
			result.Status = "integrity_checked_assertion"
			valid[e.ID] = e
		}
		r.Evidence = append(r.Evidence, result)
	}
	return valid
}
