package idreserve

import (
	"errors"
	"testing"
	"time"
)

func newReg(t *testing.T) (*Registry, *time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	r, err := New(func() time.Time { return now }, time.Minute, 8)
	if err != nil {
		t.Fatal(err)
	}
	return r, &now
}

func TestReserveIsIdempotentPerOwnerKey(t *testing.T) {
	r, _ := newReg(t)
	a, replay, err := r.Reserve("owner-1", "key-1", "survival-1")
	if err != nil || replay {
		t.Fatalf("first: %v %v", err, replay)
	}
	b, replay, err := r.Reserve("owner-1", "key-1", "survival-1")
	if err != nil || !replay || a != b {
		t.Fatalf("replay: %+v %v %v", b, replay, err)
	}
	if _, _, err := r.Reserve("owner-1", "key-1", "other"); !errors.Is(err, ErrKeyConflict) {
		t.Fatalf("want key conflict, got %v", err)
	}
}

func TestIDHeldByAnotherOwnerIsRejected(t *testing.T) {
	r, _ := newReg(t)
	if _, _, err := r.Reserve("owner-1", "k1", "survival-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Reserve("owner-2", "k2", "survival-1"); !errors.Is(err, ErrIDTaken) {
		t.Fatalf("want taken, got %v", err)
	}
	if _, _, err := r.Reserve("owner-1", "k3", "survival-1"); !errors.Is(err, ErrIDTaken) {
		t.Fatalf("same owner new key must not double-reserve, got %v", err)
	}
}

func TestExpiryAndRelease(t *testing.T) {
	r, now := newReg(t)
	if _, _, err := r.Reserve("o1", "k1", "id-a"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	if _, replay, err := r.Reserve("o2", "k2", "id-a"); err != nil || replay {
		t.Fatalf("expired id should be reusable: %v %v", err, replay)
	}
	if err := r.Release("o1", "id-a"); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("non-holder release: %v", err)
	}
	if err := r.Release("o2", "id-a"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Reserve("o3", "k3", "id-a"); err != nil {
		t.Fatalf("released id reusable: %v", err)
	}
}

func TestInvalidInputAndCapacity(t *testing.T) {
	r, _ := newReg(t)
	long := make([]byte, 70)
	for i := range long {
		long[i] = 'a'
	}
	for _, id := range []string{"", "UPPER", "-lead", "has space", "../x", "a/b", string(long)} {
		if _, _, err := r.Reserve("o", "k", id); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	if _, _, err := r.Reserve("", "k", "ok"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("empty owner accepted")
	}
	if _, _, err := r.Reserve("o", "", "ok"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("empty key accepted")
	}
	for i := 0; i < 8; i++ {
		s := string(rune('a' + i))
		if _, _, err := r.Reserve("o", s, "id-"+s); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := r.Reserve("o", "zz", "id-zz"); !errors.Is(err, ErrFull) {
		t.Fatalf("want full, got %v", err)
	}
}

func TestNewValidates(t *testing.T) {
	if _, err := New(nil, time.Minute, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("nil clock")
	}
	if _, err := New(time.Now, 0, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("zero ttl")
	}
	if _, err := New(time.Now, time.Minute, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("zero max")
	}
}
