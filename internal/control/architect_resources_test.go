package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oncoursemail/metallic-mc/internal/hosting"
)

func TestArchitectHostFlowAndCredentialBoundary(t *testing.T) {
	c, h, cookie, _ := architectFixture(t, true)
	pairing := architectRequest(h, "POST", architectPrefix+"hosts/pairing", "{}", cookie, true)
	if pairing.Code != 201 {
		t.Fatalf("pairing status %d: %s", pairing.Code, pairing.Body)
	}
	var ticket hosting.PairingTicket
	if err := json.Unmarshal(pairing.Body.Bytes(), &ticket); err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(map[string]string{"ticket": ticket.Token, "host_name": "Home server"})
	enrollment := architectRequest(h, "POST", architectPrefix+"hosts/redeem", string(request), nil, false)
	if enrollment.Code != 201 {
		t.Fatalf("enrollment status %d: %s", enrollment.Code, enrollment.Body)
	}
	var enrolled hosting.Enrollment
	if err := json.Unmarshal(enrollment.Body.Bytes(), &enrolled); err != nil {
		t.Fatal(err)
	}
	replay := architectRequest(h, "POST", architectPrefix+"hosts/redeem", string(request), nil, false)
	if replay.Code < 400 {
		t.Fatal("pairing ticket replay accepted")
	}
	listed := architectRequest(h, "GET", architectPrefix+"hosts", "", cookie, false)
	if listed.Code != 200 || strings.Contains(listed.Body.String(), enrolled.Credential) || strings.Contains(listed.Body.String(), ticket.Token) {
		t.Fatal("host listing leaked secrets")
	}
	allocation := hosting.AllocationRequest{HostID: enrolled.Host.ID, ServerID: "survival", GamePort: 25565, Resources: hosting.Resources{CPUCores: 2, MemoryMB: 2048, DiskMB: 10240, PlayerSlots: 20}}
	body, _ := json.Marshal(allocation)
	saved := architectRequest(h, "POST", architectPrefix+"servers", string(body), cookie, true)
	if saved.Code != 201 {
		t.Fatalf("allocation %d: %s", saved.Code, saved.Body)
	}
	route := hosting.RouteRequest{HostID: enrolled.Host.ID, ServerID: "survival", Target: "127.0.0.1:25565", TTLSeconds: 60}
	routeBody, _ := json.Marshal(route)
	doRoute := func(token string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://example.com"+architectPrefix+"routes/plan", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	denied := doRoute(c.cfg.AgentToken, routeBody)
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("shared telemetry token authorized host route: %d", denied.Code)
	}
	allowed := doRoute(enrolled.Credential, routeBody)
	if allowed.Code != 200 || !strings.Contains(allowed.Body.String(), "policy_allowed") {
		t.Fatalf("route plan %d: %s", allowed.Code, allowed.Body)
	}
	route.Target = "169.254.169.254:80"
	hostile, _ := json.Marshal(route)
	if doRoute(enrolled.Credential, hostile).Code < 400 {
		t.Fatal("metadata address route accepted")
	}
	revoked := architectRequest(h, "DELETE", architectPrefix+"hosts/"+enrolled.Host.ID, "", cookie, true)
	if revoked.Code != 204 {
		t.Fatalf("revoke %d", revoked.Code)
	}
	if doRoute(enrolled.Credential, routeBody).Code != http.StatusUnauthorized {
		t.Fatal("revoked credential authorized route")
	}
}

func TestArchitectHostRequestsCannotChooseOwner(t *testing.T) {
	_, h, cookie, _ := architectFixture(t, true)
	for _, body := range []string{`{"owner_id":"victim"}`, `{}{}`, `{"unexpected":true}`} {
		w := architectRequest(h, "POST", architectPrefix+"hosts/pairing", body, cookie, true)
		if w.Code != 400 {
			t.Fatalf("invalid body accepted: %d", w.Code)
		}
	}
	w := architectRequest(h, "POST", architectPrefix+"hosts/pairing", "{}", cookie, false)
	if w.Code != 403 {
		t.Fatalf("missing same-origin intent: %d", w.Code)
	}
	w = architectRequest(h, "POST", architectPrefix+"hosts/redeem", `{"ticket":"not-a-ticket","host_name":"host","owner_id":"victim"}`, nil, false)
	if w.Code != 400 {
		t.Fatalf("machine owner override accepted: %d", w.Code)
	}
}

func TestArchitectResourcesDisabledAndPrivate(t *testing.T) {
	_, h, cookie, _ := architectFixture(t, false)
	routes := []struct{ method, path, body string }{
		{"GET", "hosts", ""}, {"GET", "servers", ""}, {"GET", "catalog/search", ""},
		{"POST", "hosts/pairing", "{}"}, {"POST", "hosts/redeem", "{}"}, {"POST", "routes/plan", "{}"}, {"POST", "catalog/resolve", "{}"},
	}
	for _, route := range routes {
		w := architectRequest(h, route.method, architectPrefix+route.path, route.body, cookie, true)
		if w.Code != 404 {
			t.Fatalf("disabled %s: %d", route.path, w.Code)
		}
	}
	_, h, _, _ = architectFixture(t, true)
	for _, path := range []string{"hosts", "servers", "catalog/search"} {
		w := architectRequest(h, "GET", architectPrefix+path, "", nil, false)
		if w.Code != 401 {
			t.Fatalf("private %s accepted anonymous: %d", path, w.Code)
		}
	}
}
