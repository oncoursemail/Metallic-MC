// metallic-core-lab exercises the original network core with deterministic,
// synthetic data. It opens no sockets, starts no servers, and accepts no logins.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/network/policy"
	"github.com/oncoursemail/metallic-mc/internal/network/presentation"
	"github.com/oncoursemail/metallic-mc/internal/network/session"
	"github.com/oncoursemail/metallic-mc/internal/network/wire"
)

type check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

type report struct {
	Schema            string   `json:"schema"`
	Simulation        bool     `json:"simulation"`
	ProductionReady   bool     `json:"productionReady"`
	Clock             string   `json:"clock"`
	Checks            []check  `json:"checks"`
	VisiblePlayerIDs  []string `json:"visiblePlayerIds"`
	PolicyRevision    uint64   `json:"policyRevision"`
	SessionGeneration uint64   `json:"sessionGeneration"`
	OwnerInstance     string   `json:"ownerInstance"`
	ContextRevision   uint64   `json:"contextRevision"`
	Realm             string   `json:"realm"`
	Error             string   `json:"error,omitempty"`
}

func (r *report) require(name string, ok bool) error {
	r.Checks = append(r.Checks, check{Name: name, Passed: ok})
	if !ok {
		return fmt.Errorf("offline invariant failed: %s", name)
	}
	return nil
}

func fixtureDocument(now time.Time) policy.Document {
	return policy.Document{
		Schema: policy.SchemaVersion, Epoch: "lab-epoch-v1", Revision: 1, TenantID: "metallic-mc",
		Roles: []policy.Role{{ID: "member"}, {ID: "moderator", Parents: []string{"member"}}},
		Subjects: []policy.Subject{{ID: "viewer", Memberships: []policy.Membership{
			{RoleID: "moderator", Context: map[string]string{"realm": "hub"}},
			{RoleID: "member", Context: map[string]string{"realm": "arena"}},
		}}},
		Rules: []policy.Rule{
			{ID: "chat-members", RoleID: "member", Action: "network.chat.send", ResourceType: "channel", ResourceID: "global", Effect: policy.Allow},
			{ID: "kick-moderators", RoleID: "moderator", Action: "command.execute", ResourceType: "command", ResourceID: "moderation.kick", Effect: policy.Allow},
			{ID: "kick-lockdown", RoleID: "moderator", Action: "command.execute", ResourceType: "command", ResourceID: "moderation.kick", Effect: policy.Deny, Context: map[string]string{"lockdown": "true"}},
			{ID: "see-builder", RoleID: "member", Action: presentation.VisibilityAction, ResourceType: "player", ResourceID: "builder", Effect: policy.Allow},
			{ID: "see-staff", RoleID: "member", Action: presentation.VisibilityAction, ResourceType: "player", ResourceID: "staff", Effect: policy.Allow},
			{ID: "scheduled-hide-staff", RoleID: "member", Action: presentation.VisibilityAction, ResourceType: "player", ResourceID: "staff", Effect: policy.Deny, NotBefore: now.Add(10 * time.Second)},
		},
	}
}

func simulate() (report, error) {
	r := report{Schema: "metallic.core-lab/v1", Simulation: true, ProductionReady: false, Clock: "fixed synthetic clock; no live measurements"}
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	doc := fixtureDocument(now)
	p1, err := policy.Compile(doc)
	if err != nil {
		return r, err
	}
	request := policy.Request{Expected: p1.Version(), TenantID: "metallic-mc", SubjectID: "viewer", Action: "network.chat.send", ResourceType: "channel", ResourceID: "global", Context: map[string]string{"realm": "hub", "lockdown": "false"}, Now: now}
	decision := p1.Evaluate(request)
	if err := r.require("inherited_allow_has_witness", decision.Allowed && slices.Equal(decision.InheritancePath, []string{"moderator", "member"})); err != nil {
		return r, err
	}
	kick := request
	kick.Action, kick.ResourceType, kick.ResourceID = "command.execute", "command", "moderation.kick"
	kick.Context = map[string]string{"realm": "hub", "lockdown": "true"}
	decision = p1.Evaluate(kick)
	if err := r.require("contextual_deny_overrides_allow", !decision.Allowed && decision.Code == "explicit_deny" && decision.RuleID == "kick-lockdown"); err != nil {
		return r, err
	}
	stale := request
	stale.Expected.Revision++
	if err := r.require("wrong_policy_version_denied", !p1.Evaluate(stale).Allowed && p1.Evaluate(stale).Code == "version_mismatch"); err != nil {
		return r, err
	}

	machine, err := session.New(doc.Epoch)
	if err != nil {
		return r, err
	}
	source, err := machine.Acquire(now, "viewer", "hub:boot1")
	if err != nil {
		return r, err
	}
	stamp := presentation.Stamp{AuthorityEpoch: source.AuthorityEpoch, ViewerID: source.PlayerID, InstanceID: source.InstanceID, Generation: source.Generation, ContextRevision: 1, Policy: p1.Version(), Revision: 1}
	candidates := []presentation.Candidate{{PlayerID: "builder", Name: "Builder", Prefix: "[Member]", Priority: 10}, {PlayerID: "staff", Name: "Staff", Prefix: "[Staff]", Priority: 100}}
	view1, err := presentation.Build(presentation.Input{Stamp: stamp, Policy: p1, Request: request, Candidates: candidates, Limit: 20})
	if err != nil {
		return r, err
	}
	rows := view1.Rows()
	if err := r.require("view_uses_session_and_policy_versions", len(rows) == 2 && rows[0].PlayerID == "staff" && view1.Stamp().Generation == source.Generation && view1.Stamp().Policy == p1.Version()); err != nil {
		return r, err
	}

	queuedStamp := stamp
	queuedStamp.Revision++
	queuedRequest := request
	queuedRequest.Now = now.Add(time.Second)
	queuedView, err := presentation.Build(presentation.Input{Stamp: queuedStamp, Policy: p1, Request: queuedRequest, Candidates: candidates, Limit: 20})
	if err != nil {
		return r, err
	}
	queued, err := presentation.Diff(view1, queuedView)
	if err != nil {
		return r, err
	}
	_, applyErr := presentation.Apply(view1, queued, now.Add(10*time.Second))
	if err := r.require("scheduled_deny_expires_queued_view", errors.Is(applyErr, presentation.ErrStale) && view1.ValidUntil().Equal(now.Add(10*time.Second))); err != nil {
		return r, err
	}

	doc.Revision = 2
	doc.Rules = append(doc.Rules,
		policy.Rule{ID: "revoke-see-staff", RoleID: "member", Action: presentation.VisibilityAction, ResourceType: "player", ResourceID: "staff", Effect: policy.Deny},
		policy.Rule{ID: "revoke-kick", RoleID: "moderator", Action: "command.execute", ResourceType: "command", ResourceID: "moderation.kick", Effect: policy.Deny},
	)
	p2, err := policy.Compile(doc)
	if err != nil {
		return r, err
	}
	nextRequest := request
	nextRequest.Expected, nextRequest.Now = p2.Version(), now.Add(2*time.Second)
	kick.Context, kick.Expected, kick.Now = nextRequest.Context, nextRequest.Expected, nextRequest.Now
	decision = p2.Evaluate(kick)
	if err := r.require("new_revision_revokes_command", !decision.Allowed && decision.RuleID == "revoke-kick"); err != nil {
		return r, err
	}
	nextStamp := stamp
	nextStamp.Policy, nextStamp.ContextRevision, nextStamp.Revision = p2.Version(), 2, 3
	view2, err := presentation.Build(presentation.Input{Stamp: nextStamp, Policy: p2, Request: nextRequest, Candidates: candidates, Limit: 20})
	if err != nil {
		return r, err
	}
	plan, err := presentation.Diff(view1, view2)
	if err != nil {
		return r, err
	}
	changes := plan.Changes()
	if err := r.require("privacy_removal_precedes_updates", len(changes) == 2 && changes[0].Kind == "remove" && changes[0].Row.PlayerID == "staff" && changes[1].Kind == "upsert" && changes[1].Row.PlayerID == "builder"); err != nil {
		return r, err
	}
	current, err := presentation.Apply(view1, plan, nextRequest.Now)
	if err != nil {
		return r, err
	}
	for _, row := range current.Rows() {
		r.VisiblePlayerIDs = append(r.VisiblePlayerIDs, row.PlayerID)
	}
	if err := r.require("revoked_player_removed_from_model", slices.Equal(r.VisiblePlayerIDs, []string{"builder"})); err != nil {
		return r, err
	}
	_, applyErr = presentation.Apply(current, plan, nextRequest.Now)
	if err := r.require("replayed_view_plan_rejected", errors.Is(applyErr, presentation.ErrStale)); err != nil {
		return r, err
	}
	_, diffErr := presentation.Diff(view2, view1)
	if err := r.require("view_policy_rollback_rejected", errors.Is(diffErr, presentation.ErrStale)); err != nil {
		return r, err
	}

	// Fixed bytes are deliberately synthetic fixture data. A real adapter must
	// generate an unpredictable token and bind it to authenticated transport.
	token := sha256.Sum256([]byte("synthetic offline lab token; never a credential"))
	transfer, err := machine.Reserve(nextRequest.Now, source, session.Reservation{ID: "transfer:1", DestinationInstance: "arena:boot2", Token: token[:], ExpiresAt: now.Add(20 * time.Second)})
	if err != nil {
		return r, err
	}
	_, claimErr := machine.Claim(nextRequest.Now, transfer.Ref, "arena:boot2", token[:])
	if err := r.require("destination_cannot_claim_before_freeze", errors.Is(claimErr, session.ErrConflict) && machine.Authorize(source)); err != nil {
		return r, err
	}
	checkpoint := session.Checkpoint{Version: 1, Digest: sha256.Sum256([]byte("synthetic checkpoint; no world was saved"))}
	frozen, err := machine.Freeze(now.Add(3*time.Second), source, transfer.Ref, checkpoint)
	if err != nil {
		return r, err
	}
	player, ok := machine.Player(source.PlayerID)
	if err := r.require("freeze_removes_owner_and_fences_source", ok && player.Phase == session.Frozen && player.OwnerInstance == "" && player.Generation == source.Generation+1 && !machine.Authorize(source)); err != nil {
		return r, err
	}
	_, claimErr = machine.Claim(now.Add(3*time.Second), frozen.Ref, "wrong:boot3", token[:])
	if err := r.require("handoff_is_bound_to_destination", errors.Is(claimErr, session.ErrUnauthorized)); err != nil {
		return r, err
	}
	target, err := machine.Claim(now.Add(4*time.Second), frozen.Ref, "arena:boot2", token[:])
	if err != nil {
		return r, err
	}
	if err := r.require("only_destination_has_current_fence", machine.Authorize(target) && !machine.Authorize(source) && target.Generation == frozen.Ref.Generation); err != nil {
		return r, err
	}
	_, claimErr = machine.Claim(now.Add(4*time.Second), frozen.Ref, "arena:boot2", token[:])
	if err := r.require("duplicate_handoff_claim_rejected", errors.Is(claimErr, session.ErrNotFound)); err != nil {
		return r, err
	}
	targetStamp := nextStamp
	targetStamp.InstanceID, targetStamp.Generation, targetStamp.ContextRevision, targetStamp.Revision = target.InstanceID, target.Generation, 3, 1
	targetRequest := nextRequest
	targetRequest.Now = now.Add(4 * time.Second)
	// The trusted destination adapter must assemble its own context. Reusing
	// the hub's context would incorrectly carry hub-conditional membership.
	targetRequest.Context = map[string]string{"realm": "arena", "lockdown": "false"}
	targetDecision := p2.Evaluate(targetRequest)
	if err := r.require("destination_rebuilds_trusted_context", targetDecision.Allowed && slices.Equal(targetDecision.InheritancePath, []string{"member"})); err != nil {
		return r, err
	}
	targetView, err := presentation.Build(presentation.Input{Stamp: targetStamp, Policy: p2, Request: targetRequest, Candidates: candidates, Limit: 20})
	if err != nil {
		return r, err
	}
	_, diffErr = presentation.Diff(view2, targetView)
	if err := r.require("session_change_requires_new_view", errors.Is(diffErr, presentation.ErrReset)); err != nil {
		return r, err
	}
	r.VisiblePlayerIDs = nil
	for _, row := range targetView.Rows() {
		r.VisiblePlayerIDs = append(r.VisiblePlayerIDs, row.PlayerID)
	}
	r.PolicyRevision, r.SessionGeneration, r.OwnerInstance = p2.Version().Revision, target.Generation, target.InstanceID
	r.ContextRevision, r.Realm = targetView.Stamp().ContextRevision, targetRequest.Context["realm"]

	// Two raw envelope fixtures, not meaningful Minecraft gameplay packets.
	budget, err := wire.NewBudget(4)
	if err != nil {
		return r, err
	}
	reader := bytes.NewReader([]byte{3, 0, 1, 2, 2, 3, 4})
	decoder, err := wire.NewDecoder(reader, wire.Limits{MaxFrameBytes: 16, MaxUncompressedBytes: 16, CompressionThreshold: -1}, budget)
	if err != nil {
		return r, err
	}
	first, err := decoder.Next()
	if err != nil {
		return r, err
	}
	_, budgetErr := decoder.Next()
	if err := r.require("overload_waits_before_reading_body", errors.Is(budgetErr, wire.ErrBudget) && reader.Len() == 2 && budget.Used() == 3); err != nil {
		first.Release()
		return r, err
	}
	first.Release()
	second, err := decoder.Next()
	if err != nil {
		return r, err
	}
	aligned := bytes.Equal(second.Payload, []byte{3, 4})
	second.Release()
	second.Release()
	if err := r.require("budget_resume_preserves_frame_alignment", aligned && budget.Used() == 0); err != nil {
		return r, err
	}
	return r, nil
}

func run(out, errorOut io.Writer) int {
	result, err := simulate()
	if err != nil {
		result.Error = err.Error()
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if encodeErr := encoder.Encode(result); encodeErr != nil {
		fmt.Fprintln(errorOut, encodeErr)
		return 1
	}
	if err != nil {
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Stdout, os.Stderr)) }
