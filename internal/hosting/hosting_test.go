package hosting

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testClock struct{ nanos atomic.Int64 }

func testService(t *testing.T, cfg Config) (*Service, *testClock) {
	t.Helper()
	clock := &testClock{}
	clock.nanos.Store(time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC).UnixNano())
	cfg.Now = func() time.Time { return time.Unix(0, clock.nanos.Load()).UTC() }
	s, err := NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s, clock
}
func (c *testClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }
func enroll(t *testing.T, s *Service, owner string) Enrollment {
	t.Helper()
	ticket, e := s.IssuePairingTicket(owner)
	if e != nil {
		t.Fatal(e)
	}
	h, e := s.RedeemPairingTicket(owner, ticket.Token, "Home host")
	if e != nil {
		t.Fatal(e)
	}
	return h
}
func resources() Resources { return Resources{2, 2048, 10240, 20} }
func allocate(t *testing.T, s *Service, h Enrollment, id string, port int, r Resources) ServerAllocation {
	t.Helper()
	a, e := s.AllocateServer(h.Host.OwnerID, AllocationRequest{HostID: h.Host.ID, ServerID: id, Resources: r, GamePort: port})
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func route(h Enrollment, s ServerAllocation) RouteRequest {
	return RouteRequest{HostID: h.Host.ID, ServerID: s.ID, Target: fmt.Sprintf("127.0.0.1:%d", s.GamePort), TTLSeconds: 60}
}
func wantError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
func TestPairingScopeReplayAndExpiry(t *testing.T) {
	s, clock := testService(t, Config{})
	ticket, e := s.IssuePairingTicket("alice")
	if e != nil {
		t.Fatal(e)
	}
	if !ticket.ExpiresAt.Equal(time.Unix(0, clock.nanos.Load()).Add(5 * time.Minute)) {
		t.Fatal("ticket lifetime")
	}
	raw, e := base64.RawURLEncoding.Strict().DecodeString(ticket.Token[4:])
	if e != nil || len(raw) != 32 {
		t.Fatal("ticket entropy")
	}
	_, e = s.RedeemPairingTicket("bob", ticket.Token, "Host")
	wantError(t, e, ErrUnauthorized)
	h, e := s.RedeemPairingTicket("alice", ticket.Token, "Host")
	if e != nil {
		t.Fatalf("wrong owner consumed ticket: %v", e)
	}
	if h.Host.OwnerID != "alice" || h.Credential == ticket.Token || !strings.HasPrefix(h.Credential, "mhc_") {
		t.Fatal("credential identity")
	}
	raw, e = base64.RawURLEncoding.Strict().DecodeString(h.Credential[4:])
	if e != nil || len(raw) != 32 {
		t.Fatal("credential entropy")
	}
	_, e = s.RedeemPairingTicket("alice", ticket.Token, "Host")
	wantError(t, e, ErrUnauthorized)
	expired, e := s.IssuePairingTicket("alice")
	if e != nil {
		t.Fatal(e)
	}
	clock.advance(5 * time.Minute)
	_, e = s.RedeemPairingTicket("alice", expired.Token, "Host")
	wantError(t, e, ErrExpired)
	_, e = s.RedeemPairingTicket("alice", "mtp_not-a-ticket", "Host")
	wantError(t, e, ErrUnauthorized)
}
func TestListingsAreOwnerScopedCopiesWithoutSecrets(t *testing.T) {
	s, _ := testService(t, Config{})
	a := enroll(t, s, "alice")
	b := enroll(t, s, "bob")
	allocate(t, s, a, "alice-server", 25565, resources())
	allocate(t, s, b, "bob-server", 25565, resources())
	hs, e := s.ListHosts("alice")
	if e != nil || len(hs) != 1 || hs[0].ID != a.Host.ID {
		t.Fatal("host scope")
	}
	ss, e := s.ListServers("alice")
	if e != nil || len(ss) != 1 || ss[0].ID != "alice-server" {
		t.Fatal("server scope")
	}
	encoded, _ := json.Marshal(struct {
		Hosts   []Host
		Servers []ServerAllocation
	}{hs, ss})
	for _, bad := range []string{a.Credential, b.Credential, `"credential":`, `"token":`, `"credential_digest":`} {
		if strings.Contains(string(encoded), bad) {
			t.Fatal("listing leaked a secret")
		}
	}
	hs[0].Name = "changed"
	ss[0].Resources.CPUCores = 999
	hs, _ = s.ListHosts("alice")
	ss, _ = s.ListServers("alice")
	if hs[0].Name == "changed" || ss[0].Resources.CPUCores == 999 {
		t.Fatal("mutable listing")
	}
	empty, _ := s.ListHosts("nobody")
	if len(empty) != 0 {
		t.Fatal("cross-owner data")
	}
	wantError(t, s.RevokeHost("bob", a.Host.ID), ErrNotFound)
	_, e = s.AllocateServer("bob", AllocationRequest{HostID: a.Host.ID, ServerID: "stolen", GamePort: 25566, Resources: resources()})
	wantError(t, e, ErrNotFound)
}
func TestRouteRequiresOwnerHostCredentialAndAllocation(t *testing.T) {
	s, _ := testService(t, Config{})
	a := enroll(t, s, "alice")
	other := enroll(t, s, "alice")
	b := enroll(t, s, "bob")
	server := allocate(t, s, a, "a-server", 25565, resources())
	bs := allocate(t, s, b, "b-server", 25565, resources())
	r := route(a, server)
	decision, e := s.PlanRoute("alice", a.Credential, r)
	if e != nil {
		t.Fatal(e)
	}
	if server.State != "policy_registered" || decision.Decision != "policy_allowed" || decision.HostID != a.Host.ID || decision.ServerID != server.ID || decision.Target != r.Target || !decision.ExpiresAt.Equal(server.CreatedAt.Add(time.Minute)) {
		t.Fatal("incorrect or misleading policy result")
	}
	_, e = s.PlanRoute("bob", a.Credential, r)
	wantError(t, e, ErrNotFound)
	_, e = s.PlanRoute("alice", other.Credential, r)
	wantError(t, e, ErrUnauthorized)
	_, e = s.PlanRoute("alice", "global-velocity-or-floodgate-secret", r)
	wantError(t, e, ErrUnauthorized)
	changed := r
	changed.HostID = other.Host.ID
	_, e = s.PlanRoute("alice", other.Credential, changed)
	wantError(t, e, ErrNotFound)
	changed = r
	changed.ServerID = bs.ID
	_, e = s.PlanRoute("alice", a.Credential, changed)
	wantError(t, e, ErrNotFound)
	changed.ServerID = "missing"
	_, e = s.PlanRoute("alice", a.Credential, changed)
	wantError(t, e, ErrNotFound)
	r.Target = "[::1]:25565"
	decision, e = s.PlanRoute("alice", a.Credential, r)
	if e != nil || decision.Target != r.Target {
		t.Fatal("IPv6 loopback")
	}
}
func TestRevocationAndCredentialExpiry(t *testing.T) {
	t.Run("revoke", func(t *testing.T) {
		s, _ := testService(t, Config{})
		h := enroll(t, s, "alice")
		server := allocate(t, s, h, "server", 25565, resources())
		if e := s.RevokeHost("alice", h.Host.ID); e != nil {
			t.Fatal(e)
		}
		if e := s.RevokeHost("alice", h.Host.ID); e != nil {
			t.Fatal(e)
		}
		_, e := s.PlanRoute("alice", h.Credential, route(h, server))
		wantError(t, e, ErrRevoked)
		_, e = s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: "second", GamePort: 25566, Resources: resources()})
		wantError(t, e, ErrRevoked)
		hs, _ := s.ListHosts("alice")
		if !hs[0].Revoked {
			t.Fatal("revoked state")
		}
	})
	t.Run("expiry", func(t *testing.T) {
		s, clock := testService(t, Config{CredentialTTL: 2 * time.Minute})
		h := enroll(t, s, "alice")
		server := allocate(t, s, h, "server", 25565, resources())
		r := route(h, server)
		r.TTLSeconds = 120
		d, e := s.PlanRoute("alice", h.Credential, r)
		if e != nil || !d.ExpiresAt.Equal(h.Host.CredentialExpiresAt) {
			t.Fatal("exact expiry")
		}
		r.TTLSeconds = 121
		_, e = s.PlanRoute("alice", h.Credential, r)
		wantError(t, e, ErrExpired)
		clock.advance(2 * time.Minute)
		r.TTLSeconds = 1
		_, e = s.PlanRoute("alice", h.Credential, r)
		wantError(t, e, ErrExpired)
		_, e = s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: "second", GamePort: 25566, Resources: resources()})
		wantError(t, e, ErrExpired)
	})
}
func TestHostileDestinationsAndLifetimes(t *testing.T) {
	s, _ := testService(t, Config{})
	h := enroll(t, s, "alice")
	server := allocate(t, s, h, "server", 25565, resources())
	for _, target := range []string{"localhost:25565", "example.com:25565", "10.0.0.1:25565", "192.168.1.1:25565", "169.254.169.254:80", "8.8.8.8:25565", "0.0.0.0:25565", "127.0.0.2:25565", "127.0.0.1:22", "127.0.0.1:80", "127.0.0.1:443", "127.0.0.1:8080", "127.0.0.1:8123", "127.0.0.1:25575", "127.0.0.1:25566", "127.0.0.1:65535", "127.0.0.1:0", "[::]:25565", "[::ffff:127.0.0.1]:25565", "[::1%eth0]:25565", "[fd00::1]:25565", "http://127.0.0.1:25565", "127.0.0.1:25565/admin", "127.0.0.1:25565@10.0.0.1:80", ""} {
		t.Run(target, func(t *testing.T) {
			r := route(h, server)
			r.Target = target
			_, e := s.PlanRoute("alice", h.Credential, r)
			wantError(t, e, ErrInvalidInput)
		})
	}
	for _, ttl := range []int{-1, 0, 301, int(^uint(0) >> 1)} {
		r := route(h, server)
		r.TTLSeconds = ttl
		_, e := s.PlanRoute("alice", h.Credential, r)
		wantError(t, e, ErrInvalidInput)
	}
}
func TestRecordBoundsAndExpiredTicketCleanup(t *testing.T) {
	t.Run("tickets", func(t *testing.T) {
		s, c := testService(t, Config{MaxTickets: 2})
		s.IssuePairingTicket("a")
		s.IssuePairingTicket("b")
		_, e := s.IssuePairingTicket("c")
		wantError(t, e, ErrLimitExceeded)
		c.advance(5 * time.Minute)
		if _, e = s.IssuePairingTicket("c"); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("hosts", func(t *testing.T) {
		s, _ := testService(t, Config{MaxHosts: 1})
		h := enroll(t, s, "a")
		tk, _ := s.IssuePairingTicket("b")
		_, e := s.RedeemPairingTicket("b", tk.Token, "Host")
		wantError(t, e, ErrLimitExceeded)
		s.RevokeHost("a", h.Host.ID)
		_, e = s.RedeemPairingTicket("b", tk.Token, "Host")
		wantError(t, e, ErrLimitExceeded)
	})
	t.Run("servers", func(t *testing.T) {
		s, _ := testService(t, Config{MaxServers: 1})
		h := enroll(t, s, "a")
		allocate(t, s, h, "first", 25565, resources())
		_, e := s.AllocateServer("a", AllocationRequest{HostID: h.Host.ID, ServerID: "second", GamePort: 25566, Resources: resources()})
		wantError(t, e, ErrLimitExceeded)
	})
}
func TestResourceBoundsPortConflictsAndInputValidation(t *testing.T) {
	for _, field := range []string{"cpu", "memory", "disk", "players", "overflow"} {
		t.Run(field, func(t *testing.T) {
			s, _ := testService(t, Config{MaxResources: Resources{4, 4096, 20480, 40}})
			h := enroll(t, s, "alice")
			allocate(t, s, h, "first", 25565, resources())
			r := resources()
			switch field {
			case "cpu":
				r.CPUCores++
			case "memory":
				r.MemoryMB++
			case "disk":
				r.DiskMB++
			case "players":
				r.PlayerSlots++
			case "overflow":
				r.CPUCores = int(^uint(0) >> 1)
			}
			_, e := s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: "second", GamePort: 25566, Resources: r})
			wantError(t, e, ErrLimitExceeded)
			allocate(t, s, h, "second", 25566, resources())
			ss, _ := s.ListServers("alice")
			if len(ss) != 2 {
				t.Fatal("rejected allocation changed accounting")
			}
		})
	}
	s, _ := testService(t, Config{})
	h := enroll(t, s, "alice")
	allocate(t, s, h, "first", 25565, resources())
	_, e := s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: "duplicate-port", GamePort: 25565, Resources: resources()})
	wantError(t, e, ErrConflict)
	_, e = s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: "first", GamePort: 25566, Resources: resources()})
	wantError(t, e, ErrConflict)
	for _, r := range []Resources{{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1}, {1, -1, 1, 1}, {1, 1, -1, 1}, {1, 1, 1, -1}} {
		_, e = s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: "invalid", GamePort: 25566, Resources: r})
		wantError(t, e, ErrInvalidInput)
	}
	for _, p := range []int{-1, 0, 22, 1023, 8080, 25575, 65536, int(^uint(0) >> 1)} {
		_, e = s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: "bad-port", GamePort: p, Resources: resources()})
		wantError(t, e, ErrInvalidInput)
	}
	for _, owner := range []string{"", " alice", "alice\n", strings.Repeat("a", 129)} {
		_, e = s.IssuePairingTicket(owner)
		wantError(t, e, ErrInvalidInput)
	}
	_, e = s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: "../shell", GamePort: 25566, Resources: resources()})
	wantError(t, e, ErrInvalidInput)
}
func TestConfigurationRejectsUnboundedOrUnsafeValues(t *testing.T) {
	for _, cfg := range []Config{{TicketTTL: -time.Second}, {TicketTTL: 5*time.Minute + time.Second}, {CredentialTTL: -time.Second}, {CredentialTTL: 31 * 24 * time.Hour}, {MaxRouteTTL: time.Millisecond}, {MaxRouteTTL: 6 * time.Minute}, {MaxTickets: -1}, {MaxTickets: 16385}, {MaxHosts: -1}, {MaxHosts: 4097}, {MaxServers: -1}, {MaxServers: 65537}, {MaxResources: Resources{CPUCores: 1}}, {MaxResources: Resources{257, 1, 1, 1}}} {
		_, e := NewService(cfg)
		wantError(t, e, ErrInvalidInput)
	}
}
func TestConcurrentTicketRedemptionIsSingleUse(t *testing.T) {
	s, _ := testService(t, Config{})
	tk, _ := s.IssuePairingTicket("alice")
	const workers = 32
	out := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := s.RedeemPairingTicket("alice", tk.Token, "Home host")
			out <- e
		}()
	}
	close(start)
	wg.Wait()
	close(out)
	success := 0
	for e := range out {
		if e == nil {
			success++
		} else {
			wantError(t, e, ErrUnauthorized)
		}
	}
	if success != 1 {
		t.Fatalf("redeemed %d times", success)
	}
	hs, _ := s.ListHosts("alice")
	if len(hs) != 1 {
		t.Fatal("duplicate hosts")
	}
}
func TestConcurrentAllocationsCannotExceedAggregateBounds(t *testing.T) {
	s, _ := testService(t, Config{MaxResources: Resources{4, 4, 4, 4}})
	h := enroll(t, s, "alice")
	const workers = 10
	out := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, e := s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: fmt.Sprintf("server-%d", i), GamePort: 25565 + i, Resources: Resources{1, 1, 1, 1}})
			out <- e
		}(i)
	}
	close(start)
	wg.Wait()
	close(out)
	success := 0
	for e := range out {
		if e == nil {
			success++
		} else {
			wantError(t, e, ErrLimitExceeded)
		}
	}
	if success != 4 {
		t.Fatalf("allocated %d servers", success)
	}
	ss, _ := s.ListServers("alice")
	if len(ss) != 4 {
		t.Fatal("allocation count")
	}
}
func TestConcurrentListsRoutesAndRevocation(t *testing.T) {
	s, _ := testService(t, Config{})
	h := enroll(t, s, "alice")
	server := allocate(t, s, h, "server", 25565, resources())
	const workers = 16
	out := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < 40; n++ {
				if _, e := s.ListHosts("alice"); e != nil {
					out <- e
					return
				}
				if _, e := s.ListServers("alice"); e != nil {
					out <- e
					return
				}
				_, e := s.PlanRoute("alice", h.Credential, route(h, server))
				if e != nil && !errors.Is(e, ErrRevoked) {
					out <- e
					return
				}
			}
		}()
	}
	close(start)
	if e := s.RevokeHost("alice", h.Host.ID); e != nil {
		t.Fatal(e)
	}
	wg.Wait()
	close(out)
	for e := range out {
		t.Error(e)
	}
	_, e := s.PlanRoute("alice", h.Credential, route(h, server))
	wantError(t, e, ErrRevoked)
}
