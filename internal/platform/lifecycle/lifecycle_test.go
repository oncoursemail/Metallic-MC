package lifecycle

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture() TrustedState {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	s := TrustedState{
		ServerID: "server-1", OwnerID: "owner@example.test", AuthorityEpoch: "authority-1", Revision: 7, Generation: 2,
		Location: Hosted, Visibility: Public, Phase: Stopped, RequiredPolicyRevision: "rules-1", AcceptedPolicyRevision: "rules-1",
		ManagedProfileDigest: strings.Repeat("a", 64), CompatibilityProfileDigest: strings.Repeat("b", 64), Now: now,
	}
	for _, kind := range []AttestationKind{ManagedProfile, RuntimeReady, CompatibilityReady, GatewayReady, ModerationClear} {
		d := strings.Repeat("c", 64)
		if kind == ManagedProfile || kind == RuntimeReady {
			d = s.ManagedProfileDigest
		}
		if kind == CompatibilityReady {
			d = s.CompatibilityProfileDigest
		}
		s.Attestations = append(s.Attestations, Attestation{Kind: kind, ServerID: s.ServerID, AuthorityEpoch: s.AuthorityEpoch, ServerRevision: s.Revision, Generation: s.Generation, IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), Digest: d})
	}
	return s
}

func request(s TrustedState, a Action) Request {
	return Request{Principal: Principal{ID: s.OwnerID, Kind: Owner}, ServerID: s.ServerID, Action: a, RequestID: "operation-1", ExpectedRevision: s.Revision}
}

func plan(t *testing.T, s TrustedState, r Request) Decision {
	t.Helper()
	e, err := NewEvidence(s)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Plan(r, e)
	if err != nil {
		t.Fatal(err)
	}
	if d.ExecutionEnabled {
		t.Fatal("planner enabled execution")
	}
	if !d.Allowed && len(d.Steps) != 0 {
		t.Fatal("denied plan returned executable steps")
	}
	return d
}

func blocked(d Decision, code string) bool {
	for _, b := range d.Blockers {
		if b.Code == code {
			return true
		}
	}
	return false
}

func index(d Decision, step Step) int {
	for i, s := range d.Steps {
		if s == step {
			return i
		}
	}
	return -1
}

func TestHostedStartNeverAdmitsBeforeReadiness(t *testing.T) {
	s := fixture()
	d := plan(t, s, request(s, Start))
	if !d.Allowed || d.Scope != ManagedRuntime || d.NextGeneration != 3 || d.RuntimeGeneration != 2 || d.AuthorityEpoch != s.AuthorityEpoch {
		t.Fatalf("unexpected start: %+v", d)
	}
	if index(d, BootIsolated) < 0 || index(d, AwaitReadiness) < 0 || index(d, OpenPublicRoute) >= 0 || index(d, PublishListing) >= 0 {
		t.Fatalf("start must boot without admitting: %+v", d)
	}
	if d := plan(t, s, request(s, Admit)); !blocked(d, "runtime_not_running") {
		t.Fatalf("stopped server was admitted: %+v", d)
	}
	s.Phase = Running
	d = plan(t, s, request(s, Admit))
	if !d.Allowed || !reflect.DeepEqual(d.Steps, []Step{OpenPublicRoute, PublishListing}) {
		t.Fatalf("ready public admission: %+v", d)
	}
	s.Visibility = Private
	s.Attestations = s.Attestations[:4]
	d = plan(t, s, request(s, Admit))
	if !d.Allowed || !reflect.DeepEqual(d.Steps, []Step{OpenPrivateRoute}) {
		t.Fatalf("private hosted route: %+v", d)
	}
}

func TestUnprovisionedAllocationFailsClosed(t *testing.T) {
	s := TrustedState{ServerID: "allocation-1", OwnerID: "admin", AuthorityEpoch: "preview-1", Revision: 1, Location: Hosted, Visibility: Private, Phase: Unprovisioned, Now: fixture().Now}
	d := plan(t, s, request(s, Start))
	if d.Allowed || !blocked(d, "policy_requirement_unavailable") || !blocked(d, "managed_profile_evidence_required") || !blocked(d, "compatibility_profile_missing") {
		t.Fatalf("unprovisioned allocation pretended to be ready: %+v", d)
	}
}

func TestOwnerAndModeratorScopes(t *testing.T) {
	for _, a := range []Action{Start, Stop, Restart, Hibernate, Export, Delete, Suspend, Reinstate, Admit, Unpublish} {
		t.Run(string(a), func(t *testing.T) {
			s := fixture()
			r := request(s, a)
			r.Principal.ID = "other-owner"
			d := plan(t, s, r)
			if d.Allowed || !blocked(d, "not_authorized") || d.AuthorityEpoch != "" || d.RuntimeGeneration != 0 || !d.EvaluatedAt.IsZero() {
				t.Fatalf("foreign owner leaked state or gained control: %+v", d)
			}
			r.Principal = Principal{ID: "moderator-1", Kind: PlatformModerator}
			s.Suspended = a == Reinstate
			d = plan(t, s, r)
			if a != Suspend && a != Reinstate && !blocked(d, "not_authorized") {
				t.Fatalf("moderator gained owner capabilities: %+v", d)
			}
		})
	}
	s := fixture()
	for _, a := range []Action{Suspend, Reinstate} {
		if d := plan(t, s, request(s, a)); !blocked(d, "not_authorized") {
			t.Fatalf("owner gained moderation: %+v", d)
		}
	}
	r := request(s, Export)
	r.ServerID = "foreign-server"
	if d := plan(t, s, r); !blocked(d, "not_authorized") {
		t.Fatalf("wrong resource accepted: %+v", d)
	}
}

func TestLocalPrivateHasNoPlatformEnforcement(t *testing.T) {
	s := fixture()
	s.Location, s.Visibility = Local, Private
	s.Attestations = nil
	s.RequiredPolicyRevision, s.AcceptedPolicyRevision = "", ""
	s.ManagedProfileDigest, s.CompatibilityProfileDigest = "", ""
	for _, tc := range []struct {
		a     Action
		phase Phase
		step  Step
	}{
		{Start, Stopped, LocalStart}, {Stop, Running, LocalStop}, {Restart, Running, LocalRestart},
		{Hibernate, Running, LocalHibernate}, {Export, Stopped, LocalExport}, {Delete, Stopped, LocalTrash},
	} {
		s.Phase = tc.phase
		d := plan(t, s, request(s, tc.a))
		if !d.Allowed || d.Scope != LocalLauncher || !reflect.DeepEqual(d.Steps, []Step{tc.step}) {
			t.Fatalf("local private %s was cloud-enforced: %+v", tc.a, d)
		}
	}
	for _, a := range []Action{Suspend, Reinstate} {
		r := request(s, a)
		r.Principal = Principal{ID: "moderator-1", Kind: PlatformModerator}
		if d := plan(t, s, r); !blocked(d, "not_authorized") {
			t.Fatalf("moderator can control local private runtime: %+v", d)
		}
	}
	if d := plan(t, s, request(s, Admit)); !blocked(d, "not_authorized") {
		t.Fatal("local private server admitted")
	}
}

func TestLocalPublicSuspensionOnlyRevokesPlatformAccess(t *testing.T) {
	s := fixture()
	s.Location, s.Phase = Local, Running
	r := request(s, Suspend)
	r.Principal = Principal{ID: "moderator-1", Kind: PlatformModerator}
	d := plan(t, s, r)
	if !d.Allowed || d.Scope != PlatformRoute || index(d, RevokeTunnel) < 0 || index(d, RemoveListing) < 0 {
		t.Fatalf("missing tunnel revocation: %+v", d)
	}
	for _, forbidden := range []Step{StopRuntime, LocalStop, LocalTrash, QuarantineData, AdvanceGeneration, CheckpointWorld} {
		if index(d, forbidden) >= 0 {
			t.Fatalf("moderation touched local runtime: %+v", d)
		}
	}
	s.ManagedProfileDigest = ""
	s.Attestations = s.Attestations[2:]
	if d := plan(t, s, request(s, Admit)); !d.Allowed {
		t.Fatalf("tunnel should not claim managed runtime attestation: %+v", d)
	}
	s.Suspended = true
	if d := plan(t, s, request(s, Admit)); !blocked(d, "server_suspended") {
		t.Fatal("suspended tunnel re-admitted")
	}
}

func TestSuspensionCannotBeBypassedAndReinstateDoesNotStart(t *testing.T) {
	s := fixture()
	s.Suspended = true
	for _, a := range []Action{Start, Restart, Admit} {
		if d := plan(t, s, request(s, a)); !blocked(d, "server_suspended") {
			t.Fatalf("suspension bypass through %s: %+v", a, d)
		}
	}
	r := request(s, Reinstate)
	r.Principal = Principal{ID: "moderator-1", Kind: PlatformModerator}
	d := plan(t, s, r)
	if !d.Allowed || !reflect.DeepEqual(d.Steps, []Step{ClearSuspension, KeepAdmissionClosed}) {
		t.Fatalf("reinstate opened runtime or route: %+v", d)
	}
	if d := plan(t, s, request(s, Export)); !d.Allowed || index(d, BootIsolated) >= 0 {
		t.Fatalf("snapshot export restarted suspended runtime: %+v", d)
	}
}

func TestAdmissionFactsAreBoundAndExpire(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*TrustedState)
	}{
		{"missing", func(s *TrustedState) { s.Attestations = nil }},
		{"expired_at_boundary", func(s *TrustedState) { s.Now = s.Attestations[0].ExpiresAt }},
		{"future", func(s *TrustedState) { s.Now = s.Attestations[0].IssuedAt.Add(-time.Nanosecond) }},
		{"foreign_server", func(s *TrustedState) {
			for i := range s.Attestations {
				s.Attestations[i].ServerID = "server-2"
			}
		}},
		{"old_epoch", func(s *TrustedState) { s.AuthorityEpoch = "restored-authority-2" }},
		{"old_revision", func(s *TrustedState) { s.Revision++ }},
		{"old_generation", func(s *TrustedState) { s.Generation++ }},
		{"wrong_runtime", func(s *TrustedState) { s.ManagedProfileDigest = strings.Repeat("d", 64) }},
		{"wrong_compatibility", func(s *TrustedState) { s.CompatibilityProfileDigest = strings.Repeat("d", 64) }},
		{"new_policy", func(s *TrustedState) { s.RequiredPolicyRevision = "rules-2" }},
		{"missing_moderation", func(s *TrustedState) { s.Attestations = s.Attestations[:4] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			s.Phase = Running
			tc.change(&s)
			if d := plan(t, s, request(s, Admit)); d.Allowed || len(d.Blockers) == 0 {
				t.Fatalf("untrusted/stale facts accepted: %+v", d)
			}
		})
	}
}

func TestDeletionQuarantinesAndHibernateStopsBeforeSnapshot(t *testing.T) {
	s := fixture()
	s.Phase = Running
	d := plan(t, s, request(s, Delete))
	if !d.Allowed || d.Steps[0] != MarkTombstone || d.Steps[len(d.Steps)-1] != QuarantineData || index(d, InvalidateCredentials) < 0 || index(d, IsolateRuntimeNetwork) < 0 {
		t.Fatalf("delete must revoke and quarantine: %+v", d)
	}
	d = plan(t, s, request(s, Hibernate))
	if !d.Allowed || index(d, VerifyWriterExited) >= index(d, CheckpointWorld) || index(d, VerifyCheckpoint) >= index(d, ReleaseCompute) || index(d, CheckpointWorld) < 0 {
		t.Fatalf("unsafe hibernation order: %+v", d)
	}
	s.Tombstoned = true
	if d := plan(t, s, request(s, Start)); !blocked(d, "server_deleted") {
		t.Fatal("tombstoned server started")
	}
}

func TestFencesAndUnknownState(t *testing.T) {
	s := fixture()
	r := request(s, Start)
	r.ExpectedRevision--
	if d := plan(t, s, r); !blocked(d, "revision_conflict") {
		t.Fatal("stale operation accepted")
	}
	s.Generation = math.MaxUint64
	if d := plan(t, s, request(s, Delete)); !blocked(d, "generation_exhausted") {
		t.Fatal("generation wrapped")
	}
	s = fixture()
	s.Revision = math.MaxUint64
	if d := plan(t, s, request(s, Delete)); !blocked(d, "revision_exhausted") {
		t.Fatal("revision wrapped")
	}
	s = fixture()
	s.Phase = Unknown
	for _, a := range []Action{Start, Stop, Restart, Hibernate, Admit} {
		if d := plan(t, s, request(s, a)); d.Allowed {
			t.Fatalf("unknown runtime allowed %s", a)
		}
	}
}

func TestMalformedEvidenceAndRequestsFail(t *testing.T) {
	for _, change := range []func(*TrustedState){
		func(s *TrustedState) { s.ServerID = "../../other" }, func(s *TrustedState) { s.OwnerID = "owner\x00" },
		func(s *TrustedState) { s.Now = time.Time{} }, func(s *TrustedState) { s.Revision = 0 },
		func(s *TrustedState) { s.Phase = "pretend_running" }, func(s *TrustedState) { s.Location = "shell" },
		func(s *TrustedState) { s.Visibility = "listed_maybe" }, func(s *TrustedState) { s.ManagedProfileDigest = "not-a-hash" },
		func(s *TrustedState) { s.Attestations = append(s.Attestations, s.Attestations[0]) },
		func(s *TrustedState) { s.Attestations[0].ExpiresAt = s.Attestations[0].IssuedAt.Add(6 * time.Minute) },
		func(s *TrustedState) { s.Attestations[0].Kind = "tenant_says_safe" },
		func(s *TrustedState) { s.Phase, s.Generation = Running, 0 },
	} {
		s := fixture()
		change(&s)
		if _, err := NewEvidence(s); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("malformed evidence accepted: %v", err)
		}
	}
	s := fixture()
	e, _ := NewEvidence(s)
	for _, change := range []func(*Request){
		func(r *Request) { r.Action = "exec" }, func(r *Request) { r.Principal.Kind = "root" },
		func(r *Request) { r.ServerID = "/etc/passwd" }, func(r *Request) { r.Principal.ID = strings.Repeat("x", 129) },
		func(r *Request) { r.ExpectedRevision = 0 }, func(r *Request) { r.RequestID = "id\ncommand" },
	} {
		r := request(s, Start)
		change(&r)
		if err := ValidateRequest(r); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("request validator accepted malformed intent: %v", err)
		}
		if _, err := Plan(r, e); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("malformed request accepted: %v", err)
		}
	}
	if _, err := Plan(request(s, Start), Evidence{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("zero evidence accepted")
	}
	var decoded Evidence
	if err := json.Unmarshal([]byte(`{"valid":true,"state":{"Location":"hosted"}}`), &decoded); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("JSON populated trusted evidence")
	}
}

func TestEvidenceCopiesAndConcurrentDecisions(t *testing.T) {
	s := fixture()
	e, err := NewEvidence(s)
	if err != nil {
		t.Fatal(err)
	}
	r := request(s, Start)
	s.Attestations[0].Digest = strings.Repeat("d", 64)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				d, err := Plan(r, e)
				if err != nil || !d.Allowed || d.ExecutionEnabled {
					t.Errorf("immutable plan: %+v %v", d, err)
					return
				}
				d.Steps[0] = "caller_mutation"
			}
		}()
	}
	wg.Wait()
	d, _ := Plan(r, e)
	if d.Steps[0] != KeepAdmissionClosed {
		t.Fatal("decision alias mutated stored evidence")
	}
}

func TestFailedHostedStartContainsOldWriterBeforeReplacement(t *testing.T) {
	s := fixture()
	s.Phase = Failed
	d := plan(t, s, request(s, Start))
	if !d.Allowed {
		t.Fatalf("failed start recovery proposal denied: %+v", d)
	}
	previous := -1
	for _, step := range []Step{RevokeAdmission, IsolateRuntimeNetwork, StopRuntime, VerifyWriterExited, CheckpointWorld, VerifyCheckpoint, AdvanceGeneration, PrepareManaged, BootIsolated, AwaitReadiness} {
		position := index(d, step)
		if position <= previous {
			t.Fatalf("failed writer containment missing or out of order at %s: %+v", step, d)
		}
		previous = position
	}
	if index(d, OpenPublicRoute) >= 0 || index(d, PublishListing) >= 0 || d.NextGeneration != s.Generation+1 {
		t.Fatalf("failed recovery admitted traffic or reused generation: %+v", d)
	}
}

func TestValidateRequestDoesNotRequireOrAuthorizeEvidence(t *testing.T) {
	s := fixture()
	r := request(s, Start)
	if err := ValidateRequest(r); err != nil {
		t.Fatalf("valid request shape denied: %v", err)
	}
	r.Principal.ID = "foreign-owner"
	if err := ValidateRequest(r); err != nil {
		t.Fatalf("shape validation requires server authorization: %v", err)
	}
	if d := plan(t, s, r); !blocked(d, "not_authorized") {
		t.Fatalf("shape validation acted as authorization: %+v", d)
	}
}

func TestEvaluatedAtIsCapturedEvidenceTimeNotExecutionLease(t *testing.T) {
	s := fixture()
	s.Phase = Running
	e, err := NewEvidence(s)
	if err != nil {
		t.Fatal(err)
	}
	r := request(s, Admit)
	d, err := Plan(r, e)
	if err != nil || !d.Allowed || !d.EvaluatedAt.Equal(s.Now) {
		t.Fatalf("missing captured evaluation time: %+v %v", d, err)
	}
	captured := s.Now
	s.Now = s.Now.Add(2 * time.Minute)
	freshEvaluation := plan(t, s, r)
	if freshEvaluation.Allowed || !blocked(freshEvaluation, "gateway_ready_evidence_required") || !freshEvaluation.EvaluatedAt.Equal(s.Now) {
		t.Fatalf("fresh evaluation accepted expired observations: %+v", freshEvaluation)
	}
	repeated, err := Plan(r, e)
	if err != nil || !repeated.Allowed || !repeated.EvaluatedAt.Equal(captured) || repeated.ExecutionEnabled {
		t.Fatalf("historical evidence was silently advanced or enabled: %+v %v", repeated, err)
	}
	r.Principal.ID = "foreign-owner"
	denied, err := Plan(r, e)
	if err != nil || !blocked(denied, "not_authorized") || !denied.EvaluatedAt.IsZero() {
		t.Fatalf("foreign owner received captured trusted time: %+v %v", denied, err)
	}
}
