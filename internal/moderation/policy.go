// Package moderation defines side-effect-free authorization decisions for
// Metallic Minecraft administrative and player-moderation operations.
//
// This package NEVER executes Minecraft commands, changes containers, contacts
// Wings, removes files, or writes audit records. It is only a policy gate.
// A secure API must independently authenticate the actor, transactionally apply
// state transitions after rechecking policy, and durably audit every decision.
package moderation

import (
	"slices"
	"strings"
	"time"
)

type Role string

const (
	Viewer           Role = "viewer"
	PlayerModerator  Role = "player-moderator"
	HostingModerator Role = "hosting-moderator"
	HostingAdmin     Role = "hosting-admin"
	PlatformAdmin    Role = "platform-admin"
)

type TargetType string

const (
	Player       TargetType = "player"
	HostedServer TargetType = "hosted-server"
	Listing      TargetType = "server-listing"
	LegacyServer TargetType = "legacy-server"
	OwnedServer  TargetType = "metallic-owned-server"
)

type Action string

const (
	WarnPlayer      Action = "player.warn"
	MutePlayer      Action = "player.mute"
	BanPlayer       Action = "player.ban"
	UnlistServer    Action = "listing.unlist"
	RelistServer    Action = "listing.relist"
	SuspendServer   Action = "server.suspend"
	RestoreServer   Action = "server.unsuspend"
	ArchiveServer   Action = "server.archive"
	UnarchiveServer Action = "server.unarchive"
	DeleteServer    Action = "server.delete" // deliberately not supported
)

type Actor struct {
	ID   string
	Role Role
	// TenantIDs grants explicitly scoped access to resources owned by these tenants.
	TenantIDs []string
	// Evidence that MFA was recently completed; supplied by trusted authentication.
	MFAAt time.Time
}

type Target struct {
	ID               string
	Type             TargetType
	TenantID         string
	State            string
	ActivePlayers    int
	VerifiedIdentity bool
	VerifiedBackup   bool
}

type Request struct {
	Actor          Actor
	Target         Target
	Action         Action
	Reason         string
	CaseID         string
	IdempotencyKey string
	EvidenceIDs    []string
	Now            time.Time
}

type Decision struct {
	Allowed       bool   `json:"allowed"`
	Code          string `json:"code"`
	Detail        string `json:"detail"`
	ProposedState string `json:"proposedState,omitempty"`
}

func allow(state string) Decision {
	return Decision{Allowed: true, Code: "allowed", Detail: "Policy preconditions satisfied; no side effects have occurred", ProposedState: state}
}
func deny(code, detail string) Decision { return Decision{Code: code, Detail: detail} }

func ActionNeedsEvidence(action Action) bool {
	switch action {
	case WarnPlayer, MutePlayer, BanPlayer, UnlistServer, SuspendServer:
		return true
	}
	return false
}

// Evaluate is intentionally fail-closed and has no dependencies on live services.
// The target state must be re-read transactionally when committing an operation.
func Evaluate(r Request) Decision {
	if r.Now.IsZero() || strings.TrimSpace(r.Actor.ID) == "" || strings.TrimSpace(r.Target.ID) == "" || strings.TrimSpace(r.CaseID) == "" {
		return deny("incomplete_identity", "Authenticated actor, target, case and current time are required")
	}
	if len(r.IdempotencyKey) < 8 || len(r.IdempotencyKey) > 128 {
		return deny("missing_idempotency", "An 8–128-character operation key is required")
	}
	if len(strings.TrimSpace(r.Reason)) < 8 || len(r.Reason) > 500 {
		return deny("missing_reason", "A bounded, meaningful operator reason is required")
	}
	if r.Target.Type == LegacyServer {
		return deny("legacy_read_only", "Old Servers cannot be mutated through Metallic moderation")
	}
	if r.Action == DeleteServer {
		return deny("delete_disabled", "Permanent deletion is not implemented by this policy")
	}
	if !isKnownAction(r.Action) {
		return deny("unknown_action", "Unrecognized administrative operation")
	}
	if r.Target.ActivePlayers < 0 {
		return deny("invalid_target", "Negative player count is invalid")
	}
	if ActionNeedsEvidence(r.Action) && len(r.EvidenceIDs) == 0 {
		return deny("missing_evidence", "Evidence is required for punitive actions")
	}
	for _, e := range r.EvidenceIDs {
		if strings.TrimSpace(e) == "" {
			return deny("invalid_evidence", "Evidence references must not be blank")
		}
	}
	if !targetCompatible(r.Action, r.Target.Type) {
		return deny("wrong_target", "Action is not valid for this resource type")
	}
	if !roleCan(r.Actor.Role, r.Action) {
		return deny("permission_denied", "Role does not allow this operation")
	}

	// Tenant assignment must be explicit for customer resources. An administrator
	// cannot silently gain authority over another customer's server.
	if r.Target.Type == HostedServer || r.Target.Type == Listing {
		if r.Target.TenantID == "" || !slices.Contains(r.Actor.TenantIDs, r.Target.TenantID) {
			return deny("tenant_scope", "Actor lacks explicit authority over this tenant")
		}
	}
	if r.Target.Type == Player && (r.Action == BanPlayer || r.Action == MutePlayer) && !r.Target.VerifiedIdentity {
		return deny("unverified_player", "Sanctions require verified platform player identity")
	}
	if highRisk(r.Action) {
		if r.Actor.MFAAt.IsZero() || r.Actor.MFAAt.After(r.Now) || r.Now.Sub(r.Actor.MFAAt) > 10*time.Minute {
			return deny("fresh_mfa_required", "Sensitive server operations require MFA within ten minutes")
		}
	}
	if r.Action == SuspendServer && r.Target.ActivePlayers > 0 {
		return deny("drain_required", "Active players must be safely drained before suspending a server")
	}
	if r.Action == ArchiveServer {
		if r.Target.ActivePlayers != 0 {
			return deny("drain_required", "Cannot archive a server with active players")
		}
		if !r.Target.VerifiedBackup {
			return deny("backup_required", "A verified restorable backup is required before archive")
		}
	}

	switch r.Action {
	case WarnPlayer:
		return allow(r.Target.State)
	case MutePlayer:
		return allow("muted")
	case BanPlayer:
		return allow("banned")
	case UnlistServer:
		if r.Target.State != "listed" {
			return deny("state_conflict", "Only listed servers may be unlisted")
		}
		return allow("unlisted")
	case RelistServer:
		if r.Target.State != "unlisted" {
			return deny("state_conflict", "Only unlisted servers may be relisted")
		}
		return allow("listed")
	case SuspendServer:
		if r.Target.State == "suspended" || r.Target.State == "archived" {
			return deny("state_conflict", "Server is not eligible for suspension")
		}
		return allow("suspended")
	case RestoreServer:
		if r.Target.State != "suspended" {
			return deny("state_conflict", "Only suspended servers can be unsuspended")
		}
		return allow("stopped")
	case ArchiveServer:
		if r.Target.State != "stopped" && r.Target.State != "suspended" {
			return deny("state_conflict", "Server must be stopped or suspended before archiving")
		}
		return allow("archived")
	case UnarchiveServer:
		if r.Target.State != "archived" {
			return deny("state_conflict", "Only archived servers can be restored")
		}
		return allow("stopped")
	default:
		return deny("unknown_action", "Unrecognized administrative operation")
	}
}
func isKnownAction(a Action) bool {
	switch a {
	case WarnPlayer, MutePlayer, BanPlayer, UnlistServer, RelistServer, SuspendServer, RestoreServer, ArchiveServer, UnarchiveServer, DeleteServer:
		return true
	}
	return false
}
func targetCompatible(a Action, t TargetType) bool {
	switch a {
	case WarnPlayer, MutePlayer, BanPlayer:
		return t == Player
	case UnlistServer, RelistServer:
		return t == Listing
	case SuspendServer, RestoreServer, ArchiveServer, UnarchiveServer:
		return t == HostedServer
	}
	return false
}
func roleCan(role Role, a Action) bool {
	switch a {
	case WarnPlayer, MutePlayer, BanPlayer:
		return role == PlayerModerator || role == PlatformAdmin
	case UnlistServer, RelistServer:
		return role == HostingModerator || role == HostingAdmin || role == PlatformAdmin
	case SuspendServer, RestoreServer, ArchiveServer, UnarchiveServer:
		return role == HostingAdmin || role == PlatformAdmin
	}
	return false
}
func highRisk(a Action) bool {
	switch a {
	case SuspendServer, RestoreServer, ArchiveServer, UnarchiveServer:
		return true
	}
	return false
}
