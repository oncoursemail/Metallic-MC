package control

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/oncoursemail/metallic-mc/internal/resources"
)

func testHandler() http.Handler {
	return New(Config{AdminUser: "admin", AdminPassword: "averysecurepassword", AgentToken: "this-is-a-long-secret-token-for-test"})
}
func TestUnauthenticatedAccessBlocked(t *testing.T) {
	h := testHandler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/overview", nil))
	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 303 {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
}
func TestHeartbeatRequiresTokenAndValidates(t *testing.T) {
	h := testHandler()
	hb := Heartbeat{NodeID: "node-01", Hostname: "metallic-host", CPUPercent: 18.2, MemoryUsed: 100, MemoryTotal: 1000, DiskUsed: 1000, DiskTotal: 10000, Instances: []Instance{{Name: "lobby", Address: "127.0.0.1:25566", Kind: "tcp", Healthy: true}}}
	b, _ := json.Marshal(hb)
	req := httptest.NewRequest("POST", "/api/v1/heartbeat", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	req = httptest.NewRequest("POST", "/api/v1/heartbeat", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer this-is-a-long-secret-token-for-test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	hb.CPUPercent = 101
	b, _ = json.Marshal(hb)
	req = httptest.NewRequest("POST", "/api/v1/heartbeat", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer this-is-a-long-secret-token-for-test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected invalid metrics 400, got %d", w.Code)
	}
}
func TestAdminLoginAndOverview(t *testing.T) {
	h := testHandler()
	form := url.Values{"username": {"admin"}, "password": {"averysecurepassword"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 303 {
		t.Fatalf("login returned %d: %s", w.Code, w.Body.String())
	}
	res := w.Result()
	defer res.Body.Close()
	cookies := res.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected session cookie: %+v", cookies)
	}
	req = httptest.NewRequest("GET", "/api/v1/overview", nil)
	req.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("overview returned %d", w.Code)
	}
	var v struct {
		Nodes []Node `json:"nodes"`
		Stage string `json:"stage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Stage != "foundation" || len(v.Nodes) != 0 {
		t.Fatalf("expected empty real dataset, got %+v", v)
	}
}
func TestInvalidHeartbeatIDs(t *testing.T) {
	for _, id := range []string{"../../etc/passwd", "", "hello world", "!hello"} {
		if validHeartbeat(Heartbeat{NodeID: id, Hostname: "x"}) {
			t.Errorf("accepted invalid node id %q", id)
		}
	}
}

func TestOverviewReflectsRealHeartbeat(t *testing.T) {
	c := NewController(Config{AdminUser: "admin", AdminPassword: "averysecurepassword", AgentToken: "this-is-a-long-secret-token-for-test"})
	server := c.Handler()
	hb := Heartbeat{NodeID: "host-02", Hostname: "MetallicHost", CPUPercent: 22, MemoryUsed: 500, MemoryTotal: 1000, DiskUsed: 200, DiskTotal: 1000}
	b, _ := json.Marshal(hb)
	req := httptest.NewRequest("POST", "/api/v1/heartbeat", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer this-is-a-long-secret-token-for-test")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("heartbeat failed: %s", w.Body.String())
	}
	values := url.Values{"username": {"admin"}, "password": {"averysecurepassword"}}
	req = httptest.NewRequest("POST", "/login", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	server.ServeHTTP(w, req)
	cookie := w.Result().Cookies()[0]
	req = httptest.NewRequest("GET", "/api/v1/overview", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	server.ServeHTTP(w, req)
	var result struct {
		Nodes []Node `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 1 || result.Nodes[0].NodeID != "host-02" || !result.Nodes[0].Online || len(result.Nodes[0].History) != 1 {
		t.Fatalf("unexpected live telemetry: %+v", result)
	}
}
func TestLogoutRevokesSession(t *testing.T) {
	h := testHandler()
	values := url.Values{"username": {"admin"}, "password": {"averysecurepassword"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	session := w.Result().Cookies()[0]
	req = httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(session)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 303 {
		t.Fatalf("logout HTTP %d", w.Code)
	}
	req = httptest.NewRequest("GET", "/api/v1/overview", nil)
	req.AddCookie(session)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("old session was still valid: HTTP %d", w.Code)
	}
}

func TestResourceAdmissionAndStarAIReadOnlyAccess(t *testing.T) {
	cfg := Config{AdminUser: "admin", AdminPassword: "averysecurepassword", AgentToken: "this-is-a-long-secret-token-for-test", AIReadToken: "star-read-only-token-separate-test"}
	h := New(cfg)
	hb := Heartbeat{NodeID: "node-safe", Hostname: "secret-hostname", CPUPercent: 12, CPUThreads: 16, CPUPressureSome10: 0.5, MemPressureSome10: 0, PressureAvailable: true, Load1: 2, MemoryTotal: 64 * (1 << 30), MemoryUsed: 20 * (1 << 30), DiskTotal: 256 * (1 << 30), DiskUsed: 60 * (1 << 30)}
	body, _ := json.Marshal(hb)
	req := httptest.NewRequest("POST", "/api/v1/heartbeat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+cfg.AgentToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("heartbeat HTTP %d: %s", w.Code, w.Body.String())
	}
	// Read-only planner is admin-gated, even though it cannot make changes.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/resources?profile=mc-lobby", nil))
	if w.Code != 401 {
		t.Fatalf("planner unauthenticated HTTP %d", w.Code)
	}
	form := url.Values{"username": {"admin"}, "password": {"averysecurepassword"}}
	req = httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 303 {
		t.Fatalf("login HTTP %d", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	req = httptest.NewRequest("GET", "/api/v1/resources?profile=mc-lobby", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("planner HTTP %d: %s", w.Code, w.Body.String())
	}
	var preview struct {
		Preview struct {
			RecommendedNode string `json:"recommendedNode"`
			Stage           string `json:"stage"`
		} `json:"preview"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Preview.RecommendedNode != "node-safe" || preview.Preview.Stage != "read-only-preview" {
		t.Fatalf("unexpected admission: %+v", preview)
	}
	req = httptest.NewRequest("GET", "/api/v1/resources?profile=run-arbitrary-command", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("arbitrary workload accepted: HTTP %d", w.Code)
	}
	// AI endpoint requires its own scope: admin cookies and agent credentials
	// are intentionally insufficient.
	for _, token := range []string{"", "wrong-secret", cfg.AgentToken} {
		req = httptest.NewRequest("GET", "/api/v1/ai/context", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatalf("unauthorized AI token %q: HTTP %d", token, w.Code)
		}
	}
	req = httptest.NewRequest("GET", "/api/v1/ai/context", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.AIReadToken)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("AI read HTTP %d: %s", w.Code, w.Body.String())
	}
	var info struct {
		ReadOnly              bool `json:"readOnly"`
		DeploymentImplemented bool `json:"deploymentImplemented"`
		Nodes                 []struct {
			NodeID string `json:"nodeId"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if !info.ReadOnly || info.DeploymentImplemented || len(info.Nodes) != 1 || info.Nodes[0].NodeID != "node-safe" {
		t.Fatalf("incorrect AI scope: %+v", info)
	}
	for _, forbidden := range []string{cfg.AgentToken, cfg.AIReadToken, "secret-hostname", "25565", "127.0.0.1"} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatalf("AI response exposed private value %q", forbidden)
		}
	}
}

func TestDisabledAIIntegrationReturnsNotFound(t *testing.T) {
	h := testHandler()
	r := httptest.NewRequest("GET", "/api/v1/ai/context", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("expected disabled AI endpoint to be absent, HTTP %d", w.Code)
	}
}

func TestAgentLegacyMetricsCannotTriggerPlacement(t *testing.T) {
	cfg := Config{AdminUser: "admin", AdminPassword: "averysecurepassword", AgentToken: "this-is-a-long-secret-token-for-test"}
	ctrl := NewController(cfg)
	// Older agents may legitimately report without a core count. They must
	// remain visible in diagnostics but never be eligible for new placement.
	hb := Heartbeat{NodeID: "legacy", Hostname: "legacy", CPUPercent: 10, MemoryTotal: 64 * (1 << 30), MemoryUsed: 10 * (1 << 30), DiskTotal: 256 * (1 << 30), DiskUsed: 50 * (1 << 30)}
	b, _ := json.Marshal(hb)
	r := httptest.NewRequest("POST", "/api/v1/heartbeat", bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer "+cfg.AgentToken)
	w := httptest.NewRecorder()
	ctrl.Handler().ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("legacy sample rejected unexpectedly: HTTP %d", w.Code)
	}
	p, _ := resources.FindProfile("mc-lobby")
	preview := resources.Compute(ctrl.snapshotHosts(), p)
	if preview.RecommendedNode != "" || preview.Candidates[0].Admitted {
		t.Fatalf("fail-open legacy placement: %+v", preview)
	}
}

func TestRejectInvalidIOPressure(t *testing.T) {
	h := Heartbeat{NodeID: "host", Hostname: "host", CPUPercent: 20, MemoryTotal: 100, DiskTotal: 100, IOPressureAvailable: true, IOPressureSome10: 101}
	if validHeartbeat(h) {
		t.Fatal("invalid IO pressure accepted")
	}
	h.IOPressureSome10 = 1
	h.LegacyWingsChecked = true
	h.LegacyWingsObserved = true
	if !validHeartbeat(h) {
		t.Fatal("valid legacy observation rejected")
	}
}
