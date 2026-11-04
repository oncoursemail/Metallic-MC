package proposal

import (
	"strings"
	"testing"
)

func good() Proposal {
	return Proposal{
		ID: "p1", Target: TargetPlugin, Author: "star-ai",
		ChangedPaths: []string{"plugins/lobby/src/Main.java"},
		Evidence:     []Evidence{{Metric: "tick_ms_p95", Baseline: 38, Observed: 31, Samples: 500}},
		Experiment:   Experiment{Environment: "staging", Isolated: true, Completed: true},
		Holdout:      Holdout{Evaluated: true, Passed: true, Seen: false},
		SLO:          SLOCheck{Checked: true, Within: true},
		Budget:       Budget{LimitMilliCPU: 2000, UsedMilliCPU: 1500},
		Credential:   Credential{Scopes: []string{"repo:propose"}},
	}
}

func deny(t *testing.T, p Proposal, want string) {
	t.Helper()
	d := Evaluate(p)
	if d.PROpenAllowed {
		t.Fatalf("expected denial containing %q", want)
	}
	for _, r := range d.Reasons {
		if strings.Contains(r, want) {
			return
		}
	}
	t.Fatalf("reasons %v lack %q", d.Reasons, want)
}

func TestGoodProposalMayOpenPROnlyAndNeedsHuman(t *testing.T) {
	d := Evaluate(good())
	if !d.PROpenAllowed || len(d.Reasons) != 0 {
		t.Fatalf("%+v", d)
	}
	if !d.HumanReviewRequired || d.MergeAllowed || d.DeployAllowed {
		t.Fatalf("agent must never merge or deploy: %+v", d)
	}
}

func TestTargetsSupported(t *testing.T) {
	for _, tg := range []Target{TargetPlugin, TargetInfrastructure, TargetDashboard} {
		p := good()
		p.Target = tg
		if !Evaluate(p).PROpenAllowed {
			t.Fatalf("target %s", tg)
		}
	}
	p := good()
	p.Target = "kernel"
	deny(t, p, "target")
}

func TestEvidenceRequired(t *testing.T) {
	p := good()
	p.Evidence = nil
	deny(t, p, "evidence")
	p = good()
	p.Evidence[0].Samples = 3
	deny(t, p, "samples")
	p = good()
	p.Evidence[0].Observed = 40
	deny(t, p, "improve")
}

func TestStagingIsolation(t *testing.T) {
	p := good()
	p.Experiment.Environment = "production"
	deny(t, p, "staging")
	p = good()
	p.Experiment.Isolated = false
	deny(t, p, "isolated")
	p = good()
	p.Experiment.Completed = false
	deny(t, p, "experiment")
}

func TestHoldout(t *testing.T) {
	p := good()
	p.Holdout.Evaluated = false
	deny(t, p, "holdout")
	p = good()
	p.Holdout.Passed = false
	deny(t, p, "holdout")
	p = good()
	p.Holdout.Seen = true
	deny(t, p, "holdout")
}

func TestSLOAndBudget(t *testing.T) {
	p := good()
	p.SLO.Within = false
	deny(t, p, "SLO")
	p = good()
	p.SLO.Checked = false
	deny(t, p, "SLO")
	p = good()
	p.Budget.UsedMilliCPU = 2001
	deny(t, p, "budget")
	p = good()
	p.Budget.LimitMilliCPU = 0
	deny(t, p, "budget")
}

func TestProtectedPathsCannotBeChanged(t *testing.T) {
	for _, path := range []string{
		"internal/proposal/gate.go",
		"internal/proposal/gate_test.go",
		".github/workflows/ci.yml",
		"internal/benchgate/x.go",
		"go.sum",
		"./internal/proposal/../proposal/gate.go",
		"tools/../.github/workflows/ci.yml",
	} {
		p := good()
		p.ChangedPaths = append(p.ChangedPaths, path)
		deny(t, p, "protected")
	}
	p := good()
	p.ChangedPaths = nil
	deny(t, p, "paths")
}

func TestCredentialsLeastPrivilege(t *testing.T) {
	for _, s := range []string{"repo:merge", "deploy:live", "admin", "*"} {
		p := good()
		p.Credential.Scopes = append(p.Credential.Scopes, s)
		deny(t, p, "scope")
	}
	p := good()
	p.Credential.Scopes = nil
	deny(t, p, "scope")
}

func TestLiveChangeNeedsConsentButNeverAgentDeploy(t *testing.T) {
	p := good()
	p.Live = LiveChange{Requested: true}
	deny(t, p, "consent")
	p.Live.Consent = Consent{Granted: true, By: "owner", ScopeID: "other"}
	deny(t, p, "consent")
	p.Live.Consent = Consent{Granted: true, By: "star-ai", ScopeID: "p1"}
	deny(t, p, "consent")
	p.Live.Consent = Consent{Granted: true, By: "owner", ScopeID: "p1"}
	d := Evaluate(p)
	if !d.PROpenAllowed || d.DeployAllowed || !d.HumanReviewRequired {
		t.Fatalf("%+v", d)
	}
}
