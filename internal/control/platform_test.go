package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/platform/lifecycle"
	"github.com/oncoursemail/metallic-mc/internal/platform/syncplan"
)

func TestPlatformFeatureGateAndAuthentication(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		_, h, _, _ := architectFixture(t, enabled)
		for _, path := range []string{"platform/capabilities", "platform/sync/plan", "platform/diagnose", "platform/lifecycle/plan"} {
			method := "POST"
			if strings.HasSuffix(path, "capabilities") {
				method = "GET"
			}
			w := architectRequest(h, method, architectPrefix+path, "{}", nil, true)
			want := 404
			if enabled {
				want = 401
			}
			if w.Code != want {
				t.Fatalf("enabled %v %s code %d", enabled, path, w.Code)
			}
		}
	}
}
func TestPlatformStrictRequestsAndIntent(t *testing.T) {
	_, h, cookie, _ := architectFixture(t, true)
	for _, tc := range []struct {
		body   string
		intent bool
		want   int
	}{{`{"log":"test"}`, false, 403}, {`{"log":"test","Supervisor":{"ExitCode":137}}`, true, 400}, {`{"log":"test","owner_id":"other"}`, true, 400}, {`{"log":"test"} {}`, true, 400}, {`{"log":"test","accepted_policy_revision":"ok"}`, true, 400}} {
		w := architectRequest(h, "POST", architectPrefix+"platform/diagnose", tc.body, cookie, tc.intent)
		if w.Code != tc.want {
			t.Fatalf("code %d body %s", w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "http://example.com"+architectPrefix+"platform/diagnose", strings.NewReader(`{"log":"test"}`))
	r.AddCookie(cookie)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Metallic-Intent", "architect")
	r.Header.Set("Origin", "https://foreign.invalid")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestPlatformDiagnosticsNoRawLogs(t *testing.T) {
	_, h, cookie, _ := architectFixture(t, true)
	body, _ := json.Marshal(map[string]string{"log": "secret-token-123 java.lang.OutOfMemoryError: Java heap space\n"})
	w := architectRequest(h, "POST", architectPrefix+"platform/diagnose", string(body), cookie, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "java_heap_pressure") || strings.Contains(w.Body.String(), "secret-token-123") || strings.Contains(w.Body.String(), `"supervisorAvailable":true`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("diagnosis cacheable")
	}
}
func TestPlatformSyncAlwaysNeedsTrustedEvidence(t *testing.T) {
	_, h, cookie, _ := architectFixture(t, true)
	req := syncplan.Request{TargetMode: syncplan.ManagedPrivate, SourceLayout: syncplan.Java26, WorldRoots: []string{"world"}, Entries: []syncplan.Entry{{Path: "world/level.dat", Kind: syncplan.File, Size: 1, SHA256: strings.Repeat("1", 64)}}}
	body, _ := json.Marshal(req)
	w := architectRequest(h, "POST", architectPrefix+"platform/sync/plan", string(body), cookie, true)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var result syncplan.Result
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Executable || len(result.Blockers) < 6 || result.PolicyRevision != platformPolicyRevision || result.Digest == "" {
		t.Fatalf("unsafe plan %+v", result)
	}
	req.BaseRemoteGeneration = 1
	body, _ = json.Marshal(req)
	w = architectRequest(h, "POST", architectPrefix+"platform/sync/plan", string(body), cookie, true)
	if w.Code != 409 {
		t.Fatal("conflict accepted", w.Code)
	}
	for _, field := range []string{"accepted_policy_revision", "coherent_checkpoint", "source_stopped", "trusted_context", "server_id"} {
		body := `{"` + field + `":true}`
		w = architectRequest(h, "POST", architectPrefix+"platform/sync/plan", body, cookie, true)
		if w.Code != 400 {
			t.Fatalf("client evidence %s accepted", field)
		}
	}
}
func TestPlatformLifecycleStateIsNotInventedFromAllocations(t *testing.T) {
	_, h, cookie, _ := architectFixture(t, true)
	w := architectRequest(h, "POST", architectPrefix+"platform/lifecycle/plan", `{"server_id":"survival","action":"start","request_id":"op1","expected_revision":1}`, cookie, true)
	if w.Code != 503 {
		t.Fatalf("missing adapter %d %s", w.Code, w.Body.String())
	}
}
func TestPlatformLifecyclePrincipalAndEvidenceRemainServerSide(t *testing.T) {
	c, h, cookie, _ := architectFixture(t, true)
	state := lifecycle.TrustedState{ServerID: "survival", OwnerID: "admin", AuthorityEpoch: "test-epoch", Revision: 1, Generation: 0, Location: lifecycle.Hosted, Visibility: lifecycle.Private, Phase: lifecycle.Unprovisioned}
	err := c.ConfigurePlatformState(func(principal, serverID string) (lifecycle.TrustedState, error) {
		if principal != "admin" {
			t.Fatal("untrusted principal")
		}
		if serverID != "survival" {
			return lifecycle.TrustedState{}, ErrPlatformServerNotFound
		}
		return state, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"server_id":"survival","action":"start","request_id":"op1","expected_revision":1}`
	w := architectRequest(h, "POST", architectPrefix+"platform/lifecycle/plan", body, cookie, true)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var d lifecycle.Decision
	json.Unmarshal(w.Body.Bytes(), &d)
	if d.Allowed || d.ExecutionEnabled || len(d.Blockers) == 0 {
		t.Fatal("unattested start admitted", d)
	}
	state.Location = lifecycle.Local
	w = architectRequest(h, "POST", architectPrefix+"platform/lifecycle/plan", body, cookie, true)
	json.Unmarshal(w.Body.Bytes(), &d)
	if w.Code != 200 || !d.Allowed || d.ExecutionEnabled {
		t.Fatal("local ownership lost", w.Code, w.Body.String())
	}
	state.OwnerID = "another-owner"
	w = architectRequest(h, "POST", architectPrefix+"platform/lifecycle/plan", body, cookie, true)
	json.Unmarshal(w.Body.Bytes(), &d)
	if d.Allowed {
		t.Fatal("cross owner plan allowed")
	}
	for _, bad := range []string{`{"principal":{"id":"admin","kind":"moderator"}}`, `{"evidence":{"running":true}}`, `{"supervisor":{}}`} {
		if w := architectRequest(h, "POST", architectPrefix+"platform/lifecycle/plan", bad, cookie, true); w.Code != 400 {
			t.Fatal("extra field accepted", w.Code)
		}
	}
	c.now = func() time.Time { return time.Date(2026, 10, 19, 12, 16, 0, 0, time.UTC) }
	w = architectRequest(h, "POST", architectPrefix+"platform/lifecycle/plan", body, cookie, true)
	if w.Code != 401 {
		t.Fatal("stale authentication accepted", w.Code)
	}
}
func TestPlatformTransportBound(t *testing.T) {
	_, h, cookie, _ := architectFixture(t, true)
	body := `{"log":"` + strings.Repeat("x", maxPlatformBody) + `"}`
	w := architectRequest(h, "POST", architectPrefix+"platform/diagnose", body, cookie, true)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatal(w.Code)
	}
}

func TestPlatformRejectsMalformedIntentBeforeAdapter(t *testing.T) {
	c, h, cookie, _ := architectFixture(t, true)
	called := false
	if err := c.ConfigurePlatformState(func(string, string) (lifecycle.TrustedState, error) {
		called = true
		return lifecycle.TrustedState{}, ErrPlatformServerNotFound
	}); err != nil {
		t.Fatal(err)
	}
	w := architectRequest(h, "POST", architectPrefix+"platform/lifecycle/plan", `{"server_id":"../../host","action":"start","request_id":"op1","expected_revision":1}`, cookie, true)
	if w.Code != 400 || called {
		t.Fatal("malformed intent reached adapter", w.Code, called)
	}
}

func TestPlatformBoundedWorkAndPage(t *testing.T) {
	c, h, cookie, _ := architectFixture(t, true)
	c.platformWork <- struct{}{}
	c.platformWork <- struct{}{}
	w := architectRequest(h, "POST", architectPrefix+"platform/diagnose", `{"log":"test"}`, cookie, true)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal(w.Code)
	}
	<-c.platformWork
	<-c.platformWork
	w = architectRequest(h, "POST", architectPrefix+"platform/diagnose", `{"log":"test"}`, cookie, true)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = architectRequest(h, "GET", "/architect/platform", "", cookie, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Server Studio") {
		t.Fatal(w.Code)
	}
	_, off, offCookie, _ := architectFixture(t, false)
	w = architectRequest(off, "GET", "/architect/platform", "", offCookie, false)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
