package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testKey = "ptlc_SECRETKEYVALUE123456"

const panelBody = `{"object":"list","data":[{"object":"server","attributes":{
"identifier":"abc12345","name":"Survival","node":"node-1","description":"owner Steve",
"sftp_details":{"ip":"203.0.113.9","port":2022},
"limits":{"memory":2048,"disk":10240,"cpu":200},
"relationships":{"allocations":{"data":[
{"attributes":{"ip":"203.0.113.9","ip_alias":"mc.example.net","port":25565,"is_default":true}},
{"attributes":{"ip":"203.0.113.9","port":25566}}]}}}}]}`

func newPanel(t *testing.T, methods *[]string, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*methods = append(*methods, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			http.Error(w, "nope "+testKey, http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, panelBody)
	}))
}

func TestCollectIsReadOnlyAndRedacted(t *testing.T) {
	var methods []string
	var mu sync.Mutex
	srv := newPanel(t, &methods, &mu)
	defer srv.Close()
	c, err := NewInventoryClient(srv.URL, testKey)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	inv, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range methods {
		if !strings.HasPrefix(m, "GET ") {
			t.Fatalf("non-read request: %s", m)
		}
	}
	if len(inv.Servers) != 1 || inv.Servers[0].ID != "abc12345" || inv.Servers[0].MemoryMB != 2048 {
		t.Fatalf("unexpected inventory %+v", inv)
	}
	if got := inv.Servers[0].Ports; len(got) != 2 || got[0] != 25565 || got[1] != 25566 {
		t.Fatalf("ports %v", got)
	}
	raw, _ := json.Marshal(inv)
	for _, secret := range []string{"203.0.113.9", "example.net", "Steve", testKey} {
		if strings.Contains(string(raw), secret) || strings.Contains(fmt.Sprintf("%+v %v", inv, c), secret) {
			t.Fatalf("leaked %q", secret)
		}
	}
	if !inv.CollectedAt.Equal(now) || inv.Stale(now.Add(time.Minute), time.Hour) || !inv.Stale(now.Add(2*time.Hour), time.Hour) {
		t.Fatal("staleness wrong")
	}
}

func TestWritesRefusedBeforeNetwork(t *testing.T) {
	var methods []string
	var mu sync.Mutex
	srv := newPanel(t, &methods, &mu)
	defer srv.Close()
	c, _ := NewInventoryClient(srv.URL, testKey)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req, _ := http.NewRequest(m, srv.URL+"/api/client/servers/abc/power", nil)
		if _, err := c.http.Transport.RoundTrip(req); err == nil {
			t.Fatalf("%s allowed", m)
		}
	}
	if len(methods) != 0 {
		t.Fatalf("network reached: %v", methods)
	}
}

func TestErrorsNeverContainKey(t *testing.T) {
	var methods []string
	var mu sync.Mutex
	srv := newPanel(t, &methods, &mu)
	defer srv.Close()
	c, _ := NewInventoryClient(srv.URL, "ptlc_WRONGKEY0000000000")
	_, err := c.Collect(context.Background())
	if err == nil || strings.Contains(err.Error(), "WRONGKEY") || strings.Contains(err.Error(), testKey) {
		t.Fatalf("err %v", err)
	}
}

func TestRejectsBadConfig(t *testing.T) {
	for _, u := range []string{"http://panel.example.com", "ftp://x", "", "https://u:p@panel.example.com"} {
		if _, err := NewInventoryClient(u, testKey); err == nil {
			t.Fatalf("accepted %q", u)
		}
	}
	if _, err := NewInventoryClient("https://panel.example.com", ""); err == nil {
		t.Fatal("accepted empty key")
	}
}
