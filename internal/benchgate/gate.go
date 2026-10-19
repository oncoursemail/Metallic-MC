// Package benchgate screens imported benchmark evidence. It does not run load,
// certify measurements, establish statistical significance, or approve releases.
package benchgate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	SchemaVersion            = "metallic.benchmark-gate/v1"
	MaxInputBytes            = 512 << 10
	MaxTraceBytes      int64 = 16 << 20
	MaxTotalTraceBytes int64 = 128 << 20
	MaxRepetitions           = 30
	maxIssues                = 128
)

type Report struct {
	SchemaVersion   string   `json:"schemaVersion"`
	EvidenceKind    string   `json:"evidenceKind"`
	DeclaredChanges []string `json:"declaredChanges"`
	Baseline        Run      `json:"baseline"`
	Candidate       Run      `json:"candidate"`
}

type Engine struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// Match contains controlled dimensions. Engine identity deliberately lives
// outside this structure: the only permitted change in v1 is engineArtifact.
type Match struct {
	HardwareSHA256         string         `json:"hardwareSHA256"`
	RuntimeSHA256          string         `json:"runtimeSHA256"`
	WorldSHA256            string         `json:"worldSHA256"`
	WorkloadSHA256         string         `json:"workloadSHA256"`
	HarnessSHA256          string         `json:"harnessSHA256"`
	ConfigurationSHA256    string         `json:"configurationSHA256"`
	PluginsSHA256          string         `json:"pluginsSHA256"`
	ClientMatrixSHA256     string         `json:"clientMatrixSHA256"`
	GameplayContractSHA256 string         `json:"gameplayContractSHA256"`
	ActivePlayers          int            `json:"activePlayers"`
	Clients                []ClientCohort `json:"clients"`
}

type ClientCohort struct {
	Edition   string `json:"edition"`
	Version   string `json:"version"`
	Transport string `json:"transport"`
	Input     string `json:"input"`
	Players   int    `json:"players"`
}

type Run struct {
	ID          string       `json:"id"`
	Engine      Engine       `json:"engine"`
	Match       Match        `json:"match"`
	Repetitions []Repetition `json:"repetitions"`
}

type Repetition struct {
	ID                 string      `json:"id"`
	PairID             string      `json:"pairId"`
	StartedAt          string      `json:"startedAt"`
	WarmupSeconds      int         `json:"warmupSeconds"`
	DurationSeconds    int         `json:"durationSeconds"`
	TickSamples        uint64      `json:"tickSamples"`
	InteractionSamples uint64      `json:"interactionSamples"`
	Metrics            Metrics     `json:"metrics"`
	Correctness        Correctness `json:"correctness"`
	Traces             Traces      `json:"traces"`
}

// Pointers distinguish absent/null measurements from a genuine zero rate.
type Metrics struct {
	P99TickMS            *float64 `json:"p99TickMs"`
	P99InteractionMS     *float64 `json:"p99InteractionMs"`
	DeadlineMissFraction *float64 `json:"deadlineMissFraction"`
}

type Correctness struct {
	InventoryConserved        *bool `json:"inventoryConserved"`
	EconomyConserved          *bool `json:"economyConserved"`
	PersistencePassed         *bool `json:"persistencePassed"`
	TransfersPassed           *bool `json:"transfersPassed"`
	PluginCompatibilityPassed *bool `json:"pluginCompatibilityPassed"`
}

type Traces struct {
	Ticks        Artifact `json:"ticks"`
	Interactions Artifact `json:"interactions"`
	Correctness  Artifact `json:"correctness"`
}

type Artifact struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"sizeBytes"`
	SampleCount uint64 `json:"sampleCount"`
}

// Policy is intentionally fixed in v1. These are proposed screening thresholds,
// not measured service guarantees or a replacement for a statistical analysis.
type Policy struct {
	MinimumRepetitions           int     `json:"minimumRepetitions"`
	MinimumDurationSeconds       int     `json:"minimumDurationSeconds"`
	MinimumWarmupSeconds         int     `json:"minimumWarmupSeconds"`
	MinimumTickSamples           uint64  `json:"minimumTickSamples"`
	MinimumInteractionSamples    uint64  `json:"minimumInteractionSamples"`
	MaximumP99TickMS             float64 `json:"maximumP99TickMs"`
	MaximumDeadlineMissFraction  float64 `json:"maximumDeadlineMissFraction"`
	MaximumP99RegressionFraction float64 `json:"maximumP99RegressionFraction"`
}

func DefaultPolicy() Policy {
	return Policy{3, 600, 120, 10000, 1000, 50, 0.01, 0.05}
}

type Issue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type Comparison struct {
	PairID                    string  `json:"pairId"`
	BaselineRepetition        string  `json:"baselineRepetition"`
	CandidateRepetition       string  `json:"candidateRepetition"`
	BaselineP99TickMS         float64 `json:"baselineP99TickMs"`
	CandidateP99TickMS        float64 `json:"candidateP99TickMs"`
	BaselineP99InteractionMS  float64 `json:"baselineP99InteractionMs"`
	CandidateP99InteractionMS float64 `json:"candidateP99InteractionMs"`
	TickDeltaMS               float64 `json:"tickDeltaMs"`
	InteractionDeltaMS        float64 `json:"interactionDeltaMs"`
	TickImproved              bool    `json:"tickImproved"`
	InteractionImproved       bool    `json:"interactionImproved"`
}

type Result struct {
	Status          string       `json:"status"`
	EvidenceMode    string       `json:"evidenceMode"`
	ProductionReady bool         `json:"productionReady"`
	Policy          Policy       `json:"policy"`
	BaselineEngine  Engine       `json:"baselineEngine"`
	CandidateEngine Engine       `json:"candidateEngine"`
	Issues          []Issue      `json:"issues"`
	Comparisons     []Comparison `json:"comparisons"`
	Limitations     []string     `json:"limitations"`
}

func (r *Result) add(code, path, message string) {
	if len(r.Issues) < maxIssues {
		r.Issues = append(r.Issues, Issue{code, path, message})
	}
}

// Decode bounds memory use and rejects duplicate, unknown and incorrectly cased
// object keys, trailing JSON, excessive nesting and non-JSON numeric values.
func Decode(reader io.Reader) (Report, error) {
	var report Report
	data, err := io.ReadAll(io.LimitReader(reader, MaxInputBytes+1))
	if err != nil {
		return report, fmt.Errorf("read evidence: %w", err)
	}
	if len(data) > MaxInputBytes {
		return report, errors.New("evidence exceeds 512 KiB limit")
	}
	scan := json.NewDecoder(bytes.NewReader(data))
	scan.UseNumber()
	if err := checkShape(scan, reflect.TypeOf(report), 0); err != nil {
		return report, fmt.Errorf("invalid evidence JSON: %w", err)
	}
	if _, err := scan.Token(); err != io.EOF {
		return report, errors.New("evidence must contain exactly one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return report, fmt.Errorf("invalid evidence fields: %w", err)
	}
	return report, nil
}

func checkShape(d *json.Decoder, t reflect.Type, depth int) error {
	if depth > 16 {
		return errors.New("JSON nesting exceeds limit")
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	} // Typed decoding and validation check scalar/null values.
	switch delim {
	case '{':
		if t.Kind() != reflect.Struct {
			return errors.New("unexpected object")
		}
		fields := make(map[string]reflect.Type, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			fields[strings.Split(f.Tag.Get("json"), ",")[0]] = f.Type
		}
		seen := map[string]bool{}
		for d.More() {
			keyToken, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			if seen[key] {
				return fmt.Errorf("duplicate field %q", fieldLabel(key))
			}
			seen[key] = true
			ft, ok := fields[key]
			if !ok {
				return fmt.Errorf("unknown field %q", fieldLabel(key))
			}
			if err := checkShape(d, ft, depth+1); err != nil {
				return err
			}
		}
	case '[':
		if t.Kind() != reflect.Slice {
			return errors.New("unexpected array")
		}
		limit := 1 // declaredChanges is the only string slice in the input schema.
		switch t.Elem().Name() {
		case "Repetition":
			limit = MaxRepetitions
		case "ClientCohort":
			limit = 32
		}
		count := 0
		for d.More() {
			count++
			if count > limit {
				return errors.New("JSON array exceeds schema limit")
			}
			if err := checkShape(d, t.Elem(), depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected delimiter")
	}
	_, err = d.Token()
	return err
}

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func fieldLabel(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

func validText(s string, limit int) bool {
	return s != "" && len(s) <= limit && strings.TrimSpace(s) == s && !strings.ContainsFunc(s, unicode.IsControl)
}
func validMetric(p *float64, fraction bool) bool {
	if p == nil || math.IsNaN(*p) || math.IsInf(*p, 0) {
		return false
	}
	if fraction {
		return *p >= 0 && *p <= 1
	}
	return *p > 0 && *p <= 60000
}
func validPath(p string) bool {
	return validText(p, 512) && fs.ValidPath(p) && p != "." && !strings.Contains(p, `\`) && !strings.Contains(p, ":")
}

// Screen requires local trace verification even when called without the CLI.
// Empty roots, synthetic data and incomplete/mismatched evidence fail closed.
// Trace digests verify identity only: metrics and collection remain assertions.
func Screen(report Report, artifactRoot string, now time.Time) Result {
	r := Result{Status: "insufficient_evidence", EvidenceMode: "summary_screening_only", Policy: DefaultPolicy(),
		Issues: []Issue{}, Comparisons: []Comparison{},
		Limitations: []string{
			"Offline descriptive screening only; no load was generated and no deployment is authorized.",
			"Verified trace hashes prove file identity, not authentic collection or correctness of asserted metrics, sample counts, or invariants.",
			"Engine and matched-dimension hashes are imported assertions; their source artifacts are not independently verified here.",
			"Passing allows no claim of statistical significance, universal superiority, faster performance, DDoS protection, or production readiness.",
		}}
	validate(report, now, &r)
	if len(r.Issues) > 0 {
		return r
	}
	r.BaselineEngine = report.Baseline.Engine
	r.CandidateEngine = report.Candidate.Engine
	if artifactRoot == "" {
		r.add("unverified", "artifactRoot", "explicit artifact root is required for trace verification")
		return r
	}
	if err := verifyArtifacts(report, artifactRoot); err != nil {
		r.add("artifact", "traces", err.Error())
		return r
	}
	r.EvidenceMode = "trace_identity_verified"
	baseline := map[string]Repetition{}
	for _, rep := range report.Baseline.Repetitions {
		baseline[rep.PairID] = rep
	}
	for _, c := range report.Candidate.Repetitions {
		b := baseline[c.PairID]
		bt, ct := *b.Metrics.P99TickMS, *c.Metrics.P99TickMS
		bi, ci := *b.Metrics.P99InteractionMS, *c.Metrics.P99InteractionMS
		r.Comparisons = append(r.Comparisons, Comparison{c.PairID, b.ID, c.ID, bt, ct, bi, ci, ct - bt, ci - bi, ct < bt, ci < bi})
		path := "candidate.repetitions." + c.ID
		if ct > r.Policy.MaximumP99TickMS {
			r.add("target", path, "candidate p99 tick duration exceeds proposed 50 ms ceiling")
		}
		if *c.Metrics.DeadlineMissFraction > r.Policy.MaximumDeadlineMissFraction {
			r.add("target", path, "candidate deadline-miss fraction exceeds proposed 1% ceiling")
		}
		if ct > bt*(1+r.Policy.MaximumP99RegressionFraction) {
			r.add("regression", path, "candidate p99 tick duration regressed by more than proposed 5% tolerance")
		}
		if ci > bi*(1+r.Policy.MaximumP99RegressionFraction) {
			r.add("regression", path, "candidate p99 interaction duration regressed by more than proposed 5% tolerance")
		}
	}
	for _, run := range []Run{report.Baseline, report.Candidate} {
		for _, rep := range run.Repetitions {
			for _, check := range correctnessChecks(rep.Correctness) {
				if !*check.value {
					r.add("correctness", run.ID+"."+rep.ID+"."+check.name, "required correctness invariant failed")
				}
			}
		}
	}
	sort.Slice(r.Comparisons, func(i, j int) bool { return r.Comparisons[i].PairID < r.Comparisons[j].PairID })
	r.Status = "passed_screening"
	if len(r.Issues) > 0 {
		r.Status = "failed_screening"
	}
	return r
}

type boolCheck struct {
	name  string
	value *bool
}

func correctnessChecks(c Correctness) []boolCheck {
	return []boolCheck{{"inventoryConserved", c.InventoryConserved}, {"economyConserved", c.EconomyConserved}, {"persistencePassed", c.PersistencePassed}, {"transfersPassed", c.TransfersPassed}, {"pluginCompatibilityPassed", c.PluginCompatibilityPassed}}
}

type namedArtifact struct {
	name     string
	artifact Artifact
}

func artifacts(t Traces) []namedArtifact {
	return []namedArtifact{{"ticks", t.Ticks}, {"interactions", t.Interactions}, {"correctness", t.Correctness}}
}

func validate(report Report, now time.Time, r *Result) {
	if report.SchemaVersion != SchemaVersion {
		r.add("invalid", "schemaVersion", "unsupported or missing schema version")
	}
	if report.EvidenceKind != "measured" {
		r.add("evidence_kind", "evidenceKind", "only explicitly measured evidence is eligible; synthetic or unknown evidence cannot pass")
	}
	if len(report.DeclaredChanges) != 1 || report.DeclaredChanges[0] != "engineArtifact" {
		r.add("change", "declaredChanges", "v1 requires exactly the declared engineArtifact change")
	}
	if report.Baseline.ID == report.Candidate.ID {
		r.add("duplicate", "run.id", "baseline and candidate run IDs must differ")
	}
	if report.Baseline.Engine.SHA256 == report.Candidate.Engine.SHA256 {
		r.add("change", "engine.sha256", "declared engine artifact must actually differ")
	}
	seenIDs, seenPaths := map[string]bool{}, map[string]bool{}
	type interval struct {
		start, end time.Time
		id         string
	}
	intervals := []interval{}
	var total int64
	for i, run := range []Run{report.Baseline, report.Candidate} {
		label := []string{"baseline", "candidate"}[i]
		if !idPattern.MatchString(run.ID) {
			r.add("invalid", label+".id", "missing or invalid run ID")
		}
		if !validText(run.Engine.Name, 128) || !validText(run.Engine.Version, 128) || !hashPattern.MatchString(run.Engine.SHA256) {
			r.add("invalid", label+".engine", "engine name/version and lowercase SHA-256 are required")
		}
		validateMatch(run.Match, label+".match", r)
		if len(run.Repetitions) < r.Policy.MinimumRepetitions || len(run.Repetitions) > MaxRepetitions {
			r.add("insufficient", label+".repetitions", "require between 3 and 30 independent repetitions")
		}
		pairs := map[string]bool{}
		for j, rep := range run.Repetitions {
			if j >= MaxRepetitions {
				break
			}
			path := fmt.Sprintf("%s.repetitions[%d]", label, j)
			if !idPattern.MatchString(rep.ID) || seenIDs[rep.ID] {
				r.add("duplicate", path+".id", "repetition IDs must be valid and globally unique")
			}
			seenIDs[rep.ID] = true
			if !idPattern.MatchString(rep.PairID) || pairs[rep.PairID] {
				r.add("duplicate", path+".pairId", "pair IDs must be valid and unique within each run")
			}
			pairs[rep.PairID] = true
			if rep.DurationSeconds < r.Policy.MinimumDurationSeconds || rep.DurationSeconds > 86400 {
				r.add("insufficient", path+".durationSeconds", "measured duration must be 600 to 86400 seconds")
			}
			if rep.WarmupSeconds < r.Policy.MinimumWarmupSeconds || rep.WarmupSeconds > 7200 {
				r.add("insufficient", path+".warmupSeconds", "warmup must be 120 to 7200 seconds")
			}
			start, err := time.Parse(time.RFC3339Nano, rep.StartedAt)
			if err != nil || !strings.HasSuffix(rep.StartedAt, "Z") || start.After(now) {
				r.add("timestamp", path+".startedAt", "valid UTC timestamp must not be in the future")
			} else if rep.DurationSeconds >= 0 && rep.DurationSeconds <= 86400 && rep.WarmupSeconds >= 0 && rep.WarmupSeconds <= 7200 {
				end := start.Add(time.Duration(rep.DurationSeconds+rep.WarmupSeconds) * time.Second)
				if end.After(now) {
					r.add("timestamp", path, "warmup and measured run must have completed before evaluation")
				}
				intervals = append(intervals, interval{start, end, rep.ID})
			}
			if rep.TickSamples < r.Policy.MinimumTickSamples || rep.TickSamples > 1000000000 || rep.InteractionSamples < r.Policy.MinimumInteractionSamples || rep.InteractionSamples > 1000000000 {
				r.add("insufficient", path+".samples", "insufficient or implausibly large raw sample counts")
			}
			if !validMetric(rep.Metrics.P99TickMS, false) || !validMetric(rep.Metrics.P99InteractionMS, false) || !validMetric(rep.Metrics.DeadlineMissFraction, true) {
				r.add("invalid", path+".metrics", "all metrics are required and finite; latencies must be positive and <=60000 ms, fractions in [0,1]")
			}
			for _, check := range correctnessChecks(rep.Correctness) {
				if check.value == nil {
					r.add("missing", path+".correctness."+check.name, "explicit correctness result is required")
				}
			}
			for _, entry := range artifacts(rep.Traces) {
				a := entry.artifact
				if !validPath(a.Path) || !hashPattern.MatchString(a.SHA256) || a.SizeBytes <= 0 || a.SizeBytes > MaxTraceBytes || a.SampleCount == 0 || a.SampleCount > 1000000000 {
					r.add("invalid", path+".traces."+entry.name, "nonempty trace requires a scoped relative path, lowercase SHA-256, valid size and sample count")
				}
				if seenPaths[a.Path] {
					r.add("duplicate", path+".traces."+entry.name, "each repetition must reference distinct trace paths")
				}
				seenPaths[a.Path] = true
				if a.SizeBytes > 0 && a.SizeBytes <= MaxTraceBytes {
					total += a.SizeBytes
				}
			}
			if rep.Traces.Ticks.SampleCount != rep.TickSamples || rep.Traces.Interactions.SampleCount != rep.InteractionSamples || rep.Traces.Correctness.SampleCount < 5 {
				r.add("mismatch", path+".traces", "trace counts must match raw tick/interaction counts and cover all five correctness assertions")
			}
		}
	}
	if total > MaxTotalTraceBytes {
		r.add("limit", "traces", "combined trace size exceeds 128 MiB")
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start.Before(intervals[j].start) })
	for i := 1; i < len(intervals); i++ {
		if intervals[i].start.Before(intervals[i-1].end) {
			r.add("duplicate", "repetitions."+intervals[i].id, "repetitions on matched hardware must not overlap or reuse collection windows")
		}
	}
	if len(report.Baseline.Match.Clients) <= 32 && len(report.Candidate.Match.Clients) <= 32 && !sameMatch(report.Baseline.Match, report.Candidate.Match) {
		r.add("mismatch", "match", "hardware/runtime/world/workload/harness/shared-config/plugins/gameplay/client hashes and population/cohorts must match")
	}
	base := map[string]Repetition{}
	for i, rep := range report.Baseline.Repetitions {
		if i >= MaxRepetitions {
			break
		}
		base[rep.PairID] = rep
	}
	if len(report.Baseline.Repetitions) != len(report.Candidate.Repetitions) {
		r.add("mismatch", "repetitions", "baseline and candidate repetition counts differ")
	}
	for i, rep := range report.Candidate.Repetitions {
		if i >= MaxRepetitions {
			break
		}
		b, ok := base[rep.PairID]
		if !ok || b.DurationSeconds != rep.DurationSeconds || b.WarmupSeconds != rep.WarmupSeconds {
			r.add("mismatch", "repetitions."+rep.PairID, "each pair must have matching pair ID, duration and warmup")
		}
	}
}

func validateMatch(m Match, path string, r *Result) {
	v := reflect.ValueOf(m)
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		if strings.HasSuffix(typ.Field(i).Name, "SHA256") && !hashPattern.MatchString(v.Field(i).String()) {
			r.add("invalid", path+"."+typ.Field(i).Tag.Get("json"), "lowercase SHA-256 is required")
		}
	}
	if m.ActivePlayers < 1 || m.ActivePlayers > 100000 || len(m.Clients) < 1 || len(m.Clients) > 32 {
		r.add("invalid", path+".clients", "require positive bounded population and 1 to 32 explicit client cohorts")
	}
	total := 0
	seen := map[string]bool{}
	for i, c := range m.Clients {
		if i >= 32 {
			break
		}
		key := c.Edition + "\x00" + c.Version + "\x00" + c.Transport + "\x00" + c.Input
		transportOK := (c.Edition == "java" && c.Transport == "tcp") || (c.Edition == "bedrock" && (c.Transport == "nethernet" || c.Transport == "raknet"))
		inputOK := c.Input == "keyboard-mouse" || c.Input == "controller" || c.Input == "touch"
		if !transportOK || !inputOK || !validText(c.Version, 64) || c.Players < 1 || c.Players > 100000 || seen[key] {
			r.add("invalid", path+".clients", "cohorts must have a supported edition/transport/input, version, positive population and no duplicates")
		}
		seen[key] = true
		if c.Players > 0 && c.Players <= 100000 {
			total += c.Players
		}
	}
	if total != m.ActivePlayers {
		r.add("mismatch", path+".activePlayers", "cohort population must equal activePlayers")
	}
}

func sameMatch(a, b Match) bool {
	a.Clients = append([]ClientCohort(nil), a.Clients...)
	b.Clients = append([]ClientCohort(nil), b.Clients...)
	less := func(x, y ClientCohort) bool {
		ax, _ := json.Marshal(x)
		by, _ := json.Marshal(y)
		return string(ax) < string(by)
	}
	sort.Slice(a.Clients, func(i, j int) bool { return less(a.Clients[i], a.Clients[j]) })
	sort.Slice(b.Clients, func(i, j int) bool { return less(b.Clients[i], b.Clients[j]) })
	return reflect.DeepEqual(a, b)
}

func verifyArtifacts(report Report, rootPath string) error {
	root, err := openArtifactRoot(rootPath)
	if err != nil {
		return fmt.Errorf("open artifact root: %w", err)
	}
	defer root.Close()
	for _, run := range []Run{report.Baseline, report.Candidate} {
		for _, rep := range run.Repetitions {
			for _, entry := range artifacts(rep.Traces) {
				if err := verifyArtifact(root, entry.artifact); err != nil {
					return fmt.Errorf("%s/%s/%s: %w", run.ID, rep.ID, entry.name, err)
				}
			}
		}
	}
	return nil
}

func verifyArtifact(root artifactRoot, a Artifact) error {
	f, err := root.Open(a.Path)
	if err != nil {
		return fmt.Errorf("open scoped regular trace: %w", err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() || before.Size() != a.SizeBytes {
		return errors.New("trace must be regular and match declared nonzero byte size")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, MaxTraceBytes+1))
	if err != nil {
		return err
	}
	if n != a.SizeBytes || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return errors.New("trace size or SHA-256 does not match")
	}
	after, err := f.Stat()
	if err != nil {
		return err
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return errors.New("trace changed while being read")
	}
	return nil
}
