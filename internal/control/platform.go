package control

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/oncoursemail/metallic-mc/internal/diagnostics"
	"github.com/oncoursemail/metallic-mc/internal/platform/lifecycle"
	"github.com/oncoursemail/metallic-mc/internal/platform/syncplan"
)

const platformPolicyRevision = "metallic-hosting-draft-2026-10-20"
const maxPlatformBody = 2 << 20

// PlatformStateReader is a trusted controller adapter, not an HTTP request
// shape. It must read authoritative lifecycle state with tenant authorization.
// Existing BYOH allocation plans do NOT establish managed runtime state, so the
// default controller deliberately has no adapter. No operation is executed.
type PlatformStateReader func(principalID, serverID string) (lifecycle.TrustedState, error)

var ErrPlatformServerNotFound = errors.New("platform server not found")

// ConfigurePlatformState is a startup-only extension point. A production
// implementation needs durable tenant identity, lifecycle revisions and current
// executor evidence. Passing a reader does not enable runtime execution.
func (c *Controller) ConfigurePlatformState(reader PlatformStateReader) error {
	if !c.cfg.ArchitectEnabled || reader == nil || c.platformState != nil {
		return errors.New("platform state adapter unavailable or already configured")
	}
	c.platformState = reader
	return nil
}

func (c *Controller) registerPlatform(mux *http.ServeMux) {
	mux.HandleFunc("GET "+architectPrefix+"platform/capabilities", c.requireAdmin(c.platformCapabilities))
	mux.HandleFunc("POST "+architectPrefix+"platform/sync/plan", c.requireAdmin(c.architectMutation(c.limitPlatformWork(c.platformSyncPlan), false)))
	mux.HandleFunc("POST "+architectPrefix+"platform/diagnose", c.requireAdmin(c.architectMutation(c.limitPlatformWork(c.platformDiagnose), false)))
	mux.HandleFunc("POST "+architectPrefix+"platform/lifecycle/plan", c.requireAdmin(c.architectMutation(c.limitPlatformWork(c.platformLifecyclePlan), true)))
}
func (c *Controller) limitPlatformWork(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case c.platformWork <- struct{}{}:
			defer func() { <-c.platformWork }()
			next(w, r)
		default:
			w.Header().Set("Retry-After", "2")
			http.Error(w, "platform analysis is busy; retry shortly", http.StatusTooManyRequests)
		}
	}
}
func (c *Controller) platformPage(w http.ResponseWriter, r *http.Request) {
	if !c.cfg.ArchitectEnabled {
		http.NotFound(w, r)
		return
	}
	c.requireAdmin(func(w http.ResponseWriter, r *http.Request) { c.serveFile(w, "web/platform.html") })(w, r)
}
func (c *Controller) platformCapabilities(w http.ResponseWriter, r *http.Request) {
	jsonReply(w, 200, map[string]any{
		"schema": "metallic.platform-capabilities/v1", "policy_revision": platformPolicyRevision,
		"sync_inventory_planner": true, "local_crash_diagnostics": true, "lifecycle_planner": true,
		"lifecycle_state_configured": c.platformState != nil, "runtime_execution": false, "public_discovery": false,
		"minecraft_launcher_authentication": false, "managed_compatibility_deployed": false, "gateway_population_connected": false,
		"policy_acceptance_store": false, "max_request_bytes": maxPlatformBody, "max_log_bytes": diagnostics.MaxLogBytes,
		"modes": []map[string]any{
			{"id": "local-private", "title": "Local private", "hosting": "local", "visibility": "private", "mandatory_platform_runtime": false, "platform_policy_required": false, "description": "Your local server stays outside Metallic hosting and discovery."},
			{"id": "managed-private", "title": "Hosted private", "hosting": "managed", "visibility": "private", "mandatory_platform_runtime": true, "platform_policy_required": true, "description": "A separate hosted copy with platform isolation, compatibility, and invitation-only access."},
			{"id": "managed-public", "title": "Hosted public", "hosting": "managed", "visibility": "public", "mandatory_platform_runtime": true, "platform_policy_required": true, "description": "A managed hosted copy with reviewed discovery and gateway-authenticated sessions."},
			{"id": "local-public-tunnel", "title": "Local public tunnel", "hosting": "local", "visibility": "public", "mandatory_platform_runtime": false, "platform_policy_required": true, "description": "Future network-admission tier. The local process remains under its owner's control; a tunnel is not managed isolation."},
		},
	})
}

func decodePlatformJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		http.Error(w, "application/json required", 415)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPlatformBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err = dec.Decode(value); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request too large", 413)
		} else {
			http.Error(w, "invalid request", 400)
		}
		return false
	}
	var extra any
	if !errors.Is(dec.Decode(&extra), io.EOF) {
		http.Error(w, "trailing or oversized request data", 400)
		return false
	}
	return true
}
func (c *Controller) platformSyncPlan(w http.ResponseWriter, r *http.Request) {
	var input syncplan.Request
	if !decodePlatformJSON(w, r, &input) {
		return
	}
	// This preview is a proposed first import, not a read of a hosted world.
	// No client field can assert policy acceptance or checkpoint verification.
	result, err := syncplan.Plan(input, syncplan.TrustedContext{RequiredPolicyRevision: platformPolicyRevision, RemoteGeneration: 0})
	if err != nil {
		status := 400
		if errors.Is(err, syncplan.ErrConflict) {
			status = 409
		}
		http.Error(w, "sync inventory rejected: "+err.Error(), status)
		return
	}
	jsonReply(w, 200, result)
}
func (c *Controller) platformDiagnose(w http.ResponseWriter, r *http.Request) {
	var input diagnostics.Request
	if !decodePlatformJSON(w, r, &input) {
		return
	}
	result, err := diagnostics.Analyze(input)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	jsonReply(w, 200, result)
}
func (c *Controller) platformLifecyclePlan(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ServerID         string           `json:"server_id"`
		Action           lifecycle.Action `json:"action"`
		RequestID        string           `json:"request_id"`
		ExpectedRevision uint64           `json:"expected_revision"`
	}
	if !decodePlatformJSON(w, r, &input) {
		return
	}
	request := lifecycle.Request{Principal: lifecycle.Principal{ID: c.cfg.AdminUser, Kind: lifecycle.Owner}, ServerID: input.ServerID, Action: input.Action, RequestID: input.RequestID, ExpectedRevision: input.ExpectedRevision}
	if err := lifecycle.ValidateRequest(request); err != nil {
		http.Error(w, "invalid lifecycle request", 400)
		return
	}
	if c.platformState == nil {
		http.Error(w, "authoritative lifecycle state is not configured; allocation plans do not establish a running or managed server", 503)
		return
	}
	state, err := c.platformState(c.cfg.AdminUser, input.ServerID)
	if err != nil {
		if errors.Is(err, ErrPlatformServerNotFound) {
			http.Error(w, "server not found", 404)
		} else {
			http.Error(w, "lifecycle state unavailable", 503)
		}
		return
	}
	// The reader supplies scope and attestations; request time and identity come
	// from the current authenticated controller, never JSON.
	state.Now = c.now()
	evidence, err := lifecycle.NewEvidence(state)
	if err != nil {
		http.Error(w, "lifecycle state invalid", 503)
		return
	}
	decision, err := lifecycle.Plan(request, evidence)
	if err != nil {
		http.Error(w, "lifecycle request rejected", 400)
		return
	}
	jsonReply(w, 200, decision)
}
