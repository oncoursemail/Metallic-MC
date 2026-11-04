// Package npc is a side-effect-free gate for the Star AI NPC assistant.
// The model call is an injected function run off the caller's goroutine with
// a soft timeout, a per-player quota and a safe fallback. Replies are plain
// chat text only; command-like output is dropped, so no admin operation is
// reachable from a player prompt. Callers must not invoke Ask on a game tick
// thread without handing the result back asynchronously.
package npc

import (
	"context"
	"strings"
	"sync"
	"time"
)

// FallbackText is the reply used whenever the service cannot answer safely.
const FallbackText = "Star is busy right now. Please try again shortly."

type Service func(ctx context.Context, question string) (string, error)

type Config struct {
	SoftTimeout    time.Duration
	PerPlayerQuota int
	Window         time.Duration
	Clock          func() time.Time
}

type Reply struct {
	Text     string
	Fallback bool
	Reason   string
}

type window struct {
	start time.Time
	count int
}

type Assistant struct {
	cfg  Config
	svc  Service
	mu   sync.Mutex
	used map[string]window
}

func New(cfg Config, svc Service) *Assistant {
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &Assistant{cfg: cfg, svc: svc, used: map[string]window{}}
}

func fallback(reason string) Reply {
	return Reply{Text: FallbackText, Fallback: true, Reason: reason}
}

func (a *Assistant) allow(player string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.cfg.Clock()
	w := a.used[player]
	if w.count == 0 || now.Sub(w.start) >= a.cfg.Window {
		w = window{start: now}
	}
	if w.count >= a.cfg.PerPlayerQuota {
		a.used[player] = w
		return false
	}
	w.count++
	a.used[player] = w
	return true
}

// Ask answers a player question or returns a safe fallback.
func (a *Assistant) Ask(ctx context.Context, player, question string) Reply {
	if !a.allow(player) {
		return fallback("quota")
	}
	ctx, cancel := context.WithTimeout(ctx, a.cfg.SoftTimeout)
	defer cancel()
	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		t, err := a.svc(ctx, question)
		ch <- result{t, err}
	}()
	select {
	case <-ctx.Done():
		return fallback("timeout")
	case r := <-ch:
		if r.err != nil {
			return fallback("error")
		}
		t := strings.TrimSpace(r.text)
		if t == "" || strings.HasPrefix(t, "/") {
			return fallback("unsafe")
		}
		return Reply{Text: t}
	}
}
