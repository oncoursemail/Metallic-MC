package resources

import "math"

// Action is an advisory recommendation for a Star AI batch job. Nothing in
// this package applies it: actual control (cgroup changes, checkpointing,
// deferral) requires explicit owner authorization and a tested rollback.
type Action string

const (
	ActionContinue   Action = "continue"
	ActionHold       Action = "hold"
	ActionDefer      Action = "defer"
	ActionCheckpoint Action = "checkpoint"
)

// approachFraction is the share of the SLO at which batch work stops growing.
const approachFraction = 0.8

// SLO is the Minecraft p99 MSPT (milliseconds per tick) target to protect.
type SLO struct {
	TargetP99MSPT float64 `json:"targetP99Mspt"`
}

type Advice struct {
	Action   Action `json:"action"`
	Reason   string `json:"reason"`
	Advisory bool   `json:"advisory"`
}

// AdviseBatch maps measured Minecraft p99 MSPT to an advisory action for a
// batch job. Invalid input fails closed (defer, or checkpoint if running).
func AdviseBatch(p99MSPT float64, running bool, slo SLO) Advice {
	stop := func(reason string) Advice {
		if running {
			return Advice{ActionCheckpoint, reason, true}
		}
		return Advice{ActionDefer, reason, true}
	}
	if !validMS(slo.TargetP99MSPT) || slo.TargetP99MSPT == 0 {
		return Advice{ActionDefer, "Invalid MSPT SLO; defer batch work", true}
	}
	if !validMS(p99MSPT) {
		return stop("MSPT telemetry invalid; failing closed")
	}
	switch {
	case p99MSPT >= slo.TargetP99MSPT:
		return stop("p99 MSPT has reached the SLO")
	case p99MSPT >= slo.TargetP99MSPT*approachFraction:
		if running {
			return Advice{ActionHold, "p99 MSPT approaching the SLO; do not grow batch work", true}
		}
		return Advice{ActionDefer, "p99 MSPT approaching the SLO; do not start batch work", true}
	}
	return Advice{ActionContinue, "p99 MSPT well within the SLO", true}
}

func validMS(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
