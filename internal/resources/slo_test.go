package resources

import (
	"math"
	"testing"
)

func TestAdviseBatchMSPT(t *testing.T) {
	slo := SLO{TargetP99MSPT: 50}
	cases := []struct {
		name    string
		p99     float64
		running bool
		want    Action
	}{
		{"idle healthy start", 20, false, ActionContinue},
		{"running healthy", 20, true, ActionContinue},
		{"approaching, not started", 42, false, ActionDefer},
		{"approaching, running", 42, true, ActionHold},
		{"breached, not started", 55, false, ActionDefer},
		{"breached, running", 50, true, ActionCheckpoint},
		{"NaN fails closed, running", math.NaN(), true, ActionCheckpoint},
		{"negative fails closed, idle", -1, false, ActionDefer},
	}
	for _, c := range cases {
		a := AdviseBatch(c.p99, c.running, slo)
		if a.Action != c.want || !a.Advisory || a.Reason == "" {
			t.Errorf("%s: got %+v want %s", c.name, a, c.want)
		}
	}
}

func TestAdviseBatchInvalidSLOFailsClosed(t *testing.T) {
	for _, s := range []SLO{{}, {TargetP99MSPT: -5}, {TargetP99MSPT: math.Inf(1)}} {
		if a := AdviseBatch(1, false, s); a.Action != ActionDefer {
			t.Errorf("slo %+v: %+v", s, a)
		}
	}
}
