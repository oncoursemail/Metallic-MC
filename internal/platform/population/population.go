// Package population maintains short-lived, authenticated-session observations.
// It accepts observations only through in-process sources registered by a trusted
// adapter. It does not authenticate Minecraft, detect humans, or trust server ping.
package population

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalid      = errors.New("invalid population input")
	ErrUnauthorized = errors.New("population source not authorized")
	ErrStale        = errors.New("stale population observation")
	ErrCapacity     = errors.New("population capacity exhausted")
	ErrClock        = errors.New("population clock regressed or invalid")
)

type Config struct {
	AuthorityEpoch string
	ServerIDs      []string
	Now            func() time.Time
	Lease          time.Duration
	MaxSources     int
	MaxIdentities  int
}

// Entry must come from a completed account-authenticated session and the current
// canonical session authority fence, never a tenant plugin or server status ping.
// Generation is monotonic per account within AuthorityEpoch, across all gateways.
type Entry struct {
	Identity   string `json:"identity"`
	ServerID   string `json:"server_id"`
	Generation uint64 `json:"generation"`
}

type Observation struct {
	AuthorityEpoch string  `json:"authority_epoch"`
	Sequence       uint64  `json:"sequence"`
	Sessions       []Entry `json:"sessions"`
}

// Source is an opaque in-process capability. No HTTP handler accepts it or
// publishes observations in this milestone. A future adapter must authenticate
// gateway identity, bind the boot instance and scope, and validate authority fences.
type Source struct {
	ledger       *state
	id, instance string
	nonce        *byte
}

func (*Source) UnmarshalJSON([]byte) error { return ErrUnauthorized }

type Ledger struct{ state *state }
type sourceRecord struct {
	instance string
	nonce    *byte
	scopes   map[string]bool
	sequence uint64
	expires  time.Time
	sessions map[string]Entry
	revoked  bool
}
type watermark struct {
	generation               uint64
	source, instance, server string
	closed                   bool
}
type state struct {
	mu                        sync.Mutex
	epoch                     string
	now                       func() time.Time
	lastNow                   time.Time
	lease                     time.Duration
	maxSources, maxIdentities int
	servers                   []string
	serverSet                 map[string]bool
	sources                   map[string]*sourceRecord
	identities                map[string]watermark
}

type ServerCount struct {
	ServerID string `json:"server_id"`
	Status   string `json:"status"`
	// Players is absent when coverage is incomplete. Observed contains unexpired
	// observations; neither value proves current physical presence or human activity.
	Players         *int       `json:"players"`
	Observed        int        `json:"observed_authenticated_sessions"`
	ExpectedSources int        `json:"expected_sources"`
	FreshSources    int        `json:"fresh_sources"`
	ValidUntil      *time.Time `json:"valid_until"`
}
type Snapshot struct {
	Schema         string        `json:"schema"`
	AuthorityEpoch string        `json:"authority_epoch"`
	SampledAt      time.Time     `json:"sampled_at"`
	Metric         string        `json:"metric"`
	HumanVerified  bool          `json:"human_verified"`
	Servers        []ServerCount `json:"servers"`
}

func New(cfg Config) (*Ledger, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Lease == 0 {
		cfg.Lease = 15 * time.Second
	}
	if cfg.MaxSources == 0 {
		cfg.MaxSources = 64
	}
	if cfg.MaxIdentities == 0 {
		cfg.MaxIdentities = 100000
	}
	if !identifier(cfg.AuthorityEpoch) || len(cfg.ServerIDs) == 0 || len(cfg.ServerIDs) > 4096 || cfg.Lease < time.Second || cfg.Lease > time.Minute || cfg.MaxSources < 1 || cfg.MaxSources > 4096 || cfg.MaxIdentities < 1 || cfg.MaxIdentities > 1000000 {
		return nil, ErrInvalid
	}
	set := make(map[string]bool, len(cfg.ServerIDs))
	for _, id := range cfg.ServerIDs {
		if !identifier(id) || set[id] {
			return nil, ErrInvalid
		}
		set[id] = true
	}
	servers := append([]string(nil), cfg.ServerIDs...)
	sort.Strings(servers)
	return &Ledger{state: &state{epoch: cfg.AuthorityEpoch, now: cfg.Now, lease: cfg.Lease, maxSources: cfg.MaxSources, maxIdentities: cfg.MaxIdentities, servers: servers, serverSet: set, sources: map[string]*sourceRecord{}, identities: map[string]watermark{}}}, nil
}

// RegisterSource is an operator/adapter operation, not a tenant-facing API. A
// replaced source requires prior revocation and a different boot instance ID.
func (l *Ledger) RegisterSource(id, instance string, servers []string) (*Source, error) {
	if l == nil || l.state == nil || !identifier(id) || !identifier(instance) || len(servers) == 0 || len(servers) > 4096 {
		return nil, ErrInvalid
	}
	s := l.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.clock(); err != nil {
		return nil, err
	}
	prior := s.sources[id]
	if prior != nil && (!prior.revoked || prior.instance == instance) {
		return nil, ErrStale
	}
	if prior == nil && len(s.sources) >= s.maxSources {
		return nil, ErrCapacity
	}
	scopes := make(map[string]bool, len(servers))
	for _, server := range servers {
		if !s.serverSet[server] || scopes[server] {
			return nil, ErrInvalid
		}
		scopes[server] = true
	}
	nonce := new(byte)
	s.sources[id] = &sourceRecord{instance: instance, nonce: nonce, scopes: scopes}
	return &Source{ledger: s, id: id, instance: instance, nonce: nonce}, nil
}

// Revoke immediately removes the source's observations. Watermarks remain so
// an older ownership generation cannot resurrect a transferred or revoked session.
// Expiry alone is an observation timeout; a fresh higher-sequence snapshot from
// the same current source may renew a still-active generation.
func (l *Ledger) Revoke(source *Source) error {
	if l == nil || l.state == nil {
		return ErrInvalid
	}
	s := l.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.clock(); err != nil {
		return err
	}
	r, err := s.authorize(source)
	if err != nil {
		return err
	}
	for _, entry := range r.sessions {
		w := s.identities[entry.Identity]
		if w.source == source.id && w.instance == source.instance && w.generation == entry.Generation {
			w.closed = true
			s.identities[entry.Identity] = w
		}
	}
	r.revoked = true
	r.sessions = nil
	return nil
}

// Publish atomically replaces one gateway's full observation. Freshness is
// assigned from the trusted receiver clock, not a supplied timestamp. Callers
// must not renew a lease by replaying cached sessions under new sequence numbers.
func (l *Ledger) Publish(source *Source, observation Observation) error {
	if l == nil || l.state == nil {
		return ErrInvalid
	}
	s := l.state
	s.mu.Lock()
	defer s.mu.Unlock()
	now, err := s.clock()
	if err != nil {
		return err
	}
	r, err := s.authorize(source)
	if err != nil {
		return err
	}
	if observation.AuthorityEpoch != s.epoch || observation.Sequence == 0 || observation.Sequence <= r.sequence {
		return ErrStale
	}
	if len(observation.Sessions) > s.maxIdentities {
		return ErrCapacity
	}
	seen := make(map[string]bool, len(observation.Sessions))
	newIdentities := 0
	for _, entry := range observation.Sessions {
		if !identity(entry.Identity) || !r.scopes[entry.ServerID] || entry.Generation == 0 || entry.Generation == math.MaxUint64 || seen[entry.Identity] {
			return ErrInvalid
		}
		seen[entry.Identity] = true
		prior, ok := s.identities[entry.Identity]
		if !ok {
			newIdentities++
			continue
		}
		if entry.Generation < prior.generation {
			return ErrStale
		}
		if entry.Generation == prior.generation && (prior.closed || prior.source != source.id || prior.instance != source.instance || prior.server != entry.ServerID) {
			return ErrStale
		}
	}
	if newIdentities > s.maxIdentities-len(s.identities) {
		return ErrCapacity
	}
	copyEntries := make(map[string]Entry, len(observation.Sessions))
	for _, entry := range observation.Sessions {
		copyEntries[entry.Identity] = entry
	}
	for _, entry := range r.sessions {
		if !seen[entry.Identity] {
			w := s.identities[entry.Identity]
			if w.source == source.id && w.instance == source.instance && w.generation == entry.Generation {
				w.closed = true
				s.identities[entry.Identity] = w
			}
		}
	}
	for _, entry := range copyEntries {
		// Retain at most MaxIdentities observations across all sources, even
		// after repeated transfers of the same accounts between many gateways.
		prior, ok := s.identities[entry.Identity]
		if ok && prior.source != source.id {
			if old := s.sources[prior.source]; old != nil && old.instance == prior.instance {
				delete(old.sessions, entry.Identity)
			}
		}
		s.identities[entry.Identity] = watermark{generation: entry.Generation, source: source.id, instance: source.instance, server: entry.ServerID}
	}
	r.sessions = copyEntries
	r.sequence = observation.Sequence
	r.expires = now.Add(s.lease)
	return nil
}

func (l *Ledger) Snapshot() (Snapshot, error) {
	if l == nil || l.state == nil {
		return Snapshot{}, ErrInvalid
	}
	s := l.state
	s.mu.Lock()
	defer s.mu.Unlock()
	now, err := s.clock()
	if err != nil {
		return Snapshot{}, err
	}
	result := Snapshot{Schema: "metallic.population/v1", AuthorityEpoch: s.epoch, SampledAt: now, Metric: "authenticated_sessions", HumanVerified: false, Servers: make([]ServerCount, 0, len(s.servers))}
	counts := make(map[string]int, len(s.servers))
	for id, r := range s.sources {
		if r.revoked || r.sequence == 0 || !now.Before(r.expires) {
			continue
		}
		for _, entry := range r.sessions {
			w := s.identities[entry.Identity]
			if !w.closed && w.generation == entry.Generation && w.source == id && w.instance == r.instance && w.server == entry.ServerID {
				counts[entry.ServerID]++
			}
		}
	}
	for _, server := range s.servers {
		v := ServerCount{ServerID: server, Status: "unavailable", Observed: counts[server]}
		var earliest time.Time
		for _, r := range s.sources {
			if r.revoked || !r.scopes[server] {
				continue
			}
			v.ExpectedSources++
			if r.sequence > 0 && now.Before(r.expires) {
				v.FreshSources++
				if earliest.IsZero() || r.expires.Before(earliest) {
					earliest = r.expires
				}
			}
		}
		if v.ExpectedSources > 0 && v.FreshSources == v.ExpectedSources {
			v.Status = "fresh"
			n := v.Observed
			v.Players = &n
			t := earliest
			v.ValidUntil = &t
		} else if v.FreshSources > 0 {
			v.Status = "partial"
			t := earliest
			v.ValidUntil = &t
		}
		result.Servers = append(result.Servers, v)
	}
	return result, nil
}

func (s *state) authorize(source *Source) (*sourceRecord, error) {
	if source == nil || source.ledger != s {
		return nil, ErrUnauthorized
	}
	r := s.sources[source.id]
	if r == nil || r.revoked || r.nonce != source.nonce || r.instance != source.instance {
		return nil, ErrUnauthorized
	}
	return r, nil
}
func (s *state) clock() (time.Time, error) {
	now := s.now().UTC()
	if now.IsZero() || now.Year() < 1970 || now.Year() > 9998 || (!s.lastNow.IsZero() && now.Before(s.lastNow)) {
		return time.Time{}, ErrClock
	}
	s.lastNow = now
	return now, nil
}
func identifier(v string) bool {
	if len(v) < 1 || len(v) > 96 {
		return false
	}
	for i, c := range []byte(v) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		if i > 0 && (c == '-' || c == '_' || c == '.' || c == ':') {
			continue
		}
		return false
	}
	return true
}
func identity(v string) bool {
	if strings.HasPrefix(v, "java:") {
		id := strings.TrimPrefix(v, "java:")
		if len(id) != 32 {
			return false
		}
		nonzero := false
		for _, c := range []byte(id) {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
			if c != '0' {
				nonzero = true
			}
		}
		return nonzero
	}
	if strings.HasPrefix(v, "bedrock:") {
		id := strings.TrimPrefix(v, "bedrock:")
		if len(id) < 1 || len(id) > 20 || id[0] == '0' {
			return false
		}
		for _, c := range []byte(id) {
			if c < '0' || c > '9' {
				return false
			}
		}
		n, err := strconv.ParseUint(id, 10, 64)
		return err == nil && n > 0
	}
	return false
}
