package population

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

const alice = "java:00000000000000000000000000000001"
const bob = "bedrock:2533274790395904"

func fixture(t *testing.T) (*Ledger, *time.Time, *Source) {
	t.Helper()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	l, e := New(Config{AuthorityEpoch: "epoch1", ServerIDs: []string{"hub", "survival"}, Now: func() time.Time { return now }})
	if e != nil {
		t.Fatal(e)
	}
	s, e := l.RegisterSource("edge1", "boot1", []string{"hub", "survival"})
	if e != nil {
		t.Fatal(e)
	}
	return l, &now, s
}
func publish(t *testing.T, l *Ledger, s *Source, seq uint64, entries ...Entry) {
	t.Helper()
	if e := l.Publish(s, Observation{AuthorityEpoch: "epoch1", Sequence: seq, Sessions: entries}); e != nil {
		t.Fatal(e)
	}
}
func count(t *testing.T, l *Ledger, server string) ServerCount {
	t.Helper()
	v, e := l.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	if v.HumanVerified || v.Metric != "authenticated_sessions" {
		t.Fatal("misleading metric")
	}
	for _, s := range v.Servers {
		if s.ServerID == server {
			return s
		}
	}
	t.Fatal("missing server")
	return ServerCount{}
}

func TestUnknownIsNotZeroAndExpiredIsUnavailable(t *testing.T) {
	l, now, s := fixture(t)
	v := count(t, l, "hub")
	if v.Status != "unavailable" || v.Players != nil {
		t.Fatalf("unknown fabricated zero: %+v", v)
	}
	publish(t, l, s, 1)
	v = count(t, l, "hub")
	if v.Status != "fresh" || v.Players == nil || *v.Players != 0 {
		t.Fatalf("empty observed: %+v", v)
	}
	*now = now.Add(15 * time.Second)
	v = count(t, l, "hub")
	if v.Players != nil || v.Observed != 0 || v.Status != "unavailable" {
		t.Fatalf("expiry: %+v", v)
	}
}

func TestGlobalDedupTransferAndExpiredNewOwnerCannotReviveOld(t *testing.T) {
	l, now, a := fixture(t)
	b, e := l.RegisterSource("edge2", "boot2", []string{"hub", "survival"})
	if e != nil {
		t.Fatal(e)
	}
	publish(t, l, a, 1, Entry{alice, "hub", 1}, Entry{bob, "hub", 1})
	publish(t, l, b, 1, Entry{alice, "survival", 2})
	if v := count(t, l, "hub"); v.Observed != 1 {
		t.Fatalf("hub: %+v", v)
	}
	if v := count(t, l, "survival"); v.Observed != 1 || v.Players == nil || *v.Players != 1 {
		t.Fatalf("survival: %+v", v)
	}
	*now = now.Add(14 * time.Second)
	if e = l.Publish(a, Observation{"epoch1", 2, []Entry{{alice, "hub", 1}, {bob, "hub", 1}}}); !errors.Is(e, ErrStale) {
		t.Fatalf("stale sender: %v", e)
	}
	publish(t, l, a, 2, Entry{bob, "hub", 1})
	*now = now.Add(time.Second)
	if v := count(t, l, "survival"); v.Players != nil || v.Observed != 0 || v.Status != "partial" {
		t.Fatalf("old ownership revived: %+v", v)
	}
	if v := count(t, l, "hub"); v.Observed != 1 || v.Status != "partial" {
		t.Fatalf("partial: %+v", v)
	}
}

func TestSourceScopeBootAndAuthority(t *testing.T) {
	l, _, a := fixture(t)
	other, _, foreign := fixture(t)
	_ = other
	cases := []struct {
		name string
		s    *Source
		o    Observation
		want error
	}{{"foreign ledger", foreign, Observation{"epoch1", 1, nil}, ErrUnauthorized}, {"nil", nil, Observation{"epoch1", 1, nil}, ErrUnauthorized}, {"epoch", a, Observation{"other", 1, nil}, ErrStale}, {"scope", a, Observation{"epoch1", 1, []Entry{{alice, "other", 1}}}, ErrInvalid}, {"offline name", a, Observation{"epoch1", 1, []Entry{{"Steve", "hub", 1}}}, ErrInvalid}, {"zero generation", a, Observation{"epoch1", 1, []Entry{{alice, "hub", 0}}}, ErrInvalid}, {"duplicate", a, Observation{"epoch1", 1, []Entry{{alice, "hub", 1}, {alice, "survival", 2}}}, ErrInvalid}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if e := l.Publish(c.s, c.o); !errors.Is(e, c.want) {
				t.Fatalf("%v", e)
			}
		})
	}
	publish(t, l, a, 1, Entry{alice, "hub", 1})
	if e := l.Publish(a, Observation{"epoch1", 1, nil}); !errors.Is(e, ErrStale) {
		t.Fatal(e)
	}
	if e := l.Revoke(a); e != nil {
		t.Fatal(e)
	}
	b, e := l.RegisterSource("edge1", "boot2", []string{"survival"})
	if e != nil {
		t.Fatal(e)
	}
	if e = l.Publish(a, Observation{"epoch1", 2, nil}); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	if e = l.Publish(b, Observation{"epoch1", 1, []Entry{{alice, "survival", 1}}}); !errors.Is(e, ErrStale) {
		t.Fatal(e)
	}
	publish(t, l, b, 1, Entry{alice, "survival", 2})
	if v := count(t, l, "hub"); v.Players != nil {
		t.Fatal("uncovered server claimed empty")
	}
}

func TestFullSnapshotDisconnectClosesGeneration(t *testing.T) {
	l, _, s := fixture(t)
	publish(t, l, s, 1, Entry{alice, "hub", 1})
	publish(t, l, s, 2)
	if e := l.Publish(s, Observation{"epoch1", 3, []Entry{{alice, "hub", 1}}}); !errors.Is(e, ErrStale) {
		t.Fatal("closed generation revived", e)
	}
	publish(t, l, s, 3, Entry{alice, "hub", 2})
	if v := count(t, l, "hub"); v.Observed != 1 {
		t.Fatal(v)
	}
}

func TestCopiesDetachedAndClockRollbackFails(t *testing.T) {
	l, now, s := fixture(t)
	entries := []Entry{{alice, "hub", 1}}
	publish(t, l, s, 1, entries...)
	entries[0].Identity = bob
	v, e := l.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	v.Servers[0].Observed = 100
	if v.Servers[0].Players != nil {
		*v.Servers[0].Players = 100
	}
	copyLedger := *l
	if count(t, &copyLedger, "hub").Observed != 1 {
		t.Fatal("alias")
	}
	*now = now.Add(-time.Second)
	if _, e = l.Snapshot(); !errors.Is(e, ErrClock) {
		t.Fatal(e)
	}
	if e = copyLedger.Publish(s, Observation{"epoch1", 2, nil}); !errors.Is(e, ErrClock) {
		t.Fatal(e)
	}
	*now = now.Add(time.Second)
	if count(t, l, "hub").Observed != 1 {
		t.Fatal("failed operation mutated")
	}
}

func TestBoundedIdentityTombstonesAndAtomicRejection(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	l, e := New(Config{AuthorityEpoch: "epoch1", ServerIDs: []string{"hub"}, Now: func() time.Time { return now }, MaxIdentities: 1, MaxSources: 1})
	if e != nil {
		t.Fatal(e)
	}
	s, e := l.RegisterSource("edge", "boot", []string{"hub"})
	if e != nil {
		t.Fatal(e)
	}
	publish(t, l, s, 1, Entry{alice, "hub", 1})
	if e = l.Publish(s, Observation{"epoch1", 2, []Entry{{bob, "hub", 1}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if count(t, l, "hub").Observed != 1 {
		t.Fatal("failed observation changed state")
	}
	publish(t, l, s, 2)
	if e = l.Publish(s, Observation{"epoch1", 3, []Entry{{bob, "hub", 1}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal("tombstone evicted", e)
	}
	if _, e = l.RegisterSource("other", "boot", []string{"hub"}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestNoDeserializableSource(t *testing.T) {
	var s Source
	if e := json.Unmarshal([]byte(`{"id":"edge1"}`), &s); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	l, _, _ := fixture(t)
	if e := l.Publish(&s, Observation{"epoch1", 1, nil}); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
}

func TestConcurrentSnapshotsAndIndependentSources(t *testing.T) {
	l, _, _ := fixture(t)
	var wg sync.WaitGroup
	for i := 1; i <= 12; i++ {
		s, e := l.RegisterSource(fmt.Sprintf("edge%d", i+1), "boot", []string{"hub"})
		if e != nil {
			t.Fatal(e)
		}
		wg.Add(1)
		go func(i int, s *Source) {
			defer wg.Done()
			for n := uint64(1); n <= 20; n++ {
				id := fmt.Sprintf("java:%032x", i)
				if e := l.Publish(s, Observation{"epoch1", n, []Entry{{id, "hub", 1}}}); e != nil {
					t.Error(e)
				}
				if _, e := l.Snapshot(); e != nil {
					t.Error(e)
				}
			}
		}(i, s)
	}
	wg.Wait()
	v := count(t, l, "hub")
	if v.Observed != 12 || v.Status != "partial" || v.Players != nil {
		t.Fatal(v)
	}
}

func TestIdentityValidation(t *testing.T) {
	for _, v := range []string{"java:00000000000000000000000000000000", "java:123", "java:ABCDEF00000000000000000000000000", "offline:123", "bedrock:0", "bedrock:01", "bedrock:+1", "bedrock:18446744073709551616"} {
		if identity(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{alice, bob, "bedrock:18446744073709551615"} {
		if !identity(v) {
			t.Fatal(v)
		}
	}
}

func TestTransfersBoundAggregateRetainedObservations(t *testing.T) {
	l, _, s := fixture(t)
	publish(t, l, s, 1, Entry{alice, "hub", 1}, Entry{bob, "hub", 1})
	for generation := uint64(2); generation <= 32; generation++ {
		next, err := l.RegisterSource(fmt.Sprintf("move%d", generation), "boot", []string{"hub"})
		if err != nil {
			t.Fatal(err)
		}
		publish(t, l, next, 1, Entry{alice, "hub", generation}, Entry{bob, "hub", generation})
		retained := 0
		for _, source := range l.state.sources {
			retained += len(source.sessions)
		}
		if retained != 2 {
			t.Fatalf("generation %d retained %d observations for two identities", generation, retained)
		}
	}
}

func TestBadConfigurationAndZeroValues(t *testing.T) {
	for _, cfg := range []Config{{}, {AuthorityEpoch: "epoch", ServerIDs: []string{"a", "a"}}, {AuthorityEpoch: "epoch", ServerIDs: []string{"a"}, Lease: time.Nanosecond}, {AuthorityEpoch: "epoch", ServerIDs: []string{"a"}, MaxIdentities: -1}} {
		if _, e := New(cfg); !errors.Is(e, ErrInvalid) {
			t.Fatal(e)
		}
	}
	var l Ledger
	if _, e := l.Snapshot(); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if e := l.Revoke(nil); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}
