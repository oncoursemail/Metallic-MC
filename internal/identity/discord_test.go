package identity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestMalformedTokenResponseStillRevokesRecoveredToken(t *testing.T) {
	for name, body := range map[string]string{
		"trailing-json":   `{"access_token":"fixture-access","token_type":"Bearer","scope":"identify"} {}`,
		"trailing-text":   `{"access_token":"fixture-access","token_type":"Bearer","scope":"identify"} rejected-upstream-body`,
		"scope-type":      `{"access_token":"fixture-access","token_type":"Bearer","scope":123}`,
		"token-type-type": `{"access_token":"fixture-access","token_type":[],"scope":"identify"}`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, cancelBrowser := range []bool{false, true} {
				ctx, cancel := context.WithCancel(context.Background())
				var requests, revocations int
				rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
					requests++
					switch r.URL.String() {
					case discordToken:
						res := response(http.StatusOK, body)
						if cancelBrowser {
							res.Body = &cancelOnCloseBody{ReadCloser: res.Body, cancel: cancel}
						}
						return res, nil
					case discordRevoke:
						revocations++
						deadline, bounded := r.Context().Deadline()
						if r.Context().Err() != nil || !bounded || time.Until(deadline) > 3*time.Second {
							t.Fatal("cleanup reused a cancelled or unbounded browser context")
						}
						if err := r.ParseForm(); err != nil {
							t.Fatal(err)
						}
						if r.Form.Get("token") != "fixture-access" || r.Form.Get("token_type_hint") != "access_token" {
							t.Fatal("cleanup did not revoke the recovered token")
						}
						return response(http.StatusOK, "{}"), nil
					default:
						t.Fatal("a malformed token response reached the profile endpoint")
						return nil, ErrUnavailable
					}
				})
				d, err := NewDiscord(configWithTransport(rt))
				if err != nil {
					t.Fatal(err)
				}
				profile, err := d.Resolve(ctx, "code", "verifier")
				cancel()
				if profile != (Profile{}) || !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "fixture-access") || strings.Contains(err.Error(), "rejected-upstream-body") {
					t.Fatal("malformed token response did not return an empty, redacted failure")
				}
				if requests != 2 || revocations != 1 {
					t.Fatalf("requests=%d, revocations=%d, cancelled=%v", requests, revocations, cancelBrowser)
				}
			}
		})
	}
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

func TestMalformedBearerTokensAreNotForwarded(t *testing.T) {
	for name, token := range map[string]string{
		"empty": "", "oversized": strings.Repeat("a", 4097),
		"crlf": "fixture\r\nInjected: value", "nul": "fixture\x00", "tab": "fixture\taccess",
		"space": "fixture access", "delete": "fixture\x7f", "unicode": "fixture-é",
		"comma": "fixture,access", "padding-only": "==", "middle-padding": "fixture=access",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"access_token": token, "token_type": "Bearer", "scope": "identify"})
			if err != nil {
				t.Fatal(err)
			}
			var requests int
			rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.String() != discordToken {
					t.Fatal("malformed token was forwarded to a downstream endpoint")
				}
				return response(http.StatusOK, string(body)), nil
			})
			d, err := NewDiscord(configWithTransport(rt))
			if err != nil {
				t.Fatal(err)
			}
			profile, err := d.Resolve(context.Background(), "code", "verifier")
			if profile != (Profile{}) || !errors.Is(err, ErrUnavailable) || err.Error() != ErrUnavailable.Error() || requests != 1 {
				t.Fatal("malformed token was not rejected without disclosure or forwarding")
			}
		})
	}
}

func TestBearerAlphabetAndPaddingPreserveNormalFlow(t *testing.T) {
	const token = "fixture.AZ09-_~+/=="
	var requests int
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		switch r.URL.String() {
		case discordToken:
			return response(http.StatusOK, `{"access_token":"`+token+`","token_type":"Bearer","scope":"identify"}`), nil
		case discordUser:
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Fatal("valid bearer token changed in the profile header")
			}
			return response(http.StatusOK, `{"id":"123456789012345678","username":"alex"}`), nil
		case discordRevoke:
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("token") != token {
				t.Fatal("valid bearer token changed in the revocation form")
			}
			return response(http.StatusOK, "{}"), nil
		default:
			t.Fatal("unexpected provider endpoint")
			return nil, ErrUnavailable
		}
	})
	d, err := NewDiscord(configWithTransport(rt))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := d.Resolve(context.Background(), "code", "verifier")
	if err != nil || profile.ID != "123456789012345678" || requests != 3 {
		t.Fatal("valid bearer alphabet or padding broke the normal linking flow")
	}
}
