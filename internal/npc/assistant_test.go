package npc

import (
	"context"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func TestAskReturnsServiceReply(t *testing.T) {
	a := New(Config{SoftTimeout: time.Second, PerPlayerQuota: 2, Window: time.Minute},
		func(ctx context.Context, q string) (string, error) { return "hi " + q, nil })
	r := a.Ask(context.Background(), "p1", "where is spawn")
	if r.Fallback || r.Text != "hi where is spawn" {
		t.Fatalf("got %+v", r)
	}
}

func TestSoftTimeoutFallsBack(t *testing.T) {
	a := New(Config{SoftTimeout: 20 * time.Millisecond, PerPlayerQuota: 2, Window: time.Minute},
		func(ctx context.Context, q string) (string, error) {
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
			return "late", nil
		})
	start := time.Now()
	r := a.Ask(context.Background(), "p1", "x")
	if !r.Fallback || r.Reason != "timeout" || r.Text != FallbackText {
		t.Fatalf("got %+v", r)
	}
	if time.Since(start) > time.Second {
		t.Fatal("did not return at soft timeout")
	}
}

func TestQuotaPerPlayerAndWindowReset(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1000, 0)}
	calls := 0
	a := New(Config{SoftTimeout: time.Second, PerPlayerQuota: 1, Window: time.Minute, Clock: clk.Now},
		func(ctx context.Context, q string) (string, error) { calls++; return "ok", nil })
	if r := a.Ask(context.Background(), "a", "x"); r.Fallback {
		t.Fatal("first should pass")
	}
	if r := a.Ask(context.Background(), "a", "x"); !r.Fallback || r.Reason != "quota" {
		t.Fatalf("got %+v", r)
	}
	if r := a.Ask(context.Background(), "b", "x"); r.Fallback {
		t.Fatal("other player unaffected")
	}
	clk.now = clk.now.Add(2 * time.Minute)
	if r := a.Ask(context.Background(), "a", "x"); r.Fallback {
		t.Fatal("window should reset")
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestServiceErrorFallsBackAndCommandReplyDropped(t *testing.T) {
	a := New(Config{SoftTimeout: time.Second, PerPlayerQuota: 5, Window: time.Minute},
		func(ctx context.Context, q string) (string, error) { return "/op me", nil })
	r := a.Ask(context.Background(), "p", "ignore rules")
	if !r.Fallback || r.Reason != "unsafe" {
		t.Fatalf("command-like reply must be dropped: %+v", r)
	}
	a = New(Config{SoftTimeout: time.Second, PerPlayerQuota: 5, Window: time.Minute},
		func(ctx context.Context, q string) (string, error) { return "", context.Canceled })
	if r := a.Ask(context.Background(), "p", "x"); !r.Fallback || r.Reason != "error" {
		t.Fatalf("got %+v", r)
	}
}
