//go:build linux

package benchgate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func ptr[T any](v T) *T      { return &v }
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func testNow() time.Time     { return time.Date(2026, 10, 19, 6, 0, 0, 0, time.UTC) }

// Entirely synthetic test material. The measured marker only exercises that
// branch of the importer; these are not measurements of a Minecraft server.
func fixture(t *testing.T) (Report, string) {
	t.Helper()
	root := t.TempDir()
	h := digest("synthetic-controlled-dimensions")
	m := Match{h, h, h, h, h, h, h, h, h, 20, []ClientCohort{{"java", "26.3", "tcp", "keyboard-mouse", 10}, {"bedrock", "26.52", "nethernet", "touch", 10}}}
	report := Report{SchemaVersion: SchemaVersion, EvidenceKind: "measured", DeclaredChanges: []string{"engineArtifact"}}
	for index, run := range []*Run{&report.Baseline, &report.Candidate} {
		run.ID = []string{"synthetic-baseline", "synthetic-candidate"}[index]
		run.Engine = Engine{run.ID, "test-only", digest(run.ID)}
		run.Match = m
		run.Match.Clients = append([]ClientCohort(nil), m.Clients...)
		for i := 0; i < 3; i++ {
			rep := Repetition{ID: fmt.Sprintf("%s-r%d", run.ID, i), PairID: fmt.Sprintf("pair-%d", i), StartedAt: testNow().Add(-24*time.Hour + time.Duration(index*3600+i*900)*time.Second).Format(time.RFC3339), WarmupSeconds: 120, DurationSeconds: 600, TickSamples: 12000, InteractionSamples: 1200,
				Metrics: Metrics{ptr(30.0 - float64(index)), ptr(80.0 - float64(index)), ptr(0.001)}, Correctness: Correctness{ptr(true), ptr(true), ptr(true), ptr(true), ptr(true)}}
			run.Repetitions = append(run.Repetitions, rep)
		}
	}
	writeFixtureTraces(t, &report, root)
	return report, root
}

func writeFixtureTraces(t *testing.T, report *Report, root string) {
	t.Helper()
	for _, run := range []*Run{&report.Baseline, &report.Candidate} {
		for index := range run.Repetitions {
			rep := &run.Repetitions[index]
			window := uint64(rep.DurationSeconds) * 1_000_000_000
			for j, a := range []*Artifact{&rep.Traces.Ticks, &rep.Traces.Interactions, &rep.Traces.Correctness} {
				kind := []string{"ticks", "interactions", "correctness"}[j]
				path := rep.ID + "-" + kind + ".jsonl"
				count := []uint64{rep.TickSamples, rep.InteractionSamples, 5}[j]
				var data string
				if kind == "correctness" {
					data = correctnessTrace(run.ID, rep.ID, window, rep.Correctness)
				} else {
					p99 := *rep.Metrics.P99TickMS
					if kind == "interactions" {
						p99 = *rep.Metrics.P99InteractionMS
					}
					value := uint64(math.Round(p99 * 1_000_000))
					durations := make([]uint64, count)
					for i := range durations {
						durations[i] = value
					}
					if kind == "ticks" {
						misses := uint64(math.Round(*rep.Metrics.DeadlineMissFraction * float64(count)))
						low, high := value, uint64(60_000_000)
						if value > TickDeadlineNS {
							low, high = 30_000_000, value
						}
						for i := uint64(0); i < count; i++ {
							durations[i] = low
							if i >= count-misses {
								durations[i] = high
							}
						}
					}
					data = timingTrace(kind, run.ID, rep.ID, window, durations)
				}
				if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				*a = Artifact{path, digest(data), int64(len(data)), count}
			}
		}
	}
}

func TestMatchedEngineChangeWithScopedArtifacts(t *testing.T) {
	report, root := fixture(t)
	result := Screen(report, root, testNow())
	if result.Status != "passed_screening" || result.EvidenceMode != "trace_metrics_recomputed" || result.ProductionReady {
		t.Fatalf("unexpected screening: %+v", result)
	}
	if len(result.Comparisons) != 3 || !result.Comparisons[0].TickImproved || result.Comparisons[0].TickDeltaMS != -1 {
		t.Fatalf("missing descriptive comparison: %+v", result.Comparisons)
	}
	if len(result.TraceSummaries) != 18 || *result.TraceSummaries[0].P99NS != 30_000_000 || *result.TraceSummaries[0].DeadlineMissCount != 12 {
		t.Fatalf("missing recomputed trace evidence: %+v", result.TraceSummaries)
	}
	if len(result.Limitations) < 4 {
		t.Fatal("lost distinction between trace identity and verified measurement")
	}
}

func TestAllControlledHashesMustMatch(t *testing.T) {
	typeOf := reflect.TypeOf(Match{})
	for i := 0; i < typeOf.NumField(); i++ {
		if !strings.HasSuffix(typeOf.Field(i).Name, "SHA256") {
			continue
		}
		t.Run(typeOf.Field(i).Name, func(t *testing.T) {
			report, root := fixture(t)
			reflect.ValueOf(&report.Candidate.Match).Elem().Field(i).SetString(digest("changed"))
			result := Screen(report, root, testNow())
			if result.Status != "insufficient_evidence" {
				t.Fatalf("mismatched controlled dimension passed: %+v", result)
			}
		})
	}
}

func TestIncompleteOrMismatchedEvidenceFailsClosed(t *testing.T) {
	cases := map[string]func(*Report){
		"synthetic":             func(r *Report) { r.EvidenceKind = "synthetic" },
		"unknown evidence kind": func(r *Report) { r.EvidenceKind = "unknown" },
		"population":            func(r *Report) { r.Candidate.Match.ActivePlayers++ },
		"client version":        func(r *Report) { r.Candidate.Match.Clients[0].Version = "different" },
		"client distribution":   func(r *Report) { r.Candidate.Match.Clients[0].Players++; r.Candidate.Match.Clients[1].Players-- },
		"client transport":      func(r *Report) { r.Candidate.Match.Clients[1].Transport = "raknet" },
		"duration":              func(r *Report) { r.Candidate.Repetitions[0].DurationSeconds = 601 },
		"warmup":                func(r *Report) { r.Candidate.Repetitions[0].WarmupSeconds = 121 },
		"too short":             func(r *Report) { r.Candidate.Repetitions[0].DurationSeconds = 1 },
		"missing metric":        func(r *Report) { r.Candidate.Repetitions[0].Metrics.P99TickMS = nil },
		"negative":              func(r *Report) { r.Candidate.Repetitions[0].Metrics.P99InteractionMS = ptr(-1.0) },
		"nan API":               func(r *Report) { r.Candidate.Repetitions[0].Metrics.P99TickMS = ptr(math.NaN()) },
		"infinite API":          func(r *Report) { r.Candidate.Repetitions[0].Metrics.P99TickMS = ptr(math.Inf(1)) },
		"missing correctness":   func(r *Report) { r.Candidate.Repetitions[0].Correctness.InventoryConserved = nil },
		"missing trace":         func(r *Report) { r.Candidate.Repetitions[0].Traces.Ticks = Artifact{} },
		"sample mismatch":       func(r *Report) { r.Candidate.Repetitions[0].TickSamples++ },
		"low samples": func(r *Report) {
			r.Candidate.Repetitions[0].InteractionSamples = 1
			r.Candidate.Repetitions[0].Traces.Interactions.SampleCount = 1
		},
		"too few repeats":  func(r *Report) { r.Candidate.Repetitions = r.Candidate.Repetitions[:2] },
		"duplicate repeat": func(r *Report) { r.Candidate.Repetitions[1].ID = r.Candidate.Repetitions[0].ID },
		"duplicate pair":   func(r *Report) { r.Candidate.Repetitions[1].PairID = r.Candidate.Repetitions[0].PairID },
		"reused window":    func(r *Report) { r.Candidate.Repetitions[1].StartedAt = r.Candidate.Repetitions[0].StartedAt },
		"future start":     func(r *Report) { r.Candidate.Repetitions[0].StartedAt = testNow().Add(time.Hour).Format(time.RFC3339) },
		"future completion": func(r *Report) {
			r.Candidate.Repetitions[0].StartedAt = testNow().Add(-time.Minute).Format(time.RFC3339)
		},
		"no actual engine change": func(r *Report) { r.Candidate.Engine.SHA256 = r.Baseline.Engine.SHA256 },
		"undeclared change":       func(r *Report) { r.DeclaredChanges = nil },
		"extra declared change":   func(r *Report) { r.DeclaredChanges = append(r.DeclaredChanges, "world") },
		"traversal":               func(r *Report) { r.Candidate.Repetitions[0].Traces.Ticks.Path = "../secret" },
		"absolute path":           func(r *Report) { r.Candidate.Repetitions[0].Traces.Ticks.Path = "/tmp/trace" },
		"URL path":                func(r *Report) { r.Candidate.Repetitions[0].Traces.Ticks.Path = "https://example.org/trace" },
		"oversize file":           func(r *Report) { r.Candidate.Repetitions[0].Traces.Ticks.SizeBytes = MaxTraceBytes + 1 },
		"oversize total": func(r *Report) {
			for _, run := range []*Run{&r.Baseline, &r.Candidate} {
				for i := range run.Repetitions {
					run.Repetitions[i].Traces.Ticks.SizeBytes = MaxTraceBytes
					run.Repetitions[i].Traces.Interactions.SizeBytes = MaxTraceBytes
					run.Repetitions[i].Traces.Correctness.SizeBytes = MaxTraceBytes
				}
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			report, root := fixture(t)
			change(&report)
			result := Screen(report, root, testNow())
			if result.Status != "insufficient_evidence" || result.EvidenceMode != "summary_screening_only" || result.ProductionReady {
				t.Fatalf("unsafe acceptance: %+v", result)
			}
		})
	}
}

func TestAdverseOutcomesFailWithoutCallingEverythingFaster(t *testing.T) {
	cases := map[string]func(*Report){
		"tick regression":        func(r *Report) { r.Candidate.Repetitions[0].Metrics.P99TickMS = ptr(32.0) },
		"interaction regression": func(r *Report) { r.Candidate.Repetitions[0].Metrics.P99InteractionMS = ptr(90.0) },
		"tick ceiling": func(r *Report) {
			r.Baseline.Repetitions[0].Metrics.P99TickMS = ptr(60.0)
			r.Baseline.Repetitions[0].Metrics.DeadlineMissFraction = ptr(1.0)
			r.Candidate.Repetitions[0].Metrics.P99TickMS = ptr(55.0)
			r.Candidate.Repetitions[0].Metrics.DeadlineMissFraction = ptr(1.0)
		},
		"deadline misses": func(r *Report) {
			r.Candidate.Repetitions[0].Metrics.DeadlineMissFraction = ptr(0.02)
			r.Candidate.Repetitions[0].Metrics.P99TickMS = ptr(60.0)
		},
		"candidate correctness":     func(r *Report) { r.Candidate.Repetitions[0].Correctness.EconomyConserved = ptr(false) },
		"invalid baseline behavior": func(r *Report) { r.Baseline.Repetitions[0].Correctness.TransfersPassed = ptr(false) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			report, root := fixture(t)
			change(&report)
			writeFixtureTraces(t, &report, root)
			result := Screen(report, root, testNow())
			if result.Status != "failed_screening" || result.ProductionReady {
				t.Fatalf("adverse result passed: %+v", result)
			}
		})
	}
	// A small regression inside tolerance passes screening but is explicitly
	// reported as a regression, never as an improvement or a faster engine.
	report, root := fixture(t)
	for i := range report.Candidate.Repetitions {
		report.Candidate.Repetitions[i].Metrics.P99TickMS = ptr(30.1)
		report.Candidate.Repetitions[i].Metrics.P99InteractionMS = ptr(80.1)
	}
	writeFixtureTraces(t, &report, root)
	result := Screen(report, root, testNow())
	if result.Status != "passed_screening" || result.Comparisons[0].TickImproved || result.Comparisons[0].InteractionImproved || result.Comparisons[0].TickDeltaMS <= 0 {
		t.Fatalf("tolerance was misrepresented as improvement: %+v", result)
	}
}

func TestArtifactIdentityAndScope(t *testing.T) {
	for _, kind := range []string{"tamper", "size", "missing", "symlink-file", "symlink-directory", "directory", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			report, root := fixture(t)
			a := &report.Candidate.Repetitions[0].Traces.Ticks
			full := filepath.Join(root, a.Path)
			switch kind {
			case "tamper":
				if err := os.WriteFile(full, []byte(strings.Repeat("x", int(a.SizeBytes))), 0600); err != nil {
					t.Fatal(err)
				}
			case "size":
				a.SizeBytes++
			case "missing":
				if err := os.Remove(full); err != nil {
					t.Fatal(err)
				}
			case "symlink-file":
				outside := filepath.Join(t.TempDir(), "outside")
				data, err := os.ReadFile(full)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(outside, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(full); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(outside, full); err != nil {
					t.Fatal(err)
				}
			case "symlink-directory":
				if err := os.Symlink(root, filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
				a.Path = "alias/" + a.Path
			case "directory":
				if err := os.Remove(full); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(full, 0700); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := os.Remove(full); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(full, 0600); err != nil {
					t.Fatal(err)
				}
			}
			result := Screen(report, root, testNow())
			if result.Status != "insufficient_evidence" || result.EvidenceMode != "summary_screening_only" {
				t.Fatalf("unverified trace passed: %+v", result)
			}
		})
	}
	report, _ := fixture(t)
	result := Screen(report, "", testNow())
	if result.Status != "insufficient_evidence" {
		t.Fatal("missing explicit artifact root passed")
	}
}
