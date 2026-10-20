package presentation

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/network/policy"
)

var testNow = time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)

func fixture(t testing.TB, revision uint64, denyBob bool) Input {
	t.Helper()
	d := policy.Document{Schema: 1, Epoch: "lab-epoch", Revision: revision, TenantID: "metallic-mc",
		Roles: []policy.Role{{ID: "reader"}}, Subjects: []policy.Subject{{ID: "viewer", Memberships: []policy.Membership{{RoleID: "reader"}}}}}
	for _, id := range []string{"alice", "bob", "charlie"} {
		d.Rules = append(d.Rules, policy.Rule{ID: "see-" + id, RoleID: "reader", Action: VisibilityAction, ResourceType: "player", ResourceID: id, Effect: policy.Allow})
	}
	if denyBob {
		d.Rules = append(d.Rules, policy.Rule{ID: "hide-bob", SubjectID: "viewer", Action: VisibilityAction, ResourceType: "player", ResourceID: "bob", Effect: policy.Deny})
	}
	s, err := policy.Compile(d)
	if err != nil {
		t.Fatal(err)
	}
	stamp := Stamp{AuthorityEpoch: "lab-epoch", ViewerID: "viewer", InstanceID: "gateway-1-incarnation", Generation: 1, ContextRevision: 1, Policy: s.Version(), Revision: revision}
	return Input{Stamp: stamp, Policy: s, Request: policy.Request{Expected: s.Version(), TenantID: "metallic-mc", SubjectID: "viewer", Now: testNow}, Limit: 3,
		Candidates: []Candidate{{PlayerID: "bob", Name: "Bob", Prefix: "[MOD]", Priority: 10}, {PlayerID: "alice", Name: "Alice", Priority: 5}, {PlayerID: "charlie", Name: "Charlie", Priority: 5}}}
}

func build(t testing.TB, in Input) *Snapshot {
	t.Helper()
	s, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVisibilityOrderingAndDetachedRows(t *testing.T) {
	in := fixture(t, 1, true)
	s := build(t, in)
	rows := s.Rows()
	if len(rows) != 2 || rows[0].PlayerID != "alice" || rows[1].PlayerID != "charlie" || rows[1].Position != 1 {
		t.Fatalf("wrong visible list: %+v", rows)
	}
	rows[0].Name = "changed"
	in.Candidates[1].Name = "also changed"
	if s.Rows()[0].Name != "Alice" {
		t.Fatal("snapshot aliases mutable input or output")
	}
	other := fixture(t, 1, true)
	other.Candidates[0], other.Candidates[2] = other.Candidates[2], other.Candidates[0]
	if s.Digest() != build(t, other).Digest() {
		t.Fatal("input ordering changed canonical view")
	}
	other.Request.TenantID = "other-project"
	if len(build(t, other).Rows()) != 0 {
		t.Fatal("foreign tenant obtained player visibility")
	}
}

func TestRevocationRemovesBeforeRankUpdateAndPlanCannotReplay(t *testing.T) {
	before := build(t, fixture(t, 1, false))
	in := fixture(t, 2, true)
	in.Candidates[1].Prefix = "[BUILDER]"
	after := build(t, in)
	p, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	changes := p.Changes()
	if len(changes) < 2 || changes[0].Kind != "remove" || changes[0].Row.PlayerID != "bob" {
		t.Fatalf("privacy removal must lead: %+v", changes)
	}
	changes[0].Kind = "upsert"
	if p.Changes()[0].Kind != "remove" {
		t.Fatal("plan changes are mutable through accessor")
	}
	current, err := Apply(before, p, testNow)
	if err != nil || current.Digest() != after.Digest() {
		t.Fatal("plan did not apply", err)
	}
	if _, err := Apply(current, p, testNow); !errors.Is(err, ErrStale) {
		t.Fatal("replayed plan accepted", err)
	}
	if _, err := Diff(after, before); !errors.Is(err, ErrStale) {
		t.Fatal("policy/view rollback accepted", err)
	}
}

func TestUnchangedRowsProduceNoOutputChanges(t *testing.T) {
	in := fixture(t, 1, false)
	before := build(t, in)
	in.Stamp.Revision++
	after := build(t, in)
	p, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes()) != 0 {
		t.Fatal("unchanged data generated visual writes")
	}
	if next, err := Apply(before, p, testNow); err != nil || next.Stamp().Revision != 2 {
		t.Fatal("empty delta did not advance view revision", err)
	}
}

func TestSessionResetAndContextRollback(t *testing.T) {
	in := fixture(t, 1, false)
	before := build(t, in)
	in.Stamp.Revision = 2
	in.Stamp.Generation = 2
	if _, err := Diff(before, build(t, in)); !errors.Is(err, ErrReset) {
		t.Fatal("cross-session delta accepted", err)
	}
	in.Stamp.Generation = 1
	in.Stamp.ContextRevision = 2
	after := build(t, in)
	in.Stamp.Revision = 3
	in.Stamp.ContextRevision = 1
	if _, err := Diff(after, build(t, in)); !errors.Is(err, ErrStale) {
		t.Fatal("context rollback accepted", err)
	}
	conflict := fixture(t, 1, true)
	conflict.Stamp.Revision = 2
	if _, err := Diff(before, build(t, conflict)); !errors.Is(err, ErrStale) {
		t.Fatal("same-revision different policy accepted", err)
	}
}

func TestFutureDenyExpiresQueuedPresentation(t *testing.T) {
	in := fixture(t, 1, false)
	deadline := testNow.Add(time.Second)
	d := policy.Document{Schema: 1, Epoch: "lab-epoch", Revision: 1, TenantID: "metallic-mc", Subjects: []policy.Subject{{ID: "viewer"}},
		Rules: []policy.Rule{{ID: "allow", SubjectID: "viewer", Action: VisibilityAction, ResourceType: "player", ResourceID: "alice", Effect: policy.Allow},
			{ID: "hide-later", SubjectID: "viewer", Action: VisibilityAction, ResourceType: "player", ResourceID: "alice", Effect: policy.Deny, NotBefore: deadline}}}
	s, err := policy.Compile(d)
	if err != nil {
		t.Fatal(err)
	}
	in.Policy = s
	in.Stamp.Policy = s.Version()
	in.Request.Expected = s.Version()
	before := build(t, in)
	in.Stamp.Revision++
	after := build(t, in)
	if !after.ValidUntil().Equal(deadline) || !after.ValidAt(deadline.Add(-time.Nanosecond)) || after.ValidAt(deadline) {
		t.Fatal("scheduled deny did not end view validity")
	}
	p, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(before, p, deadline); !errors.Is(err, ErrStale) {
		t.Fatal("expired queued view applied", err)
	}
	in.Request.Now = deadline
	in.Stamp.Revision++
	if rows := build(t, in).Rows(); len(rows) != 0 {
		t.Fatal("deny not reflected at exact boundary", rows)
	}
}

func TestBoundsAndInvalidInputs(t *testing.T) {
	cases := map[string]func(*Input){
		"missing policy":          func(i *Input) { i.Policy = nil },
		"wrong viewer":            func(i *Input) { i.Request.SubjectID = "imposter" },
		"wrong expected revision": func(i *Input) { i.Request.Expected.Revision++ },
		"zero limit":              func(i *Input) { i.Limit = 0 },
		"oversize limit":          func(i *Input) { i.Limit = MaxRows + 1 },
		"candidate cap":           func(i *Input) { i.Candidates = make([]Candidate, MaxCandidates+1) },
		"duplicate":               func(i *Input) { i.Candidates[1] = i.Candidates[0] },
		"control text":            func(i *Input) { i.Candidates[0].Name = "Bob\nOP" },
		"bidi text":               func(i *Input) { i.Candidates[0].Name = "Bob\u202e" },
		"long prefix":             func(i *Input) { i.Candidates[0].Prefix = strings.Repeat("x", 97) },
		"invalid utf8":            func(i *Input) { i.Candidates[0].Name = string([]byte{0xff}) },
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			in := fixture(t, 1, false)
			alter(&in)
			if _, err := Build(in); !errors.Is(err, ErrInput) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, err := Diff(&Snapshot{}, &Snapshot{}); !errors.Is(err, ErrInput) {
		t.Fatal("zero snapshots accepted")
	}
}

func TestLimitSelectsVisibleRowsAfterFiltering(t *testing.T) {
	in := fixture(t, 1, true)
	in.Limit = 1
	s := build(t, in)
	if got := s.Rows(); len(got) != 1 || got[0].PlayerID != "alice" {
		t.Fatal("hidden high-priority player consumed visible capacity", got)
	}
	if !reflect.DeepEqual(s.Stamp(), in.Stamp) {
		t.Fatal("stamp not retained")
	}
}

func TestRequestValidationRunsWithAndWithoutCandidates(t *testing.T) {
	for _, empty := range []bool{false, true} {
		cases := map[string]func(*Input){
			"malformed context":      func(in *Input) { in.Request.Context = map[string]string{"world": "bad\nvalue"} },
			"invalid tenant grammar": func(in *Input) { in.Request.TenantID = "../tenant" },
			"before clock domain":    func(in *Input) { in.Request.Now = time.Date(1960, 1, 1, 0, 0, 0, 0, time.UTC) },
			"after clock domain":     func(in *Input) { in.Request.Now = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) },
		}
		for name, alter := range cases {
			t.Run(name+map[bool]string{false: "/candidates", true: "/empty"}[empty], func(t *testing.T) {
				in := fixture(t, 1, false)
				if empty {
					in.Candidates = nil
				}
				alter(&in)
				if _, err := Build(in); !errors.Is(err, ErrInput) {
					t.Fatalf("invalid request produced a view: %v", err)
				}
			})
		}
	}
	in := fixture(t, 1, false)
	in.Candidates[0].PlayerID = ".invalid-policy-id"
	if _, err := Build(in); !errors.Is(err, ErrInput) {
		t.Fatal("invalid candidate policy identifier accepted", err)
	}
	view := build(t, fixture(t, 1, false))
	if view.ValidAt(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("view accepts time outside policy clock domain")
	}
	if view.ValidAt(testNow.Add(-time.Nanosecond)) {
		t.Fatal("view is valid before it was built")
	}
}

func BenchmarkBuildPlayerView(b *testing.B) {
	in := fixture(b, 1, false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Build(in); err != nil {
			b.Fatal(err)
		}
	}
}
