package control

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
