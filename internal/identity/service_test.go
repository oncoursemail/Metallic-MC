package identity

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeProvider struct {
	id        string
	calls     atomic.Int32
	started   chan struct{}
	release   chan struct{}
	challenge string
}

func (f *fakeProvider) AuthorizationURL(state, challenge string) string {
	f.challenge = challenge
	return "https://discord.com/oauth2/authorize?state=" + url.QueryEscape(state)
}
func (f *fakeProvider) Resolve(ctx context.Context, code, verifier string) (Profile, error) {
	f.calls.Add(1)
	d := sha256.Sum256([]byte(verifier))
	if f.challenge != "" && f.challenge != base64.RawURLEncoding.EncodeToString(d[:]) {
		return Profile{}, errors.New("PKCE mismatch")
	}
	if f.started != nil {
		close(f.started)
		select {
		case <-f.release:
		case <-ctx.Done():
			return Profile{}, ctx.Err()
		}
	}
	return Profile{ID: f.id, Username: "metallic-test"}, nil
}
func testService(t *testing.T, f *fakeProvider) (*Service, *time.Time, *atomic.Bool) {
	t.Helper()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	active := &atomic.Bool{}
	active.Store(true)
	s, err := Open(Options{Provider: f, StorePath: filepath.Join(t.TempDir(), "identity", "links.json"), Now: func() time.Time { return now }, SessionActive: func(Principal) bool { return active.Load() }})
	if err != nil {
		t.Fatal(err)
	}
	return s, &now, active
}
func begin(t *testing.T, s *Service, account string) (Start, string) {
	t.Helper()
	b, err := s.Begin(Principal{AccountID: account, SessionID: "session-" + account})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(b.AuthorizationURL)
	return b, u.Query().Get("state")
}
func TestLinkPersistsIdentityWithoutOAuthSecrets(t *testing.T) {
	f := &fakeProvider{id: "123456789012345678"}
	s, _, _ := testService(t, f)
	b, state := begin(t, s, "alex")
	l, err := s.Complete(context.Background(), state, b.BrowserNonce, "https://discord.com", "private-code")
	if err != nil || l.DiscordID != f.id {
		t.Fatalf("link: %+v %v", l, err)
	}
	if _, err = s.Complete(context.Background(), state, b.BrowserNonce, "https://discord.com", "private-code"); !errors.Is(err, ErrInvalidFlow) {
		t.Fatalf("replay: %v", err)
	}
	reopened, err := Open(s.opts)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reopened.Status("alex"); !ok || got.DiscordID != f.id {
		t.Fatalf("persisted: %+v %v", got, ok)
	}
	raw, err := os.ReadFile(s.opts.StorePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{state, b.BrowserNonce, "private-code", "session-alex", "access_token", "refresh_token"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("secret persisted: %q", secret)
		}
	}
	info, _ := os.Stat(s.opts.StorePath)
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("identity file permissions: %v", info.Mode())
	}
}
func TestRejectsWrongBrowserIssuerAndExpiredSession(t *testing.T) {
	for _, kind := range []string{"browser", "issuer", "expired", "session"} {
		t.Run(kind, func(t *testing.T) {
			f := &fakeProvider{id: "123456789012345678"}
			s, now, active := testService(t, f)
			b, state := begin(t, s, "alex")
			issuer := "https://discord.com"
			switch kind {
			case "browser":
				b.BrowserNonce = strings.Repeat("x", 43)
			case "issuer":
				issuer = "https://attacker.invalid"
			case "expired":
				*now = now.Add(FlowLifetime)
			case "session":
				active.Store(false)
			}
			if _, err := s.Complete(context.Background(), state, b.BrowserNonce, issuer, "code"); !errors.Is(err, ErrInvalidFlow) {
				t.Fatalf("got %v", err)
			}
			if f.calls.Load() != 0 {
				t.Fatal("invalid callback reached provider")
			}
		})
	}
}
func TestConcurrentCallbackExchangesOnlyOnce(t *testing.T) {
	f := &fakeProvider{id: "123456789012345678", started: make(chan struct{}), release: make(chan struct{})}
	s, _, _ := testService(t, f)
	b, state := begin(t, s, "alex")
	done := make(chan error, 1)
	go func() {
		_, err := s.Complete(context.Background(), state, b.BrowserNonce, "https://discord.com", "code")
		done <- err
	}()
	<-f.started
	if _, err := s.Complete(context.Background(), state, b.BrowserNonce, "https://discord.com", "code"); !errors.Is(err, ErrInvalidFlow) {
		t.Fatalf("concurrent replay: %v", err)
	}
	close(f.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 1 {
		t.Fatal("duplicate exchange")
	}
}
func TestLogoutCancelsInFlightExchange(t *testing.T) {
	f := &fakeProvider{id: "123456789012345678", started: make(chan struct{}), release: make(chan struct{})}
	s, _, active := testService(t, f)
	b, state := begin(t, s, "alex")
	done := make(chan error, 1)
	go func() {
		_, err := s.Complete(context.Background(), state, b.BrowserNonce, "https://discord.com", "code")
		done <- err
	}()
	<-f.started
	active.Store(false)
	s.CancelSession("session-alex")
	close(f.release)
	if err := <-done; !errors.Is(err, ErrInvalidFlow) {
		t.Fatalf("cancel: %v", err)
	}
	if _, ok := s.Status("alex"); ok {
		t.Fatal("linked after logout")
	}
}
func TestDiscordIdentityCannotLinkTwoAccounts(t *testing.T) {
	f := &fakeProvider{id: "123456789012345678"}
	s, _, _ := testService(t, f)
	b, state := begin(t, s, "alex")
	if _, err := s.Complete(context.Background(), state, b.BrowserNonce, "https://discord.com", "code"); err != nil {
		t.Fatal(err)
	}
	b, state = begin(t, s, "someone-else")
	if _, err := s.Complete(context.Background(), state, b.BrowserNonce, "https://discord.com", "code"); !errors.Is(err, ErrAlreadyLinked) {
		t.Fatalf("collision: %v", err)
	}
	if _, ok := s.Status("someone-else"); ok {
		t.Fatal("cross-account link")
	}
	if err := s.Unlink("alex"); err != nil {
		t.Fatal(err)
	}
	b, state = begin(t, s, "someone-else")
	if _, err := s.Complete(context.Background(), state, b.BrowserNonce, "https://discord.com", "code"); err != nil {
		t.Fatal(err)
	}
}
func TestFailedPersistenceDoesNotCommitLink(t *testing.T) {
	f := &fakeProvider{id: "123456789012345678"}
	s, _, _ := testService(t, f)
	if err := os.WriteFile(filepath.Dir(s.opts.StorePath), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	b, state := begin(t, s, "alex")
	if _, err := s.Complete(context.Background(), state, b.BrowserNonce, "https://discord.com", "code"); !errors.Is(err, ErrStore) {
		t.Fatalf("store: %v", err)
	}
	if _, ok := s.Status("alex"); ok {
		t.Fatal("persist failure committed")
	}
}
func TestCorruptAndDuplicateStoresRejected(t *testing.T) {
	for _, raw := range []string{
		"{broken",
		"{\"version\":2,\"links\":[]}",
		"{\"version\":1,\"links\":[]}{}",
		"{\"version\":1,\"links\":[{\"accountId\":\"a\",\"discordId\":\"123456789012345678\",\"username\":\"a\",\"linkedAt\":\"2026-10-19T12:00:00Z\"},{\"accountId\":\"b\",\"discordId\":\"123456789012345678\",\"username\":\"a\",\"linkedAt\":\"2026-10-19T12:00:00Z\"}]}",
	} {
		t.Run(raw[:min(20, len(raw))], func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "links.json")
			if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Open(Options{Provider: &fakeProvider{}, StorePath: p, SessionActive: func(Principal) bool { return true }})
			if !errors.Is(err, ErrStore) {
				t.Fatalf("accepted corrupt store: %v", err)
			}
		})
	}
}
func TestBoundedFlowsAndConcurrentStatus(t *testing.T) {
	f := &fakeProvider{id: "123456789012345678"}
	s, now, _ := testService(t, f)
	s.opts.MaxPending = 1
	b, state := begin(t, s, "alex")
	if _, err := s.Begin(Principal{AccountID: "other", SessionID: "other"}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity %v", err)
	}
	if _, err := s.Begin(Principal{AccountID: "alex", SessionID: "session-alex"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(context.Background(), state, b.BrowserNonce, "https://discord.com", "code"); !errors.Is(err, ErrInvalidFlow) {
		t.Fatal("superseded flow accepted")
	}
	*now = now.Add(FlowLifetime)
	if _, err := s.Begin(Principal{AccountID: "other", SessionID: "other"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.Status("alex") }()
	}
	wg.Wait()
}
