// Package identity implements optional, identity-only Discord account linking.
// It does not send messages, join guilds, grant roles, or authenticate Minecraft.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidFlow   = errors.New("invalid or expired linking flow")
	ErrUnavailable   = errors.New("identity provider unavailable")
	ErrAlreadyLinked = errors.New("account already linked")
	ErrCapacity      = errors.New("linking capacity reached")
	ErrStore         = errors.New("identity store unavailable")
)

const FlowLifetime = 5 * time.Minute
const maxStoreBytes = 1 << 20

type Principal struct {
	AccountID string
	SessionID string
}

type Profile struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Bot      bool   `json:"bot"`
}

type Provider interface {
	AuthorizationURL(state, challenge string) string
	Resolve(context.Context, string, string) (Profile, error)
}

type Options struct {
	Provider  Provider
	StorePath string
	Now       func() time.Time
	// SessionActive must consult the trusted session store, never request fields.
	SessionActive func(Principal) bool
	MaxPending    int
}

type Link struct {
	AccountID string    `json:"accountId"`
	DiscordID string    `json:"discordId"`
	Username  string    `json:"username"`
	LinkedAt  time.Time `json:"linkedAt"`
}

type Start struct {
	AuthorizationURL string    `json:"authorizationUrl"`
	BrowserNonce     string    `json:"-"`
	ExpiresAt        time.Time `json:"expiresAt"`
}

type transaction struct {
	principal  Principal
	nonceHash  [32]byte
	verifier   string
	expires    time.Time
	exchanging bool
}

type Service struct {
	mu      sync.Mutex
	opts    Options
	pending map[[32]byte]*transaction
	links   map[string]Link
}

func Open(opts Options) (*Service, error) {
	if opts.Provider == nil || opts.SessionActive == nil || !filepath.IsAbs(opts.StorePath) {
		return nil, errors.New("provider, session validator, and absolute identity store path required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.MaxPending == 0 {
		opts.MaxPending = 64
	}
	if opts.MaxPending < 1 || opts.MaxPending > 1024 {
		return nil, errors.New("invalid pending limit")
	}
	s := &Service{opts: opts, pending: make(map[[32]byte]*transaction), links: make(map[string]Link)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func validAccount(v string) bool {
	return len(v) > 0 && len(v) <= 128 && !strings.ContainsAny(v, "\x00\r\n\t")
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", ErrUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func (s *Service) prune(now time.Time) {
	for key, t := range s.pending {
		if !now.Before(t.expires) {
			delete(s.pending, key)
		}
	}
}

func (s *Service) Begin(p Principal) (Start, error) {
	if !validAccount(p.AccountID) || len(p.SessionID) == 0 || len(p.SessionID) > 256 || !s.opts.SessionActive(p) {
		return Start{}, ErrInvalidFlow
	}
	state, err := randomToken()
	if err != nil {
		return Start{}, err
	}
	verifier, err := randomToken()
	if err != nil {
		return Start{}, err
	}
	nonce, err := randomToken()
	if err != nil {
		return Start{}, err
	}
	now := s.opts.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(now)
	if _, ok := s.links[p.AccountID]; ok {
		return Start{}, ErrAlreadyLinked
	}
	// Only one browser-bound transaction can own the flow cookie per session.
	for key, t := range s.pending {
		if t.principal.SessionID == p.SessionID {
			delete(s.pending, key)
		}
	}
	if len(s.pending) >= s.opts.MaxPending {
		return Start{}, ErrCapacity
	}
	t := &transaction{principal: p, nonceHash: sha256.Sum256([]byte(nonce)), verifier: verifier, expires: now.Add(FlowLifetime)}
	s.pending[sha256.Sum256([]byte(state))] = t
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	return Start{AuthorizationURL: s.opts.Provider.AuthorizationURL(state, challenge), BrowserNonce: nonce, ExpiresAt: t.expires}, nil
}

func (s *Service) Complete(ctx context.Context, state, nonce, issuer, code string) (Link, error) {
	if len(state) != 43 || len(nonce) != 43 || issuer != "https://discord.com" || len(code) == 0 || len(code) > 2048 {
		return Link{}, ErrInvalidFlow
	}
	key, nh := sha256.Sum256([]byte(state)), sha256.Sum256([]byte(nonce))
	s.mu.Lock()
	s.prune(s.opts.Now())
	t, ok := s.pending[key]
	if !ok || t.exchanging || subtle.ConstantTimeCompare(nh[:], t.nonceHash[:]) != 1 || !s.opts.SessionActive(t.principal) {
		s.mu.Unlock()
		return Link{}, ErrInvalidFlow
	}
	t.exchanging = true // Claim before doing I/O: concurrent callbacks cannot exchange twice.
	s.mu.Unlock()
	profile, err := s.opts.Provider.Resolve(ctx, code, t.verifier)
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.pending[key]
	if !exists || current != t {
		return Link{}, ErrInvalidFlow
	}
	delete(s.pending, key)
	if err != nil {
		return Link{}, ErrUnavailable
	}
	if !s.opts.Now().Before(t.expires) || !s.opts.SessionActive(t.principal) || ctx.Err() != nil {
		return Link{}, ErrInvalidFlow
	}
	if !validProfile(profile) {
		return Link{}, ErrUnavailable
	}
	if _, exists := s.links[t.principal.AccountID]; exists {
		return Link{}, ErrAlreadyLinked
	}
	for _, link := range s.links {
		if link.DiscordID == profile.ID {
			return Link{}, ErrAlreadyLinked
		}
	}
	if len(s.links) >= 10000 {
		return Link{}, ErrCapacity
	}
	link := Link{AccountID: t.principal.AccountID, DiscordID: profile.ID, Username: profile.Username, LinkedAt: s.opts.Now().UTC()}
	next := copyLinks(s.links)
	next[link.AccountID] = link
	if err := s.persist(next); err != nil {
		return Link{}, ErrStore
	}
	s.links = next
	return link, nil
}

func validProfile(p Profile) bool {
	if p.Bot || len(p.ID) < 17 || len(p.ID) > 20 || p.ID[0] == '0' || len(p.Username) == 0 || len(p.Username) > 128 || strings.ContainsAny(p.Username, "\x00\r\n") {
		return false
	}
	for _, c := range p.ID {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (s *Service) Status(accountID string) (Link, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[accountID]
	return l, ok
}

func (s *Service) CancelSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, t := range s.pending {
		if t.principal.SessionID == sessionID {
			delete(s.pending, key)
		}
	}
}

// Unlink is called only after the HTTP layer requires recent reauthentication.
// Identity-only provider grants are revoked during Resolve and are never stored.
func (s *Service) Unlink(accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.links[accountID]; !ok {
		return nil
	}
	next := copyLinks(s.links)
	delete(next, accountID)
	if err := s.persist(next); err != nil {
		return ErrStore
	}
	s.links = next
	for key, t := range s.pending {
		if t.principal.AccountID == accountID {
			delete(s.pending, key)
		}
	}
	return nil
}

func copyLinks(src map[string]Link) map[string]Link {
	out := make(map[string]Link, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

type diskState struct {
	Version int    `json:"version"`
	Links   []Link `json:"links"`
}

func (s *Service) load() error {
	info, err := os.Lstat(s.opts.StorePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxStoreBytes {
		return ErrStore
	}
	f, err := os.Open(s.opts.StorePath)
	if err != nil {
		return ErrStore
	}
	defer f.Close()
	var state diskState
	dec := json.NewDecoder(io.LimitReader(f, maxStoreBytes+1))
	dec.DisallowUnknownFields()
	if dec.Decode(&state) != nil || state.Version != 1 || len(state.Links) > 10000 {
		return ErrStore
	}
	var extra any
	if !errors.Is(dec.Decode(&extra), io.EOF) {
		return ErrStore
	}
	ids := make(map[string]bool)
	for _, l := range state.Links {
		if !validAccount(l.AccountID) || !validProfile(Profile{ID: l.DiscordID, Username: l.Username}) || l.LinkedAt.IsZero() || ids[l.DiscordID] {
			return ErrStore
		}
		if _, duplicate := s.links[l.AccountID]; duplicate {
			return ErrStore
		}
		ids[l.DiscordID] = true
		s.links[l.AccountID] = l
	}
	return nil
}

// The file store is intended for one private controller process. Public,
// replicated identity requires transactional storage and shared session state.
func (s *Service) persist(links map[string]Link) error {
	state := diskState{Version: 1, Links: make([]Link, 0, len(links))}
	for _, l := range links {
		state.Links = append(state.Links, l)
	}
	sort.Slice(state.Links, func(i, j int) bool { return state.Links[i].AccountID < state.Links[j].AccountID })
	b, err := json.Marshal(state)
	if err != nil || len(b) > maxStoreBytes {
		return ErrStore
	}
	dir := filepath.Dir(s.opts.StorePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ErrStore
	}
	if info, err := os.Lstat(s.opts.StorePath); err == nil && !info.Mode().IsRegular() {
		return ErrStore
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrStore
	}
	f, err := os.CreateTemp(dir, ".discord-links-*")
	if err != nil {
		return ErrStore
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return ErrStore
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return ErrStore
	}
	if err = f.Close(); err != nil {
		return ErrStore
	}
	if err = os.Rename(tmp, s.opts.StorePath); err != nil {
		return ErrStore
	}
	return nil
}
