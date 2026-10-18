// Package resources provides conservative, read-only resource admission previews.
// Admission is NOT resource enforcement or permission to start a process. The
// production runtime will need cgroup allocations, hard quotas, durable leases,
// and a reconciler that owns the container lifecycle.
package resources

import (
	"math"
	"sort"
)

const GiB uint64 = 1024 * 1024 * 1024

type Profile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Project     string `json:"project"`
	Class       string `json:"class"`
	CPUMilli    uint64 `json:"cpuMilli"`
	MemoryBytes uint64 `json:"memoryBytes"`
	DiskBytes   uint64 `json:"diskBytes"`
	Preemptible bool   `json:"preemptible"`
	Description string `json:"description"`
}

// Workload profiles are ONLY what-if templates, never proof of player capacity.
// The memory budget represents total container memory including JVM native overhead.
var profiles = []Profile{
	{ID: "mc-lobby", Name: "Minecraft lobby", Project: "Metallic MC", Class: "realtime", CPUMilli: 2000, MemoryBytes: 4 * GiB, DiskBytes: 8 * GiB, Description: "One Paper lobby; deliberately conservative example budget."},
	{ID: "mc-match", Name: "Minecraft match", Project: "Metallic MC", Class: "realtime", CPUMilli: 3000, MemoryBytes: 6 * GiB, DiskBytes: 10 * GiB, Description: "A single independently managed minigame instance."},
	{ID: "metallic-world", Name: "Metallic game world", Project: "Metallic", Class: "realtime", CPUMilli: 4000, MemoryBytes: 8 * GiB, DiskBytes: 12 * GiB, Description: "Example persistent social-world service, not an engine benchmark."},
	{ID: "star-inference", Name: "Star AI inference", Project: "Star AI", Class: "standard", CPUMilli: 3000, MemoryBytes: 8 * GiB, DiskBytes: 4 * GiB, Description: "Example model-serving slice; actual model memory varies considerably."},
	{ID: "star-learning", Name: "Star AI background learning", Project: "Star AI", Class: "batch", CPUMilli: 6000, MemoryBytes: 12 * GiB, DiskBytes: 12 * GiB, Preemptible: true, Description: "Deferrable practice job; only preview when host has spare resources."},
}

func Profiles() []Profile { return append([]Profile(nil), profiles...) }
func FindProfile(id string) (Profile, bool) {
	for _, p := range profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

type Host struct {
	ID                string  `json:"id"`
	Online            bool    `json:"online"`
	CPUThreads        uint32  `json:"cpuThreads"`
	CPUPercent        float64 `json:"cpuPercent"`
	Load1             float64 `json:"load1"`
	CPUPressureSome10 float64 `json:"cpuPressureSome10"`
	MemPressureSome10 float64 `json:"memPressureSome10"`
	MemoryTotal       uint64  `json:"memoryTotal"`
	MemoryUsed        uint64  `json:"memoryUsed"`
	DiskTotal         uint64  `json:"diskTotal"`
	DiskUsed          uint64  `json:"diskUsed"`
	SwapTotal         uint64  `json:"swapTotal"`
	SwapUsed          uint64  `json:"swapUsed"`
}

type Candidate struct {
	NodeID              string   `json:"nodeId"`
	Admitted            bool     `json:"admitted"`
	Reasons             []string `json:"reasons"`
	CPUHeadroomMilli    uint64   `json:"cpuHeadroomMilli"`
	MemoryHeadroomBytes uint64   `json:"memoryHeadroomBytes"`
	DiskHeadroomBytes   uint64   `json:"diskHeadroomBytes"`
	Score               float64  `json:"score"`
}

type Preview struct {
	Stage           string      `json:"stage"`
	Profile         Profile     `json:"profile"`
	Candidates      []Candidate `json:"candidates"`
	RecommendedNode string      `json:"recommendedNode,omitempty"`
	Explanation     string      `json:"explanation"`
}

// Compute is an advisory what-if estimate. It cannot account for unreported
// cgroup allocations, player tick hotspots, I/O contention, or transient peaks.
func Compute(nodes []Host, profile Profile) Preview {
	r := Preview{Stage: "read-only-preview", Profile: profile, Candidates: make([]Candidate, 0, len(nodes)), Explanation: "Estimates only. No server is started, no capacity is reserved, and no existing process is changed."}
	for _, n := range nodes {
		r.Candidates = append(r.Candidates, assess(n, profile))
	}
	sort.Slice(r.Candidates, func(i, j int) bool { return r.Candidates[i].NodeID < r.Candidates[j].NodeID })
	best := -1
	for i, c := range r.Candidates {
		if c.Admitted && (best == -1 || c.Score > r.Candidates[best].Score) {
			best = i
		}
	}
	if best != -1 {
		r.RecommendedNode = r.Candidates[best].NodeID
	}
	return r
}

func assess(n Host, p Profile) Candidate {
	r := Candidate{NodeID: n.ID, Reasons: []string{}}
	if !n.Online {
		r.Reasons = append(r.Reasons, "Node is offline or telemetry is stale")
	}
	if n.CPUThreads == 0 || n.CPUThreads > 512 || !validPercent(n.CPUPercent) || !validPercent(n.CPUPressureSome10) || !validPercent(n.MemPressureSome10) || math.IsNaN(n.Load1) || math.IsInf(n.Load1, 0) || n.Load1 < 0 {
		r.Reasons = append(r.Reasons, "Insufficient or invalid CPU telemetry")
	} else {
		maxLoadFraction := 0.85
		cpuCeiling := 75.0
		pressureCeiling := 15.0
		if p.Class == "batch" {
			maxLoadFraction = 0.45
			cpuCeiling = 40
			pressureCeiling = 3
		}
		if n.CPUPercent >= cpuCeiling {
			r.Reasons = append(r.Reasons, "CPU utilization exceeds safe admission threshold")
		}
		if n.Load1 >= float64(n.CPUThreads)*maxLoadFraction {
			r.Reasons = append(r.Reasons, "System load is too high for this workload class")
		}
		if n.CPUPressureSome10 > pressureCeiling {
			r.Reasons = append(r.Reasons, "CPU pressure is elevated")
		}
		if n.MemPressureSome10 > 2 {
			r.Reasons = append(r.Reasons, "Memory pressure is elevated")
		}
		// Reserve at least one complete logical core for OS and unscheduled work.
		spare := float64(n.CPUThreads) * (cpuCeiling - n.CPUPercent) * 10
		if spare > 1000 {
			r.CPUHeadroomMilli = uint64(spare - 1000)
		}
	}
	if n.MemoryTotal == 0 || n.MemoryUsed > n.MemoryTotal || n.DiskTotal == 0 || n.DiskUsed > n.DiskTotal || n.SwapUsed > n.SwapTotal {
		r.Reasons = append(r.Reasons, "Insufficient or invalid memory/disk telemetry")
	} else {
		// Never fill the host: at least 3 GiB or 10% of RAM, and at
		// least 16 GiB or 10% of the monitored filesystem remain idle.
		memReserve := max(3*GiB, n.MemoryTotal/10)
		diskReserve := max(16*GiB, n.DiskTotal/10)
		availableMemory := n.MemoryTotal - n.MemoryUsed
		availableDisk := n.DiskTotal - n.DiskUsed
		if availableMemory > memReserve {
			r.MemoryHeadroomBytes = availableMemory - memReserve
		}
		if availableDisk > diskReserve {
			r.DiskHeadroomBytes = availableDisk - diskReserve
		}
	}
	if r.CPUHeadroomMilli < p.CPUMilli {
		r.Reasons = append(r.Reasons, "Not enough measured CPU headroom")
	}
	if r.MemoryHeadroomBytes < p.MemoryBytes {
		r.Reasons = append(r.Reasons, "Not enough memory after host reserve")
	}
	if r.DiskHeadroomBytes < p.DiskBytes {
		r.Reasons = append(r.Reasons, "Not enough free monitored disk after reserve")
	}
	if len(r.Reasons) == 0 {
		r.Admitted = true
		// Prefer a candidate with capacity above the requested budget; no
		// server placement decisions can be made from this estimate alone.
		r.Score = float64(r.CPUHeadroomMilli-p.CPUMilli)/1000 + float64(r.MemoryHeadroomBytes-p.MemoryBytes)/float64(GiB)
	}
	return r
}

func validPercent(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 100 }
