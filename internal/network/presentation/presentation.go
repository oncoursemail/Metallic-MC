// Package presentation builds immutable, policy-filtered player lists and plans
// bounded changes between them. It is an offline core, not a Minecraft packet
// writer. Callers supply authenticated identity/context and authoritative rank
// facts; the package does not authenticate inputs or prove snapshot freshness.
package presentation

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/oncoursemail/metallic-mc/internal/network/policy"
)

const (
	MaxCandidates    = 1024
	MaxRows          = 256
	VisibilityAction = "network.player.see"
)

var (
	ErrInput = errors.New("invalid presentation input")
	ErrReset = errors.New("presentation requires a new session snapshot")
	ErrStale = errors.New("stale or conflicting presentation")
)

// Stamp binds a view to a particular player session and policy/context history.
// Revision is a monotonic view revision assigned by the trusted view owner. It
// is not a database timestamp, signature, or distributed freshness lease.
type Stamp struct {
	AuthorityEpoch  string         `json:"authorityEpoch"`
	ViewerID        string         `json:"viewerId"`
	InstanceID      string         `json:"instanceId"`
	Generation      uint64         `json:"generation"`
	ContextRevision uint64         `json:"contextRevision"`
	Policy          policy.Version `json:"policy"`
	Revision        uint64         `json:"revision"`
}

// Candidate contains literal, server-supplied text. No placeholder expressions,
// MiniMessage markup, executable functions, or external lookups are evaluated.
type Candidate struct {
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	Prefix   string `json:"prefix"`
	Priority int32  `json:"priority"`
}

type Row struct {
	Candidate
	Position int `json:"position"`
}

type Input struct {
	Stamp      Stamp
	Policy     *policy.Snapshot
	Request    policy.Request
	Candidates []Candidate
	Limit      int
}

// Snapshot owns its rows. Accessors return copies, so readers cannot change the
// state used by a later comparison.
type Snapshot struct {
	stamp      Stamp
	rows       []Row
	digest     [32]byte
	builtAt    time.Time
	validUntil time.Time
	built      bool
}

func (s *Snapshot) Stamp() Stamp          { return s.stamp }
func (s *Snapshot) Digest() [32]byte      { return s.digest }
func (s *Snapshot) Rows() []Row           { return append([]Row(nil), s.rows...) }
func (s *Snapshot) ValidUntil() time.Time { return s.validUntil }

// ValidAt checks only known policy time boundaries, never distributed freshness.
func (s *Snapshot) ValidAt(now time.Time) bool {
	now = now.Round(0).UTC()
	return s != nil && s.built && policy.ValidRequestTime(now) && !now.Before(s.builtAt) && (s.validUntil.IsZero() || now.Before(s.validUntil))
}

// Build checks network.player.see on resource type player and each candidate's
// exact PlayerID. The same caller-supplied viewer context applies to every
// candidate. Per-target relationship calculations belong in a future trusted
// context adapter, not in user-controlled placeholders.
func Build(in Input) (*Snapshot, error) {
	s := in.Stamp
	if in.Policy == nil || !identifier(s.AuthorityEpoch) || !identifier(s.ViewerID) || !identifier(s.InstanceID) || s.Generation == 0 || s.ContextRevision == 0 || s.Revision == 0 || s.Policy.Revision == 0 || s.Policy.Digest == ([32]byte{}) || s.Policy != in.Policy.Version() || s.Policy.Epoch != s.AuthorityEpoch {
		return nil, ErrInput
	}
	if in.Request.Expected != s.Policy || in.Request.SubjectID != s.ViewerID || in.Request.Now.IsZero() || in.Limit < 1 || in.Limit > MaxRows || len(in.Candidates) > MaxCandidates {
		return nil, ErrInput
	}
	// Validate the shared viewer facts even for an empty candidate list. The
	// action and resource fields are owned by this builder; the supplied values
	// do not control a visibility query. This validates shape, not authority.
	validation := in.Request
	validation.Action, validation.ResourceType, validation.ResourceID = VisibilityAction, "player", s.ViewerID
	if err := policy.ValidateRequest(validation); err != nil {
		return nil, fmt.Errorf("%w: invalid viewer request", ErrInput)
	}
	seen := make(map[string]bool, len(in.Candidates))
	visible := make([]Candidate, 0, len(in.Candidates))
	var validUntil time.Time
	for _, c := range in.Candidates {
		if !identifier(c.PlayerID) || seen[c.PlayerID] || !literal(c.Name, 64, false) || !literal(c.Prefix, 96, true) {
			return nil, fmt.Errorf("%w: invalid or duplicate candidate", ErrInput)
		}
		seen[c.PlayerID] = true
		r := in.Request
		r.Action = VisibilityAction
		r.ResourceType = "player"
		r.ResourceID = c.PlayerID
		d := in.Policy.Evaluate(r)
		if d.Code == "invalid_request" {
			return nil, fmt.Errorf("%w: invalid visibility request", ErrInput)
		}
		if d.Version != s.Policy {
			return nil, ErrStale
		}
		if !d.ValidUntil.IsZero() && (validUntil.IsZero() || d.ValidUntil.Before(validUntil)) {
			validUntil = d.ValidUntil
		}
		if d.Allowed {
			visible = append(visible, c)
		}
	}
	sort.Slice(visible, func(i, j int) bool {
		if visible[i].Priority != visible[j].Priority {
			return visible[i].Priority > visible[j].Priority
		}
		return visible[i].PlayerID < visible[j].PlayerID
	})
	if len(visible) > in.Limit {
		visible = visible[:in.Limit]
	}
	rows := make([]Row, len(visible))
	for i, c := range visible {
		rows[i] = Row{Candidate: c, Position: i}
	}
	encoded, err := json.Marshal(struct {
		Stamp      Stamp     `json:"stamp"`
		Rows       []Row     `json:"rows"`
		BuiltAt    time.Time `json:"builtAt"`
		ValidUntil time.Time `json:"validUntil"`
	}{s, rows, in.Request.Now.Round(0).UTC(), validUntil.Round(0).UTC()})
	if err != nil {
		return nil, fmt.Errorf("%w: cannot encode view", ErrInput)
	}
	return &Snapshot{stamp: s, rows: rows, digest: sha256.Sum256(encoded), builtAt: in.Request.Now.Round(0).UTC(), validUntil: validUntil.Round(0).UTC(), built: true}, nil
}

type Change struct {
	Kind string `json:"kind"` // remove or upsert
	Row  Row    `json:"row"`
}

// Plan is produced only by Diff. Its private target and base bind all changes
// to the exact prior view. Minecraft clients do not offer atomic multi-packet
// commits; an eventual writer must serialize these changes, remove first,
// abandon stale queued plans, and resynchronize after an interrupted write.
type Plan struct {
	base    [32]byte
	target  *Snapshot
	changes []Change
}

func (p *Plan) Changes() []Change  { return append([]Change(nil), p.changes...) }
func (p *Plan) TargetStamp() Stamp { return p.target.stamp }

func Diff(previous, next *Snapshot) (*Plan, error) {
	if previous == nil || next == nil || !previous.built || !next.built {
		return nil, ErrInput
	}
	a, b := previous.stamp, next.stamp
	if a.AuthorityEpoch != b.AuthorityEpoch || a.ViewerID != b.ViewerID || a.InstanceID != b.InstanceID || a.Generation != b.Generation {
		return nil, ErrReset
	}
	if b.Revision <= a.Revision || b.ContextRevision < a.ContextRevision || b.Policy.Revision < a.Policy.Revision || (b.Policy.Revision == a.Policy.Revision && b.Policy != a.Policy) || next.builtAt.Before(previous.builtAt) {
		return nil, ErrStale
	}
	old := make(map[string]Row, len(previous.rows))
	newRows := make(map[string]Row, len(next.rows))
	for _, r := range previous.rows {
		old[r.PlayerID] = r
	}
	for _, r := range next.rows {
		newRows[r.PlayerID] = r
	}
	p := &Plan{base: previous.digest, target: next}
	for _, r := range previous.rows {
		if _, ok := newRows[r.PlayerID]; !ok {
			p.changes = append(p.changes, Change{Kind: "remove", Row: r})
		}
	}
	for _, r := range next.rows {
		if before, ok := old[r.PlayerID]; !ok || before != r {
			p.changes = append(p.changes, Change{Kind: "upsert", Row: r})
		}
	}
	return p, nil
}

// Apply commits a plan to an offline model only if its exact base still matches.
// It does not send packets or acknowledge delivery to an actual game client.
func Apply(current *Snapshot, plan *Plan, now time.Time) (*Snapshot, error) {
	if current == nil || !current.built || plan == nil || plan.target == nil {
		return nil, ErrInput
	}
	if current.digest != plan.base || !plan.target.ValidAt(now) {
		return nil, ErrStale
	}
	return plan.target, nil
}

func identifier(v string) bool {
	if len(v) == 0 || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ':') {
			return false
		}
	}
	return true
}

func literal(v string, limit int, empty bool) bool {
	if (!empty && len(v) == 0) || len(v) > limit || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
