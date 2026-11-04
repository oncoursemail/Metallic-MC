// Package idreserve reserves server instance IDs with idempotency keys and
// expiry. It is in-memory policy only: it creates no server and persists nothing.
package idreserve

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrInvalidInput = errors.New("invalid reservation input")
	ErrKeyConflict  = errors.New("idempotency key reused with a different instance id")
	ErrIDTaken      = errors.New("instance id is reserved")
	ErrNotHolder    = errors.New("caller does not hold this reservation")
	ErrFull         = errors.New("too many live reservations")
)

const maxIDLen = 63

// Reservation is a time-limited claim on an instance ID.
type Reservation struct {
	Owner     string
	Key       string
	ID        string
	ExpiresAt time.Time
}

type Registry struct {
	mu    sync.Mutex
	now   func() time.Time
	ttl   time.Duration
	max   int
	byID  map[string]Reservation
	byKey map[string]string // owner + NUL + key -> id
}

// New builds a registry. The clock is injected so expiry is testable.
func New(now func() time.Time, ttl time.Duration, maxLive int) (*Registry, error) {
	if now == nil || ttl <= 0 || maxLive <= 0 {
		return nil, ErrInvalidInput
	}
	return &Registry{now: now, ttl: ttl, max: maxLive,
		byID: map[string]Reservation{}, byKey: map[string]string{}}, nil
}

func validID(id string) bool {
	if id == "" || len(id) > maxIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}

func validToken(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func (r *Registry) sweep(now time.Time) {
	for id, res := range r.byID {
		if !now.Before(res.ExpiresAt) {
			delete(r.byID, id)
			delete(r.byKey, res.Owner+"\x00"+res.Key)
		}
	}
}

// Reserve claims id for owner. Repeating the same owner, key and id returns the
// existing reservation with replayed=true.
func (r *Registry) Reserve(owner, key, id string) (Reservation, bool, error) {
	if !validToken(owner) || !validToken(key) || !validID(id) {
		return Reservation{}, false, ErrInvalidInput
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	r.sweep(now)
	k := owner + "\x00" + key
	if prev, ok := r.byKey[k]; ok {
		if prev != id {
			return Reservation{}, false, ErrKeyConflict
		}
		return r.byID[id], true, nil
	}
	if _, taken := r.byID[id]; taken {
		return Reservation{}, false, ErrIDTaken
	}
	if len(r.byID) >= r.max {
		return Reservation{}, false, ErrFull
	}
	res := Reservation{Owner: owner, Key: key, ID: id, ExpiresAt: now.Add(r.ttl)}
	r.byID[id] = res
	r.byKey[k] = id
	return res, false, nil
}

// Release frees a live reservation held by owner.
func (r *Registry) Release(owner, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweep(r.now())
	res, ok := r.byID[id]
	if !ok || res.Owner != owner {
		return ErrNotHolder
	}
	delete(r.byID, id)
	delete(r.byKey, res.Owner+"\x00"+res.Key)
	return nil
}
