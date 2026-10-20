package control

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/identity"
)

const architectPrefix = "/api/v1/architect/"
const discordFlowCookie = "metallic_discord_flow"

// ConfigureDiscord is a startup operation, before Handler serves requests.
// The current principal is the private controller administrator. A public
// multi-account service must supply real account/session storage before rollout.
func (c *Controller) ConfigureDiscord(provider identity.Provider, path string) error {
	if !c.cfg.ArchitectEnabled {
		return errors.New("Architect is disabled")
	}
	service, err := identity.Open(identity.Options{
		Provider: provider, StorePath: path, Now: func() time.Time { return c.now() },
		SessionActive: c.identitySessionActive,
	})
	if err != nil {
		return err
	}
	c.discord = service
	return nil
}

func (c *Controller) identitySessionActive(p identity.Principal) bool {
	if p.AccountID != c.cfg.AdminUser {
		return false
	}
	c.mu.RLock()
	s, ok := c.sessions[p.SessionID]
	c.mu.RUnlock()
	return ok && c.now().Before(s.expiry)
}
func (c *Controller) principal(r *http.Request) (identity.Principal, bool) {
	cookie, err := r.Cookie("metallic_session")
	if err != nil {
		return identity.Principal{}, false
	}
	p := identity.Principal{AccountID: c.cfg.AdminUser, SessionID: cookie.Value}
	return p, c.identitySessionActive(p)
}

func (c *Controller) architectHandler() http.Handler {
	if !c.cfg.ArchitectEnabled {
		return http.NotFoundHandler()
	}
	mux := http.NewServeMux()
	c.registerArchitectResources(mux)
	mux.HandleFunc("GET "+architectPrefix+"capabilities", c.requireAdmin(c.architectCapabilities))
	mux.HandleFunc("GET "+architectPrefix+"discord", c.requireAdmin(c.discordStatus))
	mux.HandleFunc("POST "+architectPrefix+"discord/start", c.requireAdmin(c.architectMutation(c.discordStart, true)))
	mux.HandleFunc("DELETE "+architectPrefix+"discord", c.requireAdmin(c.architectMutation(c.discordUnlink, true)))
	// SameSite=Strict protects the existing admin session. The OAuth callback
	// uses a separate, short-lived Lax cookie and validates the stored session.
	mux.HandleFunc("GET "+architectPrefix+"discord/callback", c.discordCallback)
	return mux
}
func (c *Controller) architectPage(w http.ResponseWriter, r *http.Request) {
	if !c.cfg.ArchitectEnabled {
		http.NotFound(w, r)
		return
	}
	c.requireAdmin(func(w http.ResponseWriter, r *http.Request) { c.serveFile(w, "web/architect.html") })(w, r)
}
func (c *Controller) architectCapabilities(w http.ResponseWriter, r *http.Request) {
	jsonReply(w, http.StatusOK, map[string]any{
		"product": "Metallic-MC Architect", "stage": "private-foundation",
		"hostingPersistent": c.hosts.Persistent(),
		"discordConfigured": c.discord != nil, "publicAccountService": false,
		"hostingExecution": false, "publicRelayDeployed": false,
		"pluginExecution": false, "minecraftAuthenticationConfigured": false,
	})
}
func (c *Controller) architectMutation(next http.HandlerFunc, recent bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Metallic-Intent") != "architect" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "same-origin action required", http.StatusForbidden)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" {
			expected := c.cfg.ArchitectOrigin
			if expected == "" {
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				expected = scheme + "://" + r.Host
			}
			u, err := url.Parse(origin)
			if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || origin != expected {
				http.Error(w, "origin rejected", http.StatusForbidden)
				return
			}
		}
		if recent {
			cookie, err := r.Cookie("metallic_session")
			if err != nil {
				c.deny(w, r)
				return
			}
			c.mu.RLock()
			s, ok := c.sessions[cookie.Value]
			c.mu.RUnlock()
			age := c.now().Sub(s.authenticatedAt)
			if !ok || s.authenticatedAt.IsZero() || age < 0 || age > 15*time.Minute {
				http.Error(w, "sign in again before changing account links or credentials", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}
func decodeArchitectJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "application/json" && !strings.HasPrefix(ct, "application/json;") {
		http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(value) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return false
	}
	var extra any
	if !errors.Is(dec.Decode(&extra), io.EOF) {
		http.Error(w, "trailing request data", http.StatusBadRequest)
		return false
	}
	return true
}
func jsonReply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (c *Controller) discordStatus(w http.ResponseWriter, r *http.Request) {
	if c.discord == nil {
		jsonReply(w, 200, map[string]any{"configured": false, "linked": false})
		return
	}
	link, ok := c.discord.Status(c.cfg.AdminUser)
	if !ok {
		jsonReply(w, 200, map[string]any{"configured": true, "linked": false})
		return
	}
	jsonReply(w, 200, map[string]any{"configured": true, "linked": true, "discordId": link.DiscordID, "username": link.Username, "linkedAt": link.LinkedAt})
}
func (c *Controller) discordStart(w http.ResponseWriter, r *http.Request) {
	if c.discord == nil {
		http.Error(w, "Discord application is not configured", http.StatusServiceUnavailable)
		return
	}
	var input struct{}
	if !decodeArchitectJSON(w, r, &input) {
		return
	}
	p, ok := c.principal(r)
	if !ok {
		c.deny(w, r)
		return
	}
	start, err := c.discord.Begin(p)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, identity.ErrCapacity) {
			status = http.StatusTooManyRequests
		}
		if errors.Is(err, identity.ErrAlreadyLinked) {
			status = http.StatusConflict
		}
		http.Error(w, "could not start account link", status)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: discordFlowCookie, Value: start.BrowserNonce, Path: architectPrefix + "discord/callback", HttpOnly: true, Secure: c.cfg.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: int(identity.FlowLifetime.Seconds()), Expires: start.ExpiresAt})
	jsonReply(w, http.StatusOK, start)
}
func (c *Controller) discordCallback(w http.ResponseWriter, r *http.Request) {
	if c.discord == nil {
		http.NotFound(w, r)
		return
	}
	cookie, err := r.Cookie(discordFlowCookie)
	http.SetCookie(w, &http.Cookie{Name: discordFlowCookie, Value: "", Path: architectPrefix + "discord/callback", HttpOnly: true, Secure: c.cfg.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	q, parseErr := url.ParseQuery(r.URL.RawQuery)
	if err != nil || parseErr != nil || len(q["state"]) != 1 || len(q["code"]) != 1 || len(q["iss"]) != 1 || q.Get("error") != "" {
		http.Error(w, "link could not be verified; return to Architect and try again", http.StatusBadRequest)
		return
	}
	_, err = c.discord.Complete(r.Context(), q.Get("state"), cookie.Value, q.Get("iss"), q.Get("code"))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, identity.ErrUnavailable) || errors.Is(err, identity.ErrStore) {
			status = http.StatusServiceUnavailable
		}
		if errors.Is(err, identity.ErrAlreadyLinked) {
			status = http.StatusConflict
		}
		http.Error(w, "link could not be completed; return to Architect and try again", status)
		return
	}
	// A same-origin link after this document avoids relying on Strict cookies
	// during an OAuth cross-site redirect chain.
	c.serveFile(w, "web/discord-result.html")
}
func (c *Controller) discordUnlink(w http.ResponseWriter, r *http.Request) {
	if c.discord == nil {
		http.Error(w, "Discord application is not configured", http.StatusServiceUnavailable)
		return
	}
	if err := c.discord.Unlink(c.cfg.AdminUser); err != nil {
		http.Error(w, "could not update account link", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
