package control

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func loginRequest(h http.Handler, ip, password string) *httptest.ResponseRecorder {
	form := url.Values{"username": {"admin"}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.RemoteAddr = ip + ":12345"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestLoginThrottleConcurrentFailures(t *testing.T) {
	prior := runtime.GOMAXPROCS(8)
	defer runtime.GOMAXPROCS(prior)
	// The body stays below the real 4 KiB form limit. Releasing a burst of
	// requests together exercises accounting between the limit check and the
	// password comparison; the race detector alone cannot catch lost updates.
	for round := 0; round < 20; round++ {
		c := NewController(Config{AdminUser: "admin", AdminPassword: strings.Repeat("a", 2000)})
		now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
		c.now = func() time.Time { return now }
		h := c.Handler()
		const count = 64
		start := make(chan struct{})
		codes := make(chan int, count)
		var ready, done sync.WaitGroup
		ready.Add(count)
		done.Add(count)
		for i := 0; i < count; i++ {
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				codes <- loginRequest(h, "192.0.2.10", strings.Repeat("b", 2000)).Code
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()
		close(codes)
		attempts, limited := 0, 0
		for code := range codes {
			switch code {
			case http.StatusSeeOther:
				attempts++
			case http.StatusTooManyRequests:
				limited++
			default:
				t.Fatalf("unexpected HTTP %d", code)
			}
		}
		if attempts != 5 || limited != count-5 {
			t.Fatalf("round %d: attempted=%d, limited=%d; want exactly 5 attempts in one source window", round, attempts, limited)
		}
	}
}

func TestLoginThrottleExpirySourceIsolationAndSuccessReset(t *testing.T) {
	const password = "synthetic-password-for-login-tests"
	c := NewController(Config{AdminUser: "admin", AdminPassword: password})
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	h := c.Handler()
	for i := 0; i < 5; i++ {
		if got := loginRequest(h, "192.0.2.10", "wrong").Code; got != http.StatusSeeOther {
			t.Fatalf("attempt %d HTTP %d", i, got)
		}
	}
	if got := loginRequest(h, "192.0.2.10", password).Code; got != http.StatusTooManyRequests {
		t.Fatalf("exhausted source HTTP %d", got)
	}
	if got := loginRequest(h, "192.0.2.11", "wrong").Code; got != http.StatusSeeOther {
		t.Fatalf("independent source was blocked: HTTP %d", got)
	}
	now = now.Add(5*time.Minute + time.Nanosecond)
	for i := 0; i < 4; i++ {
		if got := loginRequest(h, "192.0.2.10", "wrong").Code; got != http.StatusSeeOther {
			t.Fatalf("renewed source HTTP %d", got)
		}
	}
	loggedIn := loginRequest(h, "192.0.2.10", password)
	if loggedIn.Code != http.StatusSeeOther || len(loggedIn.Result().Cookies()) != 1 {
		t.Fatalf("valid login did not create a session: HTTP %d", loggedIn.Code)
	}
	for i := 0; i < 5; i++ {
		if got := loginRequest(h, "192.0.2.10", "wrong").Code; got != http.StatusSeeOther {
			t.Fatalf("successful login did not reset the failure window: attempt %d HTTP %d", i, got)
		}
	}
	if got := loginRequest(h, "192.0.2.10", "wrong").Code; got != http.StatusTooManyRequests {
		t.Fatalf("newly exhausted source HTTP %d", got)
	}
}
