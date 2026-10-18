package resources

import (
	"math"
	"testing"
)

func goodHost() Host {
	return Host{ID: "test-node", Online: true, CPUThreads: 16, CPUPercent: 20, Load1: 3, CPUPressureSome10: 0, MemPressureSome10: 0, MemoryTotal: 64 * GiB, MemoryUsed: 20 * GiB, DiskTotal: 256 * GiB, DiskUsed: 45 * GiB, SwapTotal: 4 * GiB, SwapUsed: 0}
}
func TestProfileIDsAndBudgets(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Profiles() {
		if p.ID == "" || seen[p.ID] || p.CPUMilli == 0 || p.MemoryBytes == 0 || p.DiskBytes == 0 {
			t.Fatalf("invalid profile: %+v", p)
		}
		seen[p.ID] = true
		if q, ok := FindProfile(p.ID); !ok || q.ID != p.ID {
			t.Fatalf("missing profile %s", p.ID)
		}
	}
	if _, ok := FindProfile("arbitrary-script"); ok {
		t.Fatal("allowed arbitrary profile")
	}
}
func TestPreviewDoesNotStartAnything(t *testing.T) {
	p, _ := FindProfile("mc-lobby")
	r := Compute([]Host{goodHost()}, p)
	if r.Stage != "read-only-preview" || r.RecommendedNode != "test-node" || !r.Candidates[0].Admitted {
		t.Fatalf("unexpected %+v", r)
	}
}
func TestNoPlacementWhenTelemetryMissingOrOffline(t *testing.T) {
	p, _ := FindProfile("mc-lobby")
	r := Compute([]Host{{ID: "new-node", Online: true}, {ID: "offline", CPUThreads: 16, MemoryTotal: 64 * GiB, DiskTotal: 256 * GiB}}, p)
	if r.RecommendedNode != "" {
		t.Fatalf("fail-open preview: %+v", r)
	}
	for _, c := range r.Candidates {
		if c.Admitted || len(c.Reasons) == 0 {
			t.Fatalf("unsafe admission: %+v", c)
		}
	}
}
func TestBusyHostIsRejected(t *testing.T) {
	h := goodHost()
	h.CPUPercent = 90
	p, _ := FindProfile("mc-lobby")
	r := Compute([]Host{h}, p)
	if r.Candidates[0].Admitted {
		t.Fatal("accepted saturated host")
	}
}
func TestDiskSafetyBlocksPlacement(t *testing.T) {
	h := goodHost()
	h.DiskUsed = h.DiskTotal - 16*GiB
	p, _ := FindProfile("mc-lobby")
	r := Compute([]Host{h}, p)
	if r.Candidates[0].Admitted {
		t.Fatal("accepted nearly full storage")
	}
}
func TestBatchLearningMoreConservative(t *testing.T) {
	h := goodHost()
	h.CPUPercent = 50
	p, _ := FindProfile("star-learning")
	r := Compute([]Host{h}, p)
	if r.Candidates[0].Admitted {
		t.Fatal("accepted learning during significant CPU load")
	}
}
func TestPressureAndNaNAreRejected(t *testing.T) {
	h := goodHost()
	h.CPUPressureSome10 = 30
	p, _ := FindProfile("mc-lobby")
	r := Compute([]Host{h}, p)
	if r.Candidates[0].Admitted {
		t.Fatal("accepted high CPU pressure")
	}
	h.CPUPressureSome10 = math.NaN()
	r = Compute([]Host{h}, p)
	if r.Candidates[0].Admitted {
		t.Fatal("accepted NaN metric")
	}
}
func TestDeterministicBestNode(t *testing.T) {
	a := goodHost()
	a.ID = "z"
	b := goodHost()
	b.ID = "a"
	b.MemoryUsed = 10 * GiB
	p, _ := FindProfile("mc-lobby")
	r := Compute([]Host{a, b}, p)
	if r.RecommendedNode != "a" || r.Candidates[0].NodeID != "a" {
		t.Fatalf("unexpected %+v", r)
	}
}
