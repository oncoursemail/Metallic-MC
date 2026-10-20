package identity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const discordAuthorize = "https://discord.com/oauth2/authorize"
const discordToken = "https://discord.com/api/oauth2/token"
const discordUser = "https://discord.com/api/v10/users/@me"
const discordRevoke = "https://discord.com/api/oauth2/token/revoke"

type DiscordConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Client       *http.Client
}

type Discord struct {
	cfg    DiscordConfig
	client *http.Client
}

var snowflake = regexp.MustCompile("^[1-9][0-9]{16,19}$")

func NewDiscord(cfg DiscordConfig) (*Discord, error) {
	u, err := url.Parse(cfg.RedirectURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/api/v1/architect/discord/callback" {
		return nil, errors.New("Discord requires an exact registered callback URL")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("Discord callback must use HTTPS or an explicit loopback IP")
		}
	}
	if !snowflake.MatchString(cfg.ClientID) || len(cfg.ClientSecret) < 16 || len(cfg.ClientSecret) > 512 {
		return nil, errors.New("valid Discord application credentials required")
	}
	c := http.Client{Timeout: 10 * time.Second}
	if cfg.Client != nil {
		c = *cfg.Client
	}
	if c.Timeout <= 0 || c.Timeout > 10*time.Second {
		c.Timeout = 10 * time.Second
	}
	// Credential-bearing requests must never follow a provider redirect.
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Discord{cfg: cfg, client: &c}, nil
}

func (d *Discord) AuthorizationURL(state, challenge string) string {
	v := url.Values{
		"client_id": {d.cfg.ClientID}, "redirect_uri": {d.cfg.RedirectURL},
		"response_type": {"code"}, "scope": {"identify"}, "prompt": {"consent"},
		"state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	return discordAuthorize + "?" + v.Encode()
}

func (d *Discord) form(ctx context.Context, endpoint string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, ErrUnavailable
	}
	req.SetBasicAuth(d.cfg.ClientID, d.cfg.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MetallicArchitect/0.1 (+https://github.com/oncoursemail/metallic-mc)")
	res, err := d.client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	return res, nil
}

func decodeBounded(res *http.Response, out any) error {
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 16*1024+1))
	if err != nil || len(b) > 16*1024 {
		return ErrUnavailable
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	if dec.Decode(out) != nil {
		return ErrUnavailable
	}
	var extra any
	if !errors.Is(dec.Decode(&extra), io.EOF) {
		return ErrUnavailable
	}
	return nil
}

// Resolve uses only identify, verifies /users/@me, and revokes the resulting
// authorization. No access or refresh token is returned to the caller or stored.
// Any usable token recovered by bounded decoding is revoked even if the rest of
// the response is rejected. Cleanup uses a separately bounded context so a
// cancelled browser request does not skip it.
func (d *Discord) Resolve(ctx context.Context, code, verifier string) (profile Profile, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := d.form(ctx, discordToken, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {d.cfg.RedirectURL}, "code_verifier": {verifier},
	})
	if err != nil {
		return Profile{}, ErrUnavailable
	}
	var grant struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
	}
	decodeErr := decodeBounded(res, &grant)
	if !validBearerToken(grant.AccessToken) {
		return Profile{}, ErrUnavailable
	}
	defer func() {
		revokeCtx, revokeCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer revokeCancel()
		response, revokeErr := d.form(revokeCtx, discordRevoke, url.Values{"token": {grant.AccessToken}, "token_type_hint": {"access_token"}})
		if revokeErr != nil {
			profile = Profile{}
			err = ErrUnavailable
			return
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			profile = Profile{}
			err = ErrUnavailable
		}
	}()
	if decodeErr != nil || !strings.EqualFold(grant.TokenType, "Bearer") || !containsScope(grant.Scope, "identify") {
		return Profile{}, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discordUser, nil)
	if err != nil {
		return Profile{}, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+grant.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MetallicArchitect/0.1 (+https://github.com/oncoursemail/metallic-mc)")
	res, err = d.client.Do(req)
	if err != nil {
		return Profile{}, ErrUnavailable
	}
	if decodeBounded(res, &profile) != nil || !validProfile(profile) {
		return Profile{}, ErrUnavailable
	}
	return profile, nil
}

// RFC 6750 section 2.1 permits the b64token alphabet with trailing '=' padding.
// Reject malformed tokens before they reach either an Authorization header or
// revocation form, including whitespace, controls and non-ASCII characters.
func validBearerToken(token string) bool {
	if len(token) == 0 || len(token) > 4096 {
		return false
	}
	padding := false
	for i := 0; i < len(token); i++ {
		c := token[i]
		if c == '=' {
			if i == 0 {
				return false
			}
			padding = true
			continue
		}
		if padding {
			return false
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("-._~+/", rune(c)) {
			continue
		}
		return false
	}
	return true
}

func containsScope(list, want string) bool {
	for _, v := range strings.Fields(list) {
		if v == want {
			return true
		}
	}
	return false
}
