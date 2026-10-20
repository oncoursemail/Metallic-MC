package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

var fixtureNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func fixtureDocument() Document {
	return Document{
		Schema: SchemaVersion, Epoch: "fixture-epoch", Revision: 7, TenantID: "metallic",
		Roles: []Role{{ID: "member"}, {ID: "moderator", Parents: []string{"member"}}},
		Subjects: []Subject{
			{ID: "java:alice", Memberships: []Membership{{RoleID: "moderator", Context: map[string]string{"server": "survival"}}}},
			{ID: "bedrock:bob"},
		},
		Rules: []Rule{{ID: "member.build", RoleID: "member", Action: "world.build", ResourceType: "world", ResourceID: "main", Effect: Allow, Context: map[string]string{"world": "main"}}},
	}
}

func mustCompile(t testing.TB, d Document) *Snapshot {
	t.Helper()
	s, err := Compile(d)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func requestFor(s *Snapshot) Request {
	return Request{Expected: s.Version(), TenantID: "metallic", SubjectID: "java:alice",
		Action: "world.build", ResourceType: "world", ResourceID: "main",
		Context: map[string]string{"server": "survival", "world": "main"}, Now: fixtureNow}
}

func assertDecision(t *testing.T, got Decision, allowed bool, code string) {
	t.Helper()
	if got.Allowed != allowed || got.Code != code {
		t.Fatalf("decision = %+v; want allowed=%v code=%s", got, allowed, code)
	}
}

func TestExactMatchingAndWholeRequestValidation(t *testing.T) {
	s := mustCompile(t, fixtureDocument())
	base := s.Evaluate(requestFor(s))
	assertDecision(t, base, true, "explicit_allow")
	if base.RuleID != "member.build" || !slices.Equal(base.InheritancePath, []string{"moderator", "member"}) {
		t.Fatalf("missing inheritance explanation: %+v", base)
	}
	tests := []struct {
		name   string
		change func(*Request)
		code   string
	}{
		{"unknown subject", func(r *Request) { r.SubjectID = "java:missing" }, "unknown_subject"},
		{"other registered subject", func(r *Request) { r.SubjectID = "bedrock:bob" }, "no_matching_allow"},
		{"unknown action", func(r *Request) { r.Action = "world.delete" }, "unknown_action"},
		{"action case", func(r *Request) { r.Action = "World.build" }, "unknown_action"},
		{"resource type", func(r *Request) { r.ResourceType = "server" }, "no_matching_allow"},
		{"resource identifier", func(r *Request) { r.ResourceID = "Main" }, "no_matching_allow"},
		{"tenant", func(r *Request) { r.TenantID = "other" }, "tenant_mismatch"},
		{"missing membership context", func(r *Request) { delete(r.Context, "server") }, "no_matching_allow"},
		{"missing rule context", func(r *Request) { delete(r.Context, "world") }, "no_matching_allow"},
		{"context case", func(r *Request) { r.Context["world"] = "Main" }, "no_matching_allow"},
		{"context wildcard is literal", func(r *Request) { r.Context["world"] = "*" }, "no_matching_allow"},
		{"zero clock", func(r *Request) { r.Now = time.Time{} }, "invalid_request"},
		{"out of range clock", func(r *Request) { r.Now = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }, "invalid_request"},
		{"unused empty context", func(r *Request) { r.Context["unused"] = "" }, "invalid_request"},
		{"unused control context", func(r *Request) { r.Context["unused"] = "fixture\nvalue" }, "invalid_request"},
		{"unused bidi control", func(r *Request) { r.Context["unused"] = "fixture\u202evalue" }, "invalid_request"},
		{"invalid UTF8", func(r *Request) { r.Context["unused"] = string([]byte{0xff}) }, "invalid_request"},
		{"oversized context", func(r *Request) { r.Context["unused"] = strings.Repeat("x", MaxContextValue+1) }, "invalid_request"},
		{"invalid unused key", func(r *Request) { r.Context["bad key"] = "fixture" }, "invalid_request"},
		{"wildcard action", func(r *Request) { r.Action = "world.*" }, "invalid_request"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := requestFor(s)
			test.change(&r)
			err := ValidateRequest(r)
			if test.code == "invalid_request" {
				if !errors.Is(err, ErrInvalidRequest) {
					t.Fatalf("structural validation = %v; want ErrInvalidRequest", err)
				}
			} else if err != nil {
				t.Fatalf("well-formed request rejected before authorization: %v", err)
			}
			assertDecision(t, s.Evaluate(r), false, test.code)
		})
	}
	var absent *Snapshot
	assertDecision(t, absent.Evaluate(Request{}), false, "uninitialized")
}

func TestRequestTimeBoundariesUseUTCInstants(t *testing.T) {
	s := mustCompile(t, fixtureDocument())
	last := time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"zero sentinel is not a clock", time.Time{}, false},
		{"first instant", time.Unix(0, 0), true},
		{"before first instant", time.Unix(-1, 999999999), false},
		{"last instant", last, true},
		{"after last instant", last.Add(time.Nanosecond), false},
		{"local 1970 but UTC 1969", time.Date(1970, 1, 1, 0, 0, 0, 0, time.FixedZone("fixture-east", 3600)), false},
		{"local 1969 but UTC 1970", time.Date(1969, 12, 31, 23, 0, 0, 0, time.FixedZone("fixture-west", -3600)), true},
		{"local 10000 but UTC 9999", time.Date(10000, 1, 1, 0, 0, 0, 0, time.FixedZone("fixture-east", 3600)), true},
		{"local 9999 but UTC 10000", last.In(time.FixedZone("fixture-west", -3600)).Add(time.Nanosecond), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidRequestTime(test.now); got != test.want {
				t.Fatalf("ValidRequestTime(%v) = %v; want %v", test.now, got, test.want)
			}
			r := requestFor(s)
			r.Now = test.now
			if test.want {
				if err := ValidateRequest(r); err != nil {
					t.Fatal(err)
				}
				assertDecision(t, s.Evaluate(r), true, "explicit_allow")
			} else {
				if err := ValidateRequest(r); !errors.Is(err, ErrInvalidRequest) {
					t.Fatalf("invalid clock validation = %v", err)
				}
				assertDecision(t, s.Evaluate(r), false, "invalid_request")
			}
		})
	}
}

func TestStructuralRequestValidationDoesNotConferAuthority(t *testing.T) {
	s := mustCompile(t, fixtureDocument())
	tests := []struct {
		name   string
		change func(*Request)
		code   string
	}{
		{"no selected version", func(r *Request) { r.Expected = Version{} }, "version_mismatch"},
		{"unknown subject", func(r *Request) { r.SubjectID = "java:unregistered" }, "unknown_subject"},
		{"missing required context", func(r *Request) { r.Context = nil }, "no_matching_allow"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := requestFor(s)
			test.change(&r)
			if err := ValidateRequest(r); err != nil {
				t.Fatalf("structural validator asserted authority: %v", err)
			}
			assertDecision(t, s.Evaluate(r), false, test.code)
		})
	}
	r := requestFor(s)
	r.Context["unused"] = "obvious-fixture\ncontext"
	if err := ValidateRequest(r); !errors.Is(err, ErrInvalidRequest) || strings.Contains(err.Error(), "obvious-fixture") {
		t.Fatalf("validation must return a sanitized error class: %v", err)
	}
}

func TestVersionBindsEpochRevisionAndContents(t *testing.T) {
	s := mustCompile(t, fixtureDocument())
	for _, field := range []string{"epoch", "revision", "digest"} {
		t.Run(field, func(t *testing.T) {
			r := requestFor(s)
			switch field {
			case "epoch":
				r.Expected.Epoch = "different-epoch"
			case "revision":
				r.Expected.Revision++
			case "digest":
				r.Expected.Digest[0] ^= 1
			}
			assertDecision(t, s.Evaluate(r), false, "version_mismatch")
		})
	}
	d := fixtureDocument()
	d.Rules[0].Effect = Deny // publisher accidentally reused epoch + revision
	changed := mustCompile(t, d)
	if changed.Version() == s.Version() {
		t.Fatal("changed policy reused the complete version")
	}
	r := requestFor(changed)
	r.Expected = s.Version()
	assertDecision(t, changed.Evaluate(r), false, "version_mismatch")
}

func TestDenyOverridesDirectTemporaryAllowAndHasStableReason(t *testing.T) {
	d := fixtureDocument()
	allow := d.Rules[0]
	allow.ID, allow.SubjectID, allow.RoleID = "00.direct.allow", "java:alice", ""
	allow.ExpiresAt = fixtureNow.Add(time.Hour)
	deny := d.Rules[0]
	deny.ID, deny.Effect = "z.inherited.deny", Deny
	another := deny
	another.ID = "a.inherited.deny"
	d.Rules = []Rule{deny, allow, another}
	s := mustCompile(t, d)
	got := s.Evaluate(requestFor(s))
	assertDecision(t, got, false, "explicit_deny")
	if got.RuleID != "a.inherited.deny" || !slices.Equal(got.InheritancePath, []string{"moderator", "member"}) {
		t.Fatalf("unstable deny explanation: %+v", got)
	}
	// A deny for a different resource or context cannot revoke unrelated access.
	d.Rules[0].ResourceID = "other"
	d.Rules[2].Context = map[string]string{"world": "other"}
	s = mustCompile(t, d)
	got = s.Evaluate(requestFor(s))
	assertDecision(t, got, true, "explicit_allow")
	if got.RuleID != allow.ID || len(got.InheritancePath) != 0 {
		t.Fatalf("direct rule explanation: %+v", got)
	}
}

func TestShortestLexicographicActiveInheritanceWitness(t *testing.T) {
	d := fixtureDocument()
	d.Roles = []Role{
		{ID: "member"}, {ID: "left", Parents: []string{"member"}}, {ID: "right", Parents: []string{"member"}},
		{ID: "top", Parents: []string{"right", "left"}},
	}
	d.Subjects[0].Memberships = []Membership{{RoleID: "top"}}
	s := mustCompile(t, d)
	got := s.Evaluate(requestFor(s))
	if !got.Allowed || !slices.Equal(got.InheritancePath, []string{"top", "left", "member"}) {
		t.Fatalf("diamond witness: %+v", got)
	}
	d.Subjects[0].Memberships = append(d.Subjects[0].Memberships,
		Membership{RoleID: "right", ExpiresAt: fixtureNow.Add(time.Second)},
		Membership{RoleID: "left", NotBefore: fixtureNow.Add(time.Second)},
		Membership{RoleID: "member", Context: map[string]string{"server": "not-this-server"}})
	s = mustCompile(t, d)
	r := requestFor(s)
	got = s.Evaluate(r)
	if !got.Allowed || !slices.Equal(got.InheritancePath, []string{"right", "member"}) {
		t.Fatalf("shorter active path not selected: %+v", got)
	}
	r.Now = r.Now.Add(time.Second)
	got = s.Evaluate(r)
	if !got.Allowed || !slices.Equal(got.InheritancePath, []string{"left", "member"}) {
		t.Fatalf("expired path retained: %+v", got)
	}
}

func TestHalfOpenWindowsForRulesAndMemberships(t *testing.T) {
	for _, kind := range []string{"rule", "membership"} {
		t.Run(kind, func(t *testing.T) {
			d := fixtureDocument()
			start, end := fixtureNow, fixtureNow.Add(time.Second)
			if kind == "rule" {
				d.Rules[0].NotBefore, d.Rules[0].ExpiresAt = start, end
			} else {
				d.Subjects[0].Memberships[0].NotBefore, d.Subjects[0].Memberships[0].ExpiresAt = start, end
			}
			s := mustCompile(t, d)
			for _, tc := range []struct {
				now      time.Time
				allow    bool
				boundary time.Time
			}{
				{start.Add(-time.Nanosecond), false, start}, {start, true, end},
				{end.Add(-time.Nanosecond), true, end}, {end, false, time.Time{}},
			} {
				r := requestFor(s)
				r.Now = tc.now
				got := s.Evaluate(r)
				if got.Allowed != tc.allow || !got.ValidUntil.Equal(tc.boundary) {
					t.Fatalf("at %v: %+v; want allowed=%v boundary=%v", tc.now, got, tc.allow, tc.boundary)
				}
			}
		})
	}
}

func TestFutureDenialAndUnrelatedBoundariesExpireCachedMeaning(t *testing.T) {
	d := fixtureDocument()
	futureDeny := d.Rules[0]
	futureDeny.ID, futureDeny.Effect = "future.deny", Deny
	futureDeny.NotBefore = fixtureNow.Add(2 * time.Second)
	d.Rules = append(d.Rules, futureDeny)
	d.Subjects[1].Memberships = []Membership{{RoleID: "member", NotBefore: fixtureNow.Add(time.Second)}}
	s := mustCompile(t, d)
	r := requestFor(s)
	got := s.Evaluate(r)
	if !got.Allowed || !got.ValidUntil.Equal(fixtureNow.Add(time.Second)) {
		t.Fatalf("conservative future membership boundary missing: %+v", got)
	}
	r.Now = fixtureNow.Add(time.Second)
	got = s.Evaluate(r)
	if !got.Allowed || !got.ValidUntil.Equal(futureDeny.NotBefore) {
		t.Fatalf("future deny did not bound allowed decision: %+v", got)
	}
	r.Now = futureDeny.NotBefore
	assertDecision(t, s.Evaluate(r), false, "explicit_deny")
}

func TestCompileValidatesUnusedDataAndGraph(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Document)
	}{
		{"wrong schema", func(d *Document) { d.Schema++ }},
		{"zero revision", func(d *Document) { d.Revision = 0 }},
		{"empty epoch", func(d *Document) { d.Epoch = "" }},
		{"invalid tenant", func(d *Document) { d.TenantID = "../fixture" }},
		{"duplicate role", func(d *Document) { d.Roles = append(d.Roles, d.Roles[0]) }},
		{"unknown parent", func(d *Document) { d.Roles[0].Parents = []string{"missing"} }},
		{"duplicate parent", func(d *Document) { d.Roles[1].Parents = []string{"member", "member"} }},
		{"cycle", func(d *Document) { d.Roles[0].Parents = []string{"moderator"} }},
		{"unused self cycle", func(d *Document) { d.Roles = append(d.Roles, Role{ID: "unused", Parents: []string{"unused"}}) }},
		{"unknown membership", func(d *Document) { d.Subjects[1].Memberships = []Membership{{RoleID: "missing"}} }},
		{"duplicate membership", func(d *Document) {
			d.Subjects[0].Memberships = append(d.Subjects[0].Memberships, d.Subjects[0].Memberships[0])
		}},
		{"duplicate subject", func(d *Document) { d.Subjects = append(d.Subjects, d.Subjects[0]) }},
		{"duplicate rule", func(d *Document) { d.Rules = append(d.Rules, d.Rules[0]) }},
		{"unknown rule subject", func(d *Document) { d.Rules[0].RoleID, d.Rules[0].SubjectID = "", "missing" }},
		{"unknown rule role", func(d *Document) { d.Rules[0].RoleID = "missing" }},
		{"both selectors", func(d *Document) { d.Rules[0].SubjectID = "java:alice" }},
		{"neither selector", func(d *Document) { d.Rules[0].RoleID = "" }},
		{"invalid effect", func(d *Document) { d.Rules[0].Effect = "maybe" }},
		{"wildcard resource", func(d *Document) { d.Rules[0].ResourceID = "*" }},
		{"bad unused context", func(d *Document) {
			d.Subjects[1].Memberships = []Membership{{RoleID: "member", Context: map[string]string{"world": "fixture\x00secret"}}}
		}},
		{"contradictory rule time", func(d *Document) { d.Rules[0].NotBefore, d.Rules[0].ExpiresAt = fixtureNow, fixtureNow }},
		{"reversed membership time", func(d *Document) {
			d.Subjects[0].Memberships[0].NotBefore, d.Subjects[0].Memberships[0].ExpiresAt = fixtureNow, fixtureNow.Add(-time.Second)
		}},
		{"out of range time", func(d *Document) { d.Rules[0].NotBefore = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
	}
	good := mustCompile(t, fixtureDocument())
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := fixtureDocument()
			tc.change(&d)
			s, err := Compile(d)
			if s != nil || !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("invalid document produced snapshot=%v error=%v", s != nil, err)
			}
			if strings.Contains(err.Error(), "fixture\x00secret") {
				t.Fatal("validation error leaked a context value")
			}
		})
	}
	assertDecision(t, good.Evaluate(requestFor(good)), true, "explicit_allow")
}

func TestDocumentBoundsAndExactDepthLimit(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Document)
	}{
		{"roles", func(d *Document) { d.Roles = make([]Role, MaxRoles+1) }},
		{"subjects", func(d *Document) { d.Subjects = make([]Subject, MaxSubjects+1) }},
		{"rules", func(d *Document) { d.Rules = make([]Rule, MaxRules+1) }},
		{"parents", func(d *Document) { d.Roles[0].Parents = make([]string, MaxParents+1) }},
		{"memberships", func(d *Document) { d.Subjects[0].Memberships = make([]Membership, MaxMemberships+1) }},
		{"identifier", func(d *Document) { d.Rules[0].ID = strings.Repeat("a", MaxIdentifier+1) }},
		{"context values", func(d *Document) { d.Rules[0].Context["world"] = strings.Repeat("a", MaxContextValue+1) }},
		{"context pairs", func(d *Document) {
			for i := range MaxContextPairs + 1 {
				d.Rules[0].Context[fmt.Sprintf("key%d", i)] = "value"
			}
		}},
		{"document bytes", func(d *Document) {
			original := d.Rules[0]
			d.Rules = make([]Rule, MaxRules)
			for i := range d.Rules {
				d.Rules[i] = original
				d.Rules[i].ID = fmt.Sprintf("rule%d", i)
				d.Rules[i].Context = make(map[string]string)
				for key := range MaxContextPairs {
					d.Rules[i].Context[fmt.Sprintf("key%d", key)] = strings.Repeat("x", MaxContextValue)
				}
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := fixtureDocument()
			tc.change(&d)
			if s, err := Compile(d); s != nil || !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("bound accepted: snapshot=%v error=%v", s != nil, err)
			}
		})
	}
	d := fixtureDocument()
	d.Roles = make([]Role, MaxDepth)
	for i := range d.Roles {
		d.Roles[i].ID = fmt.Sprintf("role%02d", i)
		if i+1 < MaxDepth {
			d.Roles[i].Parents = []string{fmt.Sprintf("role%02d", i+1)}
		}
	}
	d.Subjects[0].Memberships[0].RoleID = d.Roles[0].ID
	d.Rules[0].RoleID = d.Roles[MaxDepth-1].ID
	s := mustCompile(t, d)
	got := s.Evaluate(requestFor(s))
	if !got.Allowed || len(got.InheritancePath) != MaxDepth {
		t.Fatalf("exact depth limit rejected: %+v", got)
	}
	d.Roles[MaxDepth-1].Parents = []string{"one-more"}
	d.Roles = append(d.Roles, Role{ID: "one-more"})
	if _, err := Compile(d); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("excess depth accepted: %v", err)
	}
}

func TestLastMatchingDenyAtRuleLimitStillWins(t *testing.T) {
	d := fixtureDocument()
	original := d.Rules[0]
	d.Rules = make([]Rule, MaxRules)
	for i := range d.Rules {
		d.Rules[i] = original
		d.Rules[i].ID = fmt.Sprintf("rule%04d", i)
	}
	d.Rules[MaxRules-1].Effect = Deny
	s := mustCompile(t, d)
	got := s.Evaluate(requestFor(s))
	assertDecision(t, got, false, "explicit_deny")
	if got.RuleID != d.Rules[MaxRules-1].ID {
		t.Fatalf("late deny was not evaluated: %+v", got)
	}
}

func TestCanonicalVersionIgnoresOrderingAndTimeZone(t *testing.T) {
	d := fixtureDocument()
	d.Roles = append(d.Roles, Role{ID: "helper", Parents: []string{"member"}})
	d.Roles[1].Parents = append(d.Roles[1].Parents, "helper")
	d.Subjects[0].Memberships = append(d.Subjects[0].Memberships, Membership{RoleID: "helper", NotBefore: fixtureNow})
	d.Rules[0].NotBefore = fixtureNow
	extra := d.Rules[0]
	extra.ID, extra.ResourceID = "other.rule", "other"
	d.Rules = append(d.Rules, extra)
	want := mustCompile(t, d).Version()
	random := rand.New(rand.NewSource(42))
	for range 30 {
		encoded, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var permuted Document
		if err := json.Unmarshal(encoded, &permuted); err != nil {
			t.Fatal(err)
		}
		random.Shuffle(len(permuted.Roles), func(i, j int) { permuted.Roles[i], permuted.Roles[j] = permuted.Roles[j], permuted.Roles[i] })
		random.Shuffle(len(permuted.Subjects), func(i, j int) {
			permuted.Subjects[i], permuted.Subjects[j] = permuted.Subjects[j], permuted.Subjects[i]
		})
		random.Shuffle(len(permuted.Rules), func(i, j int) { permuted.Rules[i], permuted.Rules[j] = permuted.Rules[j], permuted.Rules[i] })
		for i := range permuted.Roles {
			slices.Reverse(permuted.Roles[i].Parents)
		}
		for i := range permuted.Subjects {
			slices.Reverse(permuted.Subjects[i].Memberships)
		}
		for i := range permuted.Rules {
			permuted.Rules[i].NotBefore = permuted.Rules[i].NotBefore.In(time.FixedZone("fixture", 3600))
		}
		if got := mustCompile(t, permuted).Version(); got != want {
			t.Fatalf("order/time-zone changed version: got %+v want %+v", got, want)
		}
	}
	// Nil and explicitly empty collection spellings carry the same semantics.
	empty := Document{Schema: 1, Epoch: "empty", Revision: 1, TenantID: "metallic"}
	first := mustCompile(t, empty).Version()
	empty.Roles, empty.Subjects, empty.Rules = []Role{}, []Subject{}, []Rule{}
	if mustCompile(t, empty).Version() != first {
		t.Fatal("empty collection spellings changed digest")
	}
}

func TestSnapshotAndReturnedWitnessAreDetached(t *testing.T) {
	d := fixtureDocument()
	s := mustCompile(t, d)
	r := requestFor(s)
	before := s.Evaluate(r)
	d.Roles[1].Parents[0] = "missing"
	d.Subjects[0].Memberships[0].Context["server"] = "other"
	d.Subjects[0].Memberships[0].RoleID = "missing"
	d.Rules[0].Context["world"] = "other"
	d.Rules[0].Effect = Deny
	d.Rules[0].ExpiresAt = fixtureNow.Add(-time.Hour)
	d.Epoch = "mutated"
	changedWitness := s.Evaluate(r)
	changedWitness.InheritancePath[0] = "mutated"
	changedVersion := s.Version()
	changedVersion.Digest[0] ^= 1
	if got := s.Evaluate(r); !reflect.DeepEqual(got, before) || s.Version() != r.Expected {
		t.Fatalf("external mutation reached snapshot: %+v", got)
	}
}

func TestConcurrentSnapshotsAndEvaluations(t *testing.T) {
	d := fixtureDocument()
	s := mustCompile(t, d)
	r := requestFor(s)
	want := s.Evaluate(r)
	var group sync.WaitGroup
	for worker := range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 100 {
				if worker%4 == 0 {
					other, err := Compile(d)
					if err != nil || other.Version() != s.Version() {
						t.Errorf("concurrent compile differs: %v", err)
						return
					}
				}
				got := s.Evaluate(r)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("concurrent evaluation differs: %+v", got)
					return
				}
				got.InheritancePath[0] = "caller-owned"
			}
		}()
	}
	group.Wait()
}

func FuzzExactContextHasNoPatternExpansion(f *testing.F) {
	for _, value := range []string{"main", "*", "world.*", "大地", "fixture\nvalue", "", string([]byte{0xff})} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		d := fixtureDocument()
		d.Rules[0].Context["world"] = value
		s, err := Compile(d)
		if err != nil {
			if !errors.Is(err, ErrInvalidDocument) || s != nil {
				t.Fatalf("unexpected compile result: %v", err)
			}
			return
		}
		r := requestFor(s)
		r.Context["world"] = value
		if !s.Evaluate(r).Allowed {
			t.Fatal("identical valid literal failed")
		}
		r.Context["world"] = value + "x"
		if s.Evaluate(r).Allowed {
			t.Fatal("a nonidentical literal expanded as a pattern")
		}
	})
}

var benchmarkDecision Decision
var benchmarkSnapshot *Snapshot

// These are local microbenchmarks, not gateway, JVM, client, or network results.
func BenchmarkEvaluateInherited(b *testing.B) {
	s := mustCompile(b, fixtureDocument())
	r := requestFor(s)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkDecision = s.Evaluate(r)
	}
}

func BenchmarkCompileSmall(b *testing.B) {
	d := fixtureDocument()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		benchmarkSnapshot, err = Compile(d)
		if err != nil {
			b.Fatal(err)
		}
	}
}
