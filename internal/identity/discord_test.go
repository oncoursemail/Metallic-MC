package identity

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func configWithTransport(rt http.RoundTripper) DiscordConfig {
	return DiscordConfig{ClientID: "123456789012345678", ClientSecret: "fixture-secret-do-not-use", RedirectURL: "https://control.example/api/v1/architect/discord/callback", Client: &http.Client{Transport: rt}}
}
func TestDiscordMinimalScopeAndPKCEURL(t *testing.T) {
	d, err := NewDiscord(configWithTransport(nil))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(d.AuthorizationURL("state", "challenge"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("scope") != "identify" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != "challenge" || q.Get("state") != "state" || q.Get("response_type") != "code" {
		t.Fatal("unsafe authorization parameters")
	}
	if strings.Contains(u.String(), d.cfg.ClientSecret) {
		t.Fatal("secret in URL")
	}
}
func TestDiscordProfileGrantRevokedAndNoRedirect(t *testing.T) {
	var revoked atomic.Bool
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "discord.com" {
			t.Fatal("unexpected credential origin")
		}
		switch r.URL.Path {
		case "/api/oauth2/token":
			if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Fatal("invalid token request")
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			id, secret, ok := r.BasicAuth()
			if !ok || id != "123456789012345678" || secret != "fixture-secret-do-not-use" {
				t.Fatal("bad client auth")
			}
			if r.Form.Get("code_verifier") != "verifier" || r.Form.Get("code") != "code" || r.Form.Get("grant_type") != "authorization_code" {
				t.Fatal("invalid code exchange")
			}
			return response(200, "{\"access_token\":\"fixture-access\",\"refresh_token\":\"fixture-refresh\",\"token_type\":\"Bearer\",\"scope\":\"identify\"}"), nil
		case "/api/v10/users/@me":
			if r.Header.Get("Authorization") != "Bearer fixture-access" {
				t.Fatal("missing bearer")
			}
			return response(200, "{\"id\":\"123456789012345678\",\"username\":\"alex\",\"email\":\"ignored\"}"), nil
		case "/api/oauth2/token/revoke":
			r.ParseForm()
			if r.Form.Get("token") != "fixture-access" {
				t.Fatal("wrong revoked token")
			}
			revoked.Store(true)
			return response(200, "{}"), nil
		default:
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
		}
		return nil, errors.New("unreachable")
	})
	d, err := NewDiscord(configWithTransport(rt))
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.Resolve(context.Background(), "code", "verifier")
	if err != nil || p.ID != "123456789012345678" || !revoked.Load() {
		t.Fatalf("profile: %+v %v revoked=%v", p, err, revoked.Load())
	}
	if !errors.Is(d.client.CheckRedirect(&http.Request{}, nil), http.ErrUseLastResponse) {
		t.Fatal("redirects permitted")
	}
}
func TestProviderFailuresAreRedactedAndStillRevoke(t *testing.T) {
	for _, kind := range []string{"profile-error", "oversize-profile", "invalid-profile", "revocation-error", "token-redirect", "token-oversize"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			var revoked atomic.Bool
			rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests.Add(1)
				switch r.URL.Path {
				case "/api/oauth2/token":
					if kind == "token-redirect" {
						res := response(302, "private-upstream-body")
						res.Header.Set("Location", "https://attacker.invalid")
						return res, nil
					}
					if kind == "token-oversize" {
						return response(200, strings.Repeat("x", 17000)), nil
					}
					return response(200, "{\"access_token\":\"fixture-access\",\"token_type\":\"Bearer\",\"scope\":\"identify\"}"), nil
				case "/api/v10/users/@me":
					switch kind {
					case "profile-error":
						return response(500, "private-upstream-body"), nil
					case "oversize-profile":
						return response(200, strings.Repeat("x", 17000)), nil
					case "invalid-profile":
						return response(200, "{\"id\":\"not-an-id\",\"username\":\"alex\"}"), nil
					default:
						return response(200, "{\"id\":\"123456789012345678\",\"username\":\"alex\"}"), nil
					}
				case "/api/oauth2/token/revoke":
					revoked.Store(true)
					if kind == "revocation-error" {
						return response(500, "private-upstream-body"), nil
					}
					return response(200, "{}"), nil
				}
				t.Fatalf("unexpected provider request")
				return nil, ErrUnavailable
			})
			d, err := NewDiscord(configWithTransport(rt))
			if err != nil {
				t.Fatal(err)
			}
			_, err = d.Resolve(context.Background(), "code", "verifier")
			if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe error: %v", err)
			}
			if strings.HasPrefix(kind, "token-") {
				if requests.Load() != 1 {
					t.Fatal("redirect/oversize reached another endpoint")
				}
			} else if !revoked.Load() {
				t.Fatal("failed profile skipped revocation")
			}
		})
	}
}
func TestRejectsUnsafeCallbackConfiguration(t *testing.T) {
	for _, redirect := range []string{"http://control.example/api/v1/architect/discord/callback", "https://user:password@control.example/api/v1/architect/discord/callback", "https://control.example/elsewhere", "https://control.example/api/v1/architect/discord/callback?next=bad", "javascript:bad", "http://localhost/api/v1/architect/discord/callback"} {
		cfg := configWithTransport(nil)
		cfg.RedirectURL = redirect
		if _, err := NewDiscord(cfg); err == nil {
			t.Fatalf("accepted %q", redirect)
		}
	}
	for _, redirect := range []string{"http://127.0.0.1:8080/api/v1/architect/discord/callback", "http://[::1]:8080/api/v1/architect/discord/callback"} {
		cfg := configWithTransport(nil)
		cfg.RedirectURL = redirect
		if _, err := NewDiscord(cfg); err != nil {
			t.Fatalf("loopback %v", err)
		}
	}
}
