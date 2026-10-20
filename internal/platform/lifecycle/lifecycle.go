// Package lifecycle plans owner-scoped server operations without executing them.
// Authoritative state and attestations must come from trusted controller adapters,
// never from tenant request JSON. A plan is not a reusable execution capability.
package lifecycle

import (
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidInput = errors.New("invalid lifecycle input")

type PrincipalKind string

const (
	Owner             PrincipalKind = "owner"
	PlatformModerator PrincipalKind = "platform_moderator"
)

type Principal struct {
	ID   string
	Kind PrincipalKind
}

type Location string
type Visibility string
type Phase string
type Action string
type AttestationKind string
type Scope string
type Step string

const (
	Local   Location   = "local"
	Hosted  Location   = "hosted"
	Private Visibility = "private"
	Public  Visibility = "public"

	Unknown       Phase = "unknown"
	Unprovisioned Phase = "unprovisioned"
	Stopped       Phase = "stopped"
	Running       Phase = "running"
	Hibernated    Phase = "hibernated"
	Starting      Phase = "starting"
	Stopping      Phase = "stopping"
	Failed        Phase = "failed"

	Start     Action = "start"
	Stop      Action = "stop"
	Restart   Action = "restart"
	Hibernate Action = "hibernate"
	Export    Action = "export"
	Delete    Action = "delete"
	Suspend   Action = "suspend"
	Reinstate Action = "reinstate"
	Admit     Action = "admit"
	Unpublish Action = "unpublish"

	ManagedProfile     AttestationKind = "managed_profile"
	RuntimeReady       AttestationKind = "runtime_ready"
	CompatibilityReady AttestationKind = "compatibility_ready"
	GatewayReady       AttestationKind = "gateway_ready"
	ModerationClear    AttestationKind = "moderation_clear"

	LocalLauncher  Scope = "local_launcher"
	ManagedRuntime Scope = "managed_runtime"
	PlatformRoute  Scope = "platform_route"
)

const (
	KeepAdmissionClosed   Step = "keep_admission_closed"
	RevokeAdmission       Step = "revoke_admission"
	RemoveListing         Step = "remove_listing"
	RevokeTunnel          Step = "revoke_tunnel"
	InvalidateCredentials Step = "invalidate_credentials"
	IsolateRuntimeNetwork Step = "isolate_runtime_network"
	AdvanceGeneration     Step = "advance_runtime_generation"
	PrepareManaged        Step = "prepare_approved_managed_profile"
	BootIsolated          Step = "boot_isolated_runtime"
	AwaitReadiness        Step = "await_fresh_readiness_evidence"
	DrainPlayers          Step = "drain_players"
	CheckpointWorld       Step = "checkpoint_world"
	StopRuntime           Step = "stop_runtime"
	VerifyWriterExited    Step = "verify_writer_exited"
	VerifyCheckpoint      Step = "verify_durable_checkpoint"
	ReleaseCompute        Step = "release_compute"
	OpenPrivateRoute      Step = "open_private_allowlist_route"
	OpenPublicRoute       Step = "open_public_route"
	PublishListing        Step = "publish_approved_listing"
	MarkSuspended         Step = "mark_suspended"
	ClearSuspension       Step = "clear_suspension"
	MarkTombstone         Step = "mark_tombstone"
	QuarantineData        Step = "quarantine_retained_data"
	ExportSnapshot        Step = "export_owner_snapshot_without_platform_secrets"
	LocalStart            Step = "delegate_start_to_local_launcher"
	LocalStop             Step = "delegate_stop_to_local_launcher"
	LocalRestart          Step = "delegate_restart_to_local_launcher"
	LocalHibernate        Step = "delegate_checkpoint_stop_to_local_launcher"
	LocalExport           Step = "delegate_export_to_local_launcher"
	LocalTrash            Step = "delegate_trash_to_local_launcher"
	ObserveStopped        Step = "observe_already_stopped"
)

// Request contains operation intent. The adapter supplies Principal from its
// authenticated context; a public handler must not decode a Principal from JSON.
type Request struct {
	Principal        Principal
	ServerID         string
	Action           Action
	RequestID        string
	ExpectedRevision uint64
}

// Attestation is a fact whose authenticity the trusted adapter already verified.
// Digest identifies the profile/proof; it is not itself a signature. Every fact
// is bound to an authority epoch, server revision and runtime generation.
type Attestation struct {
	Kind           AttestationKind
	ServerID       string
	AuthorityEpoch string
	ServerRevision uint64
	Generation     uint64
	IssuedAt       time.Time
	ExpiresAt      time.Time
	Digest         string
}

// TrustedState must be constructed from authoritative records and verified node,
// gateway and moderation observations. It is not an HTTP request DTO.
type TrustedState struct {
	ServerID                   string
	OwnerID                    string
	AuthorityEpoch             string
	Revision                   uint64
	Generation                 uint64
	Location                   Location
	Visibility                 Visibility
	Phase                      Phase
	Suspended                  bool
	Tombstoned                 bool
	RequiredPolicyRevision     string
	AcceptedPolicyRevision     string
	ManagedProfileDigest       string
	CompatibilityProfileDigest string
	Now                        time.Time
	Attestations               []Attestation
}

// Evidence cannot be populated by encoding/json. Its zero value is invalid.
// NewEvidence takes a defensive copy, making it safe for concurrent planning.
type Evidence struct {
	state TrustedState
	valid bool
}

func (*Evidence) UnmarshalJSON([]byte) error { return ErrInvalidInput }

func NewEvidence(s TrustedState) (Evidence, error) {
	if !identifier(s.ServerID, 64) || !identityText(s.OwnerID) || !identifier(s.AuthorityEpoch, 128) || s.Revision == 0 || !validTime(s.Now) {
		return Evidence{}, ErrInvalidInput
	}
	if (s.Location != Local && s.Location != Hosted) || (s.Visibility != Private && s.Visibility != Public) || !validPhase(s.Phase) {
		return Evidence{}, ErrInvalidInput
	}
	if (s.Phase == Running || s.Phase == Starting || s.Phase == Stopping || s.Phase == Hibernated) && s.Generation == 0 {
		return Evidence{}, ErrInvalidInput
	}
	if s.Location == Local && s.Visibility == Private && s.Suspended {
		return Evidence{}, ErrInvalidInput
	}
	if !optionalIdentifier(s.RequiredPolicyRevision) || !optionalIdentifier(s.AcceptedPolicyRevision) || !optionalDigest(s.ManagedProfileDigest) || !optionalDigest(s.CompatibilityProfileDigest) || len(s.Attestations) > 5 {
		return Evidence{}, ErrInvalidInput
	}
	seen := make(map[AttestationKind]bool)
	for _, a := range s.Attestations {
		if !validKind(a.Kind) || seen[a.Kind] || !identifier(a.ServerID, 64) || !identifier(a.AuthorityEpoch, 128) || a.ServerRevision == 0 || !digest(a.Digest) || !validTime(a.IssuedAt) || !validTime(a.ExpiresAt) || !a.ExpiresAt.After(a.IssuedAt) || a.ExpiresAt.Sub(a.IssuedAt) > 5*time.Minute {
			return Evidence{}, ErrInvalidInput
		}
		seen[a.Kind] = true
	}
	s.Attestations = append([]Attestation(nil), s.Attestations...)
	return Evidence{state: s, valid: true}, nil
}

type Blocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Decision struct {
	Schema            string    `json:"schema"`
	Allowed           bool      `json:"allowed"`
	ExecutionEnabled  bool      `json:"execution_enabled"`
	ServerID          string    `json:"server_id"`
	Action            Action    `json:"action"`
	RequestID         string    `json:"request_id"`
	Scope             Scope     `json:"scope"`
	AuthorityEpoch    string    `json:"authority_epoch"`
	ExpectedRevision  uint64    `json:"expected_revision"`
	RuntimeGeneration uint64    `json:"runtime_generation"`
	NextGeneration    uint64    `json:"next_generation"`
	EvaluatedAt       time.Time `json:"evaluated_at"`
	Steps             []Step    `json:"steps"`
	Blockers          []Blocker `json:"blockers"`
}

// ValidateRequest validates bounded operation intent without loading server
// state. Adapters should call it before fetching trusted evidence. This does
// not authenticate the principal or authorize access to the requested server.
func ValidateRequest(r Request) error {
	if !identityText(r.Principal.ID) || (r.Principal.Kind != Owner && r.Principal.Kind != PlatformModerator) || !identifier(r.ServerID, 64) || !identifier(r.RequestID, 128) || r.ExpectedRevision == 0 || !validAction(r.Action) {
		return ErrInvalidInput
	}
	return nil
}

// Plan returns a deterministic proposal. Allowed means the supplied facts permit
// the proposal; ExecutionEnabled is always false. The executor must later re-read
// policy, compare-and-swap the revision, enforce fencing and obtain fresh evidence.
// EvaluatedAt is Evidence's captured trusted time, not the current wall clock or
// an execution lease. Reusing Evidence repeats that historical evaluation.
func Plan(r Request, e Evidence) (Decision, error) {
	if err := ValidateRequest(r); err != nil {
		return Decision{}, err
	}
	if !e.valid {
		return Decision{}, ErrInvalidInput
	}
	s := e.state
	d := Decision{Schema: "metallic.lifecycle-plan/v1", ServerID: r.ServerID, Action: r.Action, RequestID: r.RequestID, ExpectedRevision: r.ExpectedRevision, Steps: []Step{}, Blockers: []Blocker{}}
	if r.ServerID != s.ServerID || !authorized(r, s) {
		d.block("not_authorized", "This principal cannot plan that operation for this server.")
		return d, nil
	}
	d.Scope = ManagedRuntime
	if s.Location == Local {
		d.Scope = LocalLauncher
	}
	if r.Action == Admit || r.Action == Unpublish || (s.Location == Local && (r.Action == Suspend || r.Action == Reinstate)) {
		d.Scope = PlatformRoute
	}
	d.AuthorityEpoch, d.RuntimeGeneration, d.NextGeneration = s.AuthorityEpoch, s.Generation, s.Generation
	d.EvaluatedAt = s.Now
	if r.ExpectedRevision != s.Revision {
		d.block("revision_conflict", "Refresh the server record before planning this operation.")
	}
	if s.Tombstoned {
		d.block("server_deleted", "The server is tombstoned; normal lifecycle operations are unavailable.")
	}
	if s.Revision == math.MaxUint64 && r.Action != Export {
		d.block("revision_exhausted", "A new authority epoch is required before another state change.")
	}
	if s.Suspended && (r.Action == Start || r.Action == Restart || r.Action == Admit) {
		d.block("server_suspended", "Suspension must be cleared by an authorized platform moderator.")
	}
	if len(d.Blockers) > 0 {
		return d, nil
	}
	var steps []Step
	switch r.Action {
	case Start, Restart:
		if (r.Action == Start && s.Phase != Stopped && s.Phase != Hibernated && s.Phase != Failed && s.Phase != Unprovisioned) || (r.Action == Restart && s.Phase != Running) {
			d.block("phase_not_ready", "The observed runtime phase does not permit this operation.")
		}
		if s.Location == Hosted {
			d.requirePolicy(s)
			d.requireProof(s, ManagedProfile, s.ManagedProfileDigest)
			if s.CompatibilityProfileDigest == "" {
				d.block("compatibility_profile_missing", "An approved compatibility profile must be selected.")
			}
			steps = []Step{KeepAdmissionClosed}
			if r.Action == Restart {
				steps = []Step{RevokeAdmission, RemoveListing, DrainPlayers, StopRuntime, VerifyWriterExited, CheckpointWorld, VerifyCheckpoint}
			} else if s.Phase == Failed {
				// Failed health or a partial restart does not prove the old
				// writer exited. Contain it and establish exit before reuse.
				steps = []Step{RevokeAdmission, RemoveListing, IsolateRuntimeNetwork, StopRuntime, VerifyWriterExited, CheckpointWorld, VerifyCheckpoint}
			}
			steps = append(steps, AdvanceGeneration, PrepareManaged, BootIsolated, AwaitReadiness)
			d.advance(s)
		} else {
			steps = localPrelude(s)
			if r.Action == Start {
				steps = append(steps, LocalStart)
			} else {
				steps = append(steps, LocalRestart)
			}
		}
	case Stop, Hibernate:
		if s.Phase == Unknown || (r.Action == Hibernate && s.Phase != Running && s.Phase != Stopped) {
			d.block("phase_not_ready", "The observed runtime phase does not permit this operation.")
		}
		if s.Location == Local {
			steps = localPrelude(s)
			if r.Action == Stop {
				steps = append(steps, LocalStop)
			} else {
				steps = append(steps, LocalHibernate)
			}
		} else if r.Action == Stop && (s.Phase == Stopped || s.Phase == Hibernated || s.Phase == Unprovisioned) {
			steps = []Step{RevokeAdmission, RemoveListing, ObserveStopped}
		} else {
			steps = []Step{RevokeAdmission, RemoveListing, DrainPlayers, StopRuntime, VerifyWriterExited, CheckpointWorld, VerifyCheckpoint, AdvanceGeneration}
			if r.Action == Hibernate {
				steps = append(steps, ReleaseCompute)
			}
			d.advance(s)
		}
	case Admit:
		if s.Phase != Running {
			d.block("runtime_not_running", "Admission requires an observed running instance.")
		}
		d.requirePolicy(s)
		d.requireProof(s, GatewayReady, "")
		d.requireProof(s, CompatibilityReady, s.CompatibilityProfileDigest)
		if s.Location == Hosted {
			d.requireProof(s, RuntimeReady, s.ManagedProfileDigest)
		}
		if s.Visibility == Public {
			d.requireProof(s, ModerationClear, "")
			steps = []Step{OpenPublicRoute, PublishListing}
		} else {
			steps = []Step{OpenPrivateRoute}
		}
	case Unpublish:
		steps = []Step{RevokeAdmission, RemoveListing}
		if s.Location == Local {
			steps = append(steps, RevokeTunnel)
		}
	case Suspend:
		steps = []Step{MarkSuspended, RevokeAdmission, RemoveListing, InvalidateCredentials}
		if s.Location == Hosted {
			steps = append(steps, AdvanceGeneration, IsolateRuntimeNetwork, StopRuntime, VerifyWriterExited, QuarantineData)
			d.advance(s)
		} else {
			steps = append(steps, RevokeTunnel)
		}
	case Reinstate:
		if !s.Suspended {
			d.block("not_suspended", "Only a suspended server can be reinstated.")
		}
		steps = []Step{ClearSuspension, KeepAdmissionClosed}
	case Delete:
		if s.Location == Hosted {
			steps = []Step{MarkTombstone, RevokeAdmission, RemoveListing, InvalidateCredentials, AdvanceGeneration, IsolateRuntimeNetwork, StopRuntime, VerifyWriterExited, QuarantineData}
			d.advance(s)
		} else {
			steps = append(localPrelude(s), LocalTrash)
		}
	case Export:
		if s.Location == Hosted {
			steps = []Step{ExportSnapshot}
		} else {
			steps = []Step{LocalExport}
		}
	}
	if len(d.Blockers) == 0 {
		d.Allowed = true
		d.Steps = steps
	}
	return d, nil
}

func authorized(r Request, s TrustedState) bool {
	participates := s.Location == Hosted || s.Visibility == Public
	if r.Principal.Kind == PlatformModerator {
		return participates && (r.Action == Suspend || r.Action == Reinstate)
	}
	if r.Principal.ID != s.OwnerID || r.Action == Suspend || r.Action == Reinstate {
		return false
	}
	return participates || (r.Action != Admit && r.Action != Unpublish)
}

func localPrelude(s TrustedState) []Step {
	if s.Visibility == Public {
		return []Step{RevokeAdmission, RemoveListing, RevokeTunnel}
	}
	return []Step{}
}

func (d *Decision) block(code, message string) {
	d.Blockers = append(d.Blockers, Blocker{Code: code, Message: message})
}

func (d *Decision) advance(s TrustedState) {
	if s.Generation == math.MaxUint64 {
		d.block("generation_exhausted", "A new authority epoch is required before another runtime generation.")
		return
	}
	d.NextGeneration = s.Generation + 1
}

func (d *Decision) requirePolicy(s TrustedState) {
	if s.RequiredPolicyRevision == "" {
		d.block("policy_requirement_unavailable", "The current platform rules revision is unavailable.")
	} else if s.AcceptedPolicyRevision != s.RequiredPolicyRevision {
		d.block("policy_acceptance_required", "The server owner must accept the current platform rules.")
	}
}

func (d *Decision) requireProof(s TrustedState, kind AttestationKind, expectedDigest string) {
	profileBound := kind == ManagedProfile || kind == RuntimeReady || kind == CompatibilityReady
	for _, a := range s.Attestations {
		if a.Kind == kind && a.ServerID == s.ServerID && a.AuthorityEpoch == s.AuthorityEpoch && a.ServerRevision == s.Revision && a.Generation == s.Generation && !s.Now.Before(a.IssuedAt) && s.Now.Before(a.ExpiresAt) && (!profileBound || (expectedDigest != "" && a.Digest == expectedDigest)) {
			return
		}
	}
	d.block(string(kind)+"_evidence_required", "A current trusted "+string(kind)+" observation is required.")
}

func identifier(v string, limit int) bool {
	if len(v) == 0 || len(v) > limit || !alphaNum(v[0]) {
		return false
	}
	for i := 1; i < len(v); i++ {
		if !alphaNum(v[i]) && v[i] != '-' && v[i] != '_' && v[i] != '.' && v[i] != ':' {
			return false
		}
	}
	return true
}

func alphaNum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
func identityText(v string) bool {
	if len(v) == 0 || len(v) > 128 || !utf8.ValidString(v) || strings.TrimSpace(v) != v {
		return false
	}
	for _, c := range v {
		if unicode.IsControl(c) || unicode.Is(unicode.Cf, c) {
			return false
		}
	}
	return true
}
func optionalIdentifier(v string) bool { return v == "" || identifier(v, 128) }
func optionalDigest(v string) bool     { return v == "" || digest(v) }
func digest(v string) bool {
	if len(v) != 64 || strings.ToLower(v) != v {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}
func validTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 1970 && t.Year() <= 9999 }
func validPhase(p Phase) bool {
	return p == Unknown || p == Unprovisioned || p == Stopped || p == Running || p == Hibernated || p == Starting || p == Stopping || p == Failed
}
func validKind(k AttestationKind) bool {
	return k == ManagedProfile || k == RuntimeReady || k == CompatibilityReady || k == GatewayReady || k == ModerationClear
}
func validAction(a Action) bool {
	return a == Start || a == Stop || a == Restart || a == Hibernate || a == Export || a == Delete || a == Suspend || a == Reinstate || a == Admit || a == Unpublish
}
