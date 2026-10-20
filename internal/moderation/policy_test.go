package moderation

import (
	"testing"
	"time"
)

func standard() Request {
	now := time.Date(2026, 10, 18, 18, 0, 0, 0, time.UTC)
	return Request{
		Actor:  Actor{ID: "staff-01", Role: HostingAdmin, TenantIDs: []string{"tenantA"}, MFAAt: now.Add(-time.Minute)},
		Target: Target{ID: "srv-07", Type: HostedServer, TenantID: "tenantA", State: "stopped", ActivePlayers: 0, VerifiedBackup: true},
		Action: ArchiveServer, Reason: "Validated archival request", CaseID: "CASE-924", IdempotencyKey: "request-12345", Now: now,
	}
}
func TestAuthorizeSafeArchive(t *testing.T) {
	r := standard()
	d := Evaluate(r)
	if !d.Allowed || d.ProposedState != "archived" {
		t.Fatalf("expected archive plan, got %#v", d)
	}
	if r.Target.State != "stopped" {
		t.Fatal("Evaluate changed target state")
	}
}
func TestArchiveGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Request)
		code   string
	}{
		{"players", func(r *Request) { r.Target.ActivePlayers = 5 }, "drain_required"},
		{"backup", func(r *Request) { r.Target.VerifiedBackup = false }, "backup_required"},
		{"running", func(r *Request) { r.Target.State = "online" }, "state_conflict"},
		{"mfa stale", func(r *Request) { r.Actor.MFAAt = r.Now.Add(-12 * time.Minute) }, "fresh_mfa_required"},
		{"cross tenant", func(r *Request) { r.Target.TenantID = "tenantB" }, "tenant_scope"},
		{"missing case", func(r *Request) { r.CaseID = "" }, "incomplete_identity"},
		{"no reason", func(r *Request) { r.Reason = "no" }, "missing_reason"},
		{"missing key", func(r *Request) { r.IdempotencyKey = "x" }, "missing_idempotency"},
		{"negative players", func(r *Request) { r.Target.ActivePlayers = -1 }, "invalid_target"},
		{"viewer", func(r *Request) { r.Actor.Role = Viewer }, "permission_denied"},
		{"legacy", func(r *Request) { r.Target.Type = LegacyServer }, "legacy_read_only"},
		{"hard delete", func(r *Request) { r.Action = DeleteServer }, "delete_disabled"},
		{"unknown action", func(r *Request) { r.Action = "shell.execute" }, "unknown_action"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := standard()
			tc.change(&r)
			d := Evaluate(r)
			if d.Allowed || d.Code != tc.code {
				t.Fatalf("want %s, got %#v", tc.code, d)
			}
		})
	}
}
func TestPlayerModerator(t *testing.T) {
	r := standard()
	r.Actor.Role = PlayerModerator
	r.Action = BanPlayer
	r.Target = Target{ID: "verified-uuid", Type: Player, State: "active", VerifiedIdentity: true}
	r.EvidenceIDs = []string{"evidence-1"}
	d := Evaluate(r)
	if !d.Allowed || d.ProposedState != "banned" {
		t.Fatalf("expected ban proposal got %#v", d)
	}
	r.Target.VerifiedIdentity = false
	if d := Evaluate(r); d.Code != "unverified_player" {
		t.Fatalf("expected verification check got %#v", d)
	}
	r.Target.VerifiedIdentity = true
	r.EvidenceIDs = nil
	if d := Evaluate(r); d.Code != "missing_evidence" {
		t.Fatalf("expected evidence got %#v", d)
	}
	r.EvidenceIDs = []string{"ref"}
	r.Actor.Role = HostingAdmin
	if d := Evaluate(r); d.Code != "permission_denied" {
		t.Fatalf("hosting-admin should not silently be a game mod %#v", d)
	}
}
func TestServerSuspensionRequiresDrain(t *testing.T) {
	r := standard()
	r.Action = SuspendServer
	r.EvidenceIDs = []string{"abuse-report-01"}
	r.Target.State = "online"
	r.Target.ActivePlayers = 12
	if d := Evaluate(r); d.Code != "drain_required" {
		t.Fatalf("need drain got %#v", d)
	}
	r.Target.ActivePlayers = 0
	if d := Evaluate(r); !d.Allowed || d.ProposedState != "suspended" {
		t.Fatalf("expected reversible suspension %#v", d)
	}
}
func TestReversibleRestore(t *testing.T) {
	r := standard()
	r.Action = UnarchiveServer
	r.Target.State = "archived"
	r.Target.VerifiedBackup = false
	if d := Evaluate(r); !d.Allowed || d.ProposedState != "stopped" {
		t.Fatalf("restore permitted if archived %#v", d)
	}
	r.Action = RestoreServer
	r.Target.State = "suspended"
	if d := Evaluate(r); !d.Allowed || d.ProposedState != "stopped" {
		t.Fatalf("unsuspend permitted %#v", d)
	}
}
func TestListingModeration(t *testing.T) {
	r := standard()
	r.Actor.Role = HostingModerator
	r.Target = Target{ID: "listing-01", Type: Listing, TenantID: "tenantA", State: "listed"}
	r.Action = UnlistServer
	r.EvidenceIDs = []string{"case-evidence"}
	if d := Evaluate(r); !d.Allowed || d.ProposedState != "unlisted" {
		t.Fatalf("expected unlist %#v", d)
	}
	r.Action = SuspendServer
	if d := Evaluate(r); d.Code != "missing_evidence" && d.Code != "wrong_target" {
		t.Fatalf("unexpected result %#v", d)
	}
}
