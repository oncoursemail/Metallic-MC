//go:build linux

package benchgate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rewriteTrace(t *testing.T, root string, artifact *Artifact, change func(string) string) {
	t.Helper()
	path := filepath.Join(root, artifact.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := change(string(data))
	if changed == string(data) {
		t.Fatal("test mutation did not change trace")
	}
	if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	// Deliberately make the hash/size truthful: these cases must fail on trace
	// interpretation or manifest disagreement, not merely a bad file checksum.
	artifact.SHA256, artifact.SizeBytes = digest(changed), int64(len(changed))
}

func TestScreenRejectsLyingSummariesAndValidlyHashedBadTraces(t *testing.T) {
	cases := map[string]func(*testing.T, *Report, string){
		"declared tick p99": func(t *testing.T, r *Report, root string) {
			r.Candidate.Repetitions[0].Metrics.P99TickMS = ptr(1.0)
		},
		"declared interaction p99": func(t *testing.T, r *Report, root string) {
			r.Candidate.Repetitions[0].Metrics.P99InteractionMS = ptr(1.0)
		},
		"declared deadline fraction": func(t *testing.T, r *Report, root string) {
			r.Candidate.Repetitions[0].Metrics.DeadlineMissFraction = ptr(0.0)
		},
		"declared count agrees only with manifest": func(t *testing.T, r *Report, root string) {
			r.Candidate.Repetitions[0].TickSamples++
			r.Candidate.Repetitions[0].Traces.Ticks.SampleCount++
		},
		"raw correctness contradicts passing summary": func(t *testing.T, r *Report, root string) {
			rewriteTrace(t, root, &r.Candidate.Repetitions[0].Traces.Correctness, func(s string) string {
				return strings.Replace(s, `"passed":true`, `"passed":false`, 1)
			})
		},
		"replayed trace identity": func(t *testing.T, r *Report, root string) {
			rewriteTrace(t, root, &r.Candidate.Repetitions[0].Traces.Ticks, func(s string) string {
				return strings.Replace(s, `"runId":"synthetic-candidate"`, `"runId":"synthetic-baseline"`, 1)
			})
		},
		"synthetic trace with measured manifest": func(t *testing.T, r *Report, root string) {
			rewriteTrace(t, root, &r.Candidate.Repetitions[0].Traces.Ticks, func(s string) string {
				return strings.Replace(s, `"evidenceKind":"measured"`, `"evidenceKind":"synthetic"`, 1)
			})
		},
		"truncated trace with updated hash": func(t *testing.T, r *Report, root string) {
			rewriteTrace(t, root, &r.Candidate.Repetitions[0].Traces.Ticks, func(s string) string {
				return s[:strings.LastIndex(s, `{"type":"complete"`)]
			})
		},
		"legacy summary-only schema": func(t *testing.T, r *Report, root string) {
			r.SchemaVersion = "metallic.benchmark-gate/v1"
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			report, root := fixture(t)
			change(t, &report, root)
			result := Screen(report, root, testNow())
			if result.Status != "insufficient_evidence" || result.ProductionReady || len(result.TraceSummaries) != 0 || len(result.Comparisons) != 0 {
				t.Fatalf("untrusted summary/partial trace accepted: %+v", result)
			}
		})
	}
}

func TestScreenComparesRecomputedValuesWithoutMutatingCaller(t *testing.T) {
	report, root := fixture(t)
	// Sub-nanosecond summary rounding is tolerated, but must never change the
	// comparison away from the integer nanosecond values in the actual trace.
	report.Baseline.Repetitions[0].Metrics.P99TickMS = ptr(30.0000004)
	before, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	result := Screen(report, root, testNow())
	if result.Status != "passed_screening" || result.Comparisons[0].BaselineP99TickMS != 30 || result.Comparisons[0].TickDeltaMS != -1 {
		t.Fatalf("comparison used declared rather than recomputed metric: %+v", result)
	}
	after, err := json.Marshal(report)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("Screen mutated caller evidence: %v", err)
	}
}
