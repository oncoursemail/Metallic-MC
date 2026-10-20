package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/hosting"
)

func TestHostingStateSurvivesControllerRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosting", "hosts.json")
	cfg := Config{AdminUser: "admin", AdminPassword: "averysecurepassword", AgentToken: "this-is-a-long-secret-token-for-test", ArchitectEnabled: true}
	open := func() (*Controller, http.Handler, *http.Cookie) {
		c := NewController(cfg)
		c.now = func() time.Time { return time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC) }
		if err := c.ConfigureHostingStore(path); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		h := c.Handler()
		return c, h, architectLogin(t, h)
	}
	c, h, cookie := open()
	var ticket hosting.PairingTicket
	w := architectRequest(h, "POST", architectPrefix+"hosts/pairing", "{}", cookie, true)
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &ticket) != nil {
		t.Fatalf("issue ticket: %d", w.Code)
	}
	request, _ := json.Marshal(map[string]string{"ticket": ticket.Token, "host_name": "Recovery fixture"})
	w = architectRequest(h, "POST", architectPrefix+"hosts/redeem", string(request), nil, false)
	var enrolled hosting.Enrollment
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &enrolled) != nil {
		t.Fatalf("enroll: %d", w.Code)
	}
	allocation, _ := json.Marshal(hosting.AllocationRequest{HostID: enrolled.Host.ID, ServerID: "recovery-lobby", GamePort: 25566, Resources: hosting.Resources{CPUCores: 1, MemoryMB: 1024, DiskMB: 2048, PlayerSlots: 4}})
	if w := architectRequest(h, "POST", architectPrefix+"servers", string(allocation), cookie, true); w.Code != 201 {
		t.Fatalf("allocate: %d", w.Code)
	}
	var pending hosting.PairingTicket
	w = architectRequest(h, "POST", architectPrefix+"hosts/pairing", "{}", cookie, true)
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &pending) != nil {
		t.Fatal("pending ticket")
	}
	// A controller restart must not revive a previous live telemetry claim.
	c.nodes["fixture"] = nodeRecord{heartbeat: Heartbeat{NodeID: "fixture", Hostname: "fixture"}, seen: c.now()}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c2, h2, cookie2 := open()
	if w := architectRequest(h2, "GET", architectPrefix+"hosts", "", cookie, false); w.Code != 401 {
		t.Fatal("old administrator session survived restart")
	}
	w = architectRequest(h2, "GET", architectPrefix+"hosts", "", cookie2, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"persistent":true`) || !strings.Contains(w.Body.String(), enrolled.Host.ID) || strings.Contains(w.Body.String(), enrolled.Credential) {
		t.Fatal("durable host inventory incorrect or credential exposed")
	}
	w = architectRequest(h2, "GET", architectPrefix+"servers", "", cookie2, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "recovery-lobby") || !strings.Contains(w.Body.String(), `"execution_enabled":false`) {
		t.Fatal("durable allocation missing or execution incorrectly advertised")
	}
	w = architectRequest(h2, "GET", "/api/v1/overview", "", cookie2, false)
	var overview struct {
		Nodes []Node `json:"nodes"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &overview) != nil || len(overview.Nodes) != 0 {
		t.Fatal("restart restored an unverified online node")
	}
	request, _ = json.Marshal(map[string]string{"ticket": pending.Token, "host_name": "Stale pairing"})
	if w := architectRequest(h2, "POST", architectPrefix+"hosts/redeem", string(request), nil, false); w.Code != 401 {
		t.Fatal("ephemeral pairing ticket survived restart")
	}
	route, _ := json.Marshal(hosting.RouteRequest{HostID: enrolled.Host.ID, ServerID: "recovery-lobby", Target: "127.0.0.1:25566", TTLSeconds: 60})
	plan := func(handler http.Handler) int {
		r := httptest.NewRequest("POST", "http://example.com"+architectPrefix+"routes/plan", strings.NewReader(string(route)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+enrolled.Credential)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	if plan(h2) != 200 {
		t.Fatal("unexpired host credential was lost during restart")
	}
	if w := architectRequest(h2, "DELETE", architectPrefix+"hosts/"+enrolled.Host.ID, "", cookie2, true); w.Code != 204 {
		t.Fatal("revoke failed")
	}
	if err := c2.Close(); err != nil {
		t.Fatal(err)
	}
	_, h3, _ := open()
	if plan(h3) != 401 {
		t.Fatal("restart revived a revoked host credential")
	}
}

func TestHostingStoreMustBeExplicitAndAvailable(t *testing.T) {
	c := NewController(Config{})
	if err := c.ConfigureHostingStore(filepath.Join(t.TempDir(), "hosting", "hosts.json")); err == nil {
		t.Fatal("disabled Architect acquired persistent storage")
	}
	c, h, cookie, _ := architectFixture(t, true)
	if err := c.ConfigureHostingStore(filepath.Join(t.TempDir(), "hosting", "hosts.json")); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	w := architectRequest(h, "POST", architectPrefix+"hosts/pairing", "{}", cookie, true)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable durable store returned %d, want 503", w.Code)
	}
}
