package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/identity"
)

type discordFixture struct{ calls atomic.Int32 }

func (f *discordFixture) AuthorizationURL(state, challenge string) string {
	return "https://discord.com/oauth2/authorize?" + url.Values{"state": {state}, "code_challenge": {challenge}, "scope": {"identify"}}.Encode()
}
func (f *discordFixture) Resolve(context.Context, string, string) (identity.Profile, error) {
	f.calls.Add(1)
	return identity.Profile{ID: "123456789012345678", Username: "alex"}, nil
}
func architectFixture(t *testing.T, enabled bool) (*Controller, http.Handler, *http.Cookie, *discordFixture) {
	t.Helper()
	c := NewController(Config{AdminUser: "admin", AdminPassword: "averysecurepassword", AgentToken: "this-is-a-long-secret-token-for-test", ArchitectEnabled: enabled})
	c.now = func() time.Time { return time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC) }
	f := &discordFixture{}
	if enabled {
		if err := c.ConfigureDiscord(f, filepath.Join(t.TempDir(), "links.json")); err != nil {
			t.Fatal(err)
		}
	}
	h := c.Handler()
	return c, h, architectLogin(t, h), f
}
func architectLogin(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	r := httptest.NewRequest("POST", "http://example.com/login", strings.NewReader(url.Values{"username": {"admin"}, "password": {"averysecurepassword"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 303 {
		t.Fatalf("login %d %s", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}
func architectRequest(h http.Handler, method, path, body string, cookie *http.Cookie, intent bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://example.com"+path, strings.NewReader(body))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if intent {
		r.Header.Set("X-Metallic-Intent", "architect")
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestArchitectAbsentByDefaultAndAdminOnly(t *testing.T) {
	_, h, cookie, _ := architectFixture(t, false)
	for _, path := range []string{"/architect", architectPrefix + "capabilities", architectPrefix + "discord", architectPrefix + "discord/callback"} {
		w := architectRequest(h, "GET", path, "", cookie, false)
		if w.Code != 404 {
			t.Fatalf("%s enabled by default: %d", path, w.Code)
		}
	}
	_, h, _, _ = architectFixture(t, true)
	for _, path := range []string{architectPrefix + "capabilities", architectPrefix + "discord"} {
		w := architectRequest(h, "GET", path, "", nil, false)
		if w.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", path, w.Code)
		}
	}
}
func TestArchitectMutationsRejectCSRFAndUntrustedIdentity(t *testing.T) {
	_, h, cookie, f := architectFixture(t, true)
	w := architectRequest(h, "POST", architectPrefix+"discord/start", "{}", cookie, false)
	if w.Code != 403 {
		t.Fatalf("missing intent: %d", w.Code)
	}
	r := httptest.NewRequest("POST", "http://example.com"+architectPrefix+"discord/start", strings.NewReader("{}"))
	r.AddCookie(cookie)
	r.Header.Set("X-Metallic-Intent", "architect")
	r.Header.Set("Origin", "https://attacker.invalid")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("foreign origin %d", w.Code)
	}
	w = architectRequest(h, "POST", architectPrefix+"discord/start", "{\"accountId\":\"another-user\"}", cookie, true)
	if w.Code != 400 {
		t.Fatalf("untrusted account accepted: %d", w.Code)
	}
	if f.calls.Load() != 0 {
		t.Fatal("invalid start contacted provider")
	}
}
func TestDiscordCallbackWorksWithoutStrictAdminCookie(t *testing.T) {
	c, h, cookie, f := architectFixture(t, true)
	w := architectRequest(h, "POST", architectPrefix+"discord/start", "{}", cookie, true)
	if w.Code != 200 {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	var start identity.Start
	if err := json.Unmarshal(w.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(start.AuthorizationURL)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != architectPrefix+"discord/callback" {
		t.Fatalf("flow cookie: %+v", cookies)
	}
	if strings.Contains(w.Body.String(), cookies[0].Value) {
		t.Fatal("browser binding leaked into JSON")
	}
	callback := architectPrefix + "discord/callback?" + url.Values{"state": {u.Query().Get("state")}, "iss": {"https://discord.com"}, "code": {"code"}}.Encode()
	r := httptest.NewRequest("GET", "http://example.com"+callback, nil)
	r.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Discord is linked") {
		t.Fatalf("callback %d %s", w.Code, w.Body.String())
	}
	if f.calls.Load() != 1 {
		t.Fatal("exchange count")
	}
	w = architectRequest(h, "GET", architectPrefix+"discord", "", cookie, false)
	if !strings.Contains(w.Body.String(), "123456789012345678") || !strings.Contains(w.Body.String(), "\"linked\":true") {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 || f.calls.Load() != 1 {
		t.Fatal("callback replay succeeded")
	}
	c.now = func() time.Time { return time.Date(2026, 10, 19, 12, 16, 0, 0, time.UTC) }
	w = architectRequest(h, "DELETE", architectPrefix+"discord", "", cookie, true)
	if w.Code != 401 {
		t.Fatalf("old session unlinked: %d", w.Code)
	}
	fresh := architectLogin(t, h)
	w = architectRequest(h, "DELETE", architectPrefix+"discord", "", fresh, true)
	if w.Code != 204 {
		t.Fatalf("fresh unlink: %d %s", w.Code, w.Body.String())
	}
}
func TestLogoutInvalidatesDiscordFlow(t *testing.T) {
	_, h, cookie, f := architectFixture(t, true)
	w := architectRequest(h, "POST", architectPrefix+"discord/start", "{}", cookie, true)
	var start identity.Start
	json.Unmarshal(w.Body.Bytes(), &start)
	u, _ := url.Parse(start.AuthorizationURL)
	nonce := w.Result().Cookies()[0]
	architectRequest(h, "POST", "/logout", "", cookie, false)
	r := httptest.NewRequest("GET", "http://example.com"+architectPrefix+"discord/callback?"+url.Values{"state": {u.Query().Get("state")}, "iss": {"https://discord.com"}, "code": {"code"}}.Encode(), nil)
	r.AddCookie(nonce)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 || f.calls.Load() != 0 {
		t.Fatalf("logged-out flow accepted %d", w.Code)
	}
}
func TestUnconfiguredDiscordIsHonest(t *testing.T) {
	c := NewController(Config{AdminUser: "admin", AdminPassword: "averysecurepassword", AgentToken: "this-is-a-long-secret-token-for-test", ArchitectEnabled: true})
	h := c.Handler()
	cookie := architectLogin(t, h)
	w := architectRequest(h, "GET", architectPrefix+"discord", "", cookie, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "\"configured\":false") {
		t.Fatal(w.Body.String())
	}
	w = architectRequest(h, "POST", architectPrefix+"discord/start", "{}", cookie, true)
	if w.Code != 503 {
		t.Fatalf("unconfigured link %d", w.Code)
	}
}
