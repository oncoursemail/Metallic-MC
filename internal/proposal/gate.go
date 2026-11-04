// Package proposal is the pure, offline gate for Star AI engineering proposals.
// It decides only whether an agent may OPEN a pull request. It never merges,
// deploys or changes anything live: merges and every live change stay with a
// human, and live changes also need that person's explicit consent.
package proposal

import (
	"fmt"
	"path"
	"strings"
)

type Target string

const (
	TargetPlugin         Target = "minecraft-plugin"
	TargetInfrastructure Target = "infrastructure"
	TargetDashboard      Target = "dashboard-ui"
)

const minSamples = 30

// Evidence is one observed workload metric. Lower is better unless HigherIsBetter.
type Evidence struct {
	Metric         string
	Baseline       float64
	Observed       float64
	Samples        int
	HigherIsBetter bool
}

type Experiment struct {
	Environment string // must be "staging"
	Isolated    bool
	Completed   bool
}

// Holdout is an evaluation set the agent must not have seen.
type Holdout struct {
	Evaluated bool
	Passed    bool
	Seen      bool
}

type SLOCheck struct {
	Checked bool
	Within  bool
}

type Budget struct {
	LimitMilliCPU uint64
	UsedMilliCPU  uint64
}

type Credential struct{ Scopes []string }

type Consent struct {
	Granted bool
	By      string // must be a human owner, never the author
	ScopeID string // must equal the proposal ID
}

type LiveChange struct {
	Requested bool
	Consent   Consent
}

type Proposal struct {
	ID           string
	Target       Target
	Author       string
	ChangedPaths []string
	Evidence     []Evidence
	Experiment   Experiment
	Holdout      Holdout
	SLO          SLOCheck
	Budget       Budget
	Credential   Credential
	Live         LiveChange
}

type Decision struct {
	PROpenAllowed       bool
	HumanReviewRequired bool
	MergeAllowed        bool // always false: humans merge
	DeployAllowed       bool // always false: humans deploy
	Reasons             []string
}

var protectedPrefixes = []string{
	"internal/proposal/",
	"internal/benchgate/",
	".github/",
	"tools/",
}

var protectedFiles = []string{"go.mod", "go.sum"}

func isProtected(p string) (bool, bool) {
	c := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if path.IsAbs(c) || c == ".." || strings.HasPrefix(c, "../") {
		return true, true
	}
	for _, f := range protectedFiles {
		if c == f {
			return true, false
		}
	}
	for _, pre := range protectedPrefixes {
		if strings.HasPrefix(c+"/", pre) {
			return true, false
		}
	}
	return false, false
}

func validTarget(t Target) bool {
	return t == TargetPlugin || t == TargetInfrastructure || t == TargetDashboard
}

// Evaluate returns a denial with reasons unless every gate passes.
func Evaluate(p Proposal) Decision {
	var r []string
	add := func(f string, a ...any) { r = append(r, fmt.Sprintf(f, a...)) }

	if !validTarget(p.Target) {
		add("unsupported target %q", string(p.Target))
	}
	if len(p.ChangedPaths) == 0 {
		add("no changed paths declared")
	}
	for _, cp := range p.ChangedPaths {
		if bad, _ := isProtected(cp); bad {
			add("protected path %q may not be changed by the agent", cp)
		}
	}
	if len(p.Evidence) == 0 {
		add("no workload evidence")
	}
	for _, e := range p.Evidence {
		if e.Samples < minSamples {
			add("evidence %q has too few samples (%d < %d)", e.Metric, e.Samples, minSamples)
		}
		better := e.Observed < e.Baseline
		if e.HigherIsBetter {
			better = e.Observed > e.Baseline
		}
		if !better {
			add("evidence %q does not improve on baseline", e.Metric)
		}
	}
	if p.Experiment.Environment != "staging" {
		add("experiment must run in staging")
	}
	if !p.Experiment.Isolated {
		add("staging must be isolated")
	}
	if !p.Experiment.Completed {
		add("experiment not completed")
	}
	if !p.Holdout.Evaluated || !p.Holdout.Passed || p.Holdout.Seen {
		add("holdout must be evaluated, unseen by the agent, and passed")
	}
	if !p.SLO.Checked || !p.SLO.Within {
		add("SLO gate not satisfied")
	}
	if p.Budget.LimitMilliCPU == 0 || p.Budget.UsedMilliCPU > p.Budget.LimitMilliCPU {
		add("budget missing or exceeded")
	}
	if len(p.Credential.Scopes) == 0 {
		add("credential has no scope")
	}
	for _, s := range p.Credential.Scopes {
		if s != "repo:propose" {
			add("credential scope %q exceeds least privilege", s)
		}
	}
	if p.Live.Requested {
		c := p.Live.Consent
		if !c.Granted || c.By == "" || c.By == p.Author || c.ScopeID != p.ID || p.ID == "" {
			add("live change lacks valid human consent bound to this proposal")
		}
	}
	return Decision{PROpenAllowed: len(r) == 0, HumanReviewRequired: true, Reasons: r}
}
