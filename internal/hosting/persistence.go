package hosting

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const storeVersion = 1
const maxStoreBytes = 32 << 20

// A failed directory sync after rename has an uncertain durable outcome. The
// caller must not continue authorizing against an older in-memory snapshot.
var errCommitUncertain = errors.New("hosting commit outcome uncertain")

type durableStore interface {
	read() ([]byte, error)
	write([]byte) error
	close() error
}

// NewPersistent opens one private local Linux store, holding its writer lock
// until Close. It rejects invalid or unsafe state instead of starting empty.
// Only persistent policy records survive; pairing tickets and route decisions do
// not. Enrollment is not evidence that a host or a Minecraft server is online.
func NewPersistent(cfg Config, path string) (*Service, error) {
	if !filepath.IsAbs(path) || strings.IndexByte(path, 0) >= 0 {
		return nil, fmt.Errorf("%w: an absolute hosting store path is required", ErrInvalidInput)
	}
	s, err := NewService(cfg)
	if err != nil {
		return nil, err
	}
	store, err := openDurableStore(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	s.store = store
	b, err := store.read()
	if errors.Is(err, os.ErrNotExist) {
		err = s.persistLocked(s.hosts, s.servers)
	} else if err == nil {
		err = s.restore(b)
	}
	if err != nil {
		_ = store.close()
		return nil, err
	}
	return s, nil
}

// Persistent reports the chosen storage mode, even after Close or a store fault.
func (s *Service) Persistent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store != nil
}

// Close is idempotent and releases the durable store's single-writer lock.
// A closed service refuses further operations. Memory-only services need no
// shutdown flush because every persistent mutation is written before success.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.tickets = nil
	if s.store != nil {
		return s.store.close()
	}
	return nil
}

func (s *Service) availableLocked() error {
	if s.closed || s.storeFailed {
		return ErrStore
	}
	return nil
}

func copyHosts(src map[string]*hostRecord) map[string]*hostRecord {
	out := make(map[string]*hostRecord, len(src)+1)
	for key, record := range src {
		out[key] = record
	}
	return out
}

func copyServers(src map[string]ServerAllocation) map[string]ServerAllocation {
	out := make(map[string]ServerAllocation, len(src)+1)
	for key, allocation := range src {
		out[key] = allocation
	}
	return out
}

type diskState struct {
	Version int                `json:"version"`
	Hosts   []diskHost         `json:"hosts"`
	Servers []ServerAllocation `json:"servers"`
}

// Revoked is a pointer so a missing/null revocation field cannot silently become
// false. No plaintext ticket or credential has a place in the disk schema.
type diskHost struct {
	ID                  string    `json:"id"`
	OwnerID             string    `json:"owner_id"`
	Name                string    `json:"name"`
	CreatedAt           time.Time `json:"created_at"`
	CredentialExpiresAt time.Time `json:"credential_expires_at"`
	Revoked             *bool     `json:"revoked"`
	CredentialDigest    string    `json:"credential_digest"`
}

func (s *Service) persistLocked(hosts map[string]*hostRecord, servers map[string]ServerAllocation) error {
	if s.store == nil {
		return nil
	}
	state := diskState{Version: storeVersion, Hosts: make([]diskHost, 0, len(hosts)), Servers: make([]ServerAllocation, 0, len(servers))}
	for _, record := range hosts {
		host := record.host
		revoked := host.Revoked
		state.Hosts = append(state.Hosts, diskHost{
			ID: host.ID, OwnerID: host.OwnerID, Name: host.Name,
			CreatedAt: host.CreatedAt.UTC(), CredentialExpiresAt: host.CredentialExpiresAt.UTC(),
			Revoked: &revoked, CredentialDigest: hex.EncodeToString(record.credentialDigest[:]),
		})
	}
	for _, allocation := range servers {
		state.Servers = append(state.Servers, allocation)
	}
	sort.Slice(state.Hosts, func(i, j int) bool { return state.Hosts[i].ID < state.Hosts[j].ID })
	sort.Slice(state.Servers, func(i, j int) bool { return state.Servers[i].ID < state.Servers[j].ID })
	// Check the same invariants on write and recovery. An invalid clock/config
	// must not produce a successful operation that cannot later be recovered.
	if _, _, err := validatedRecords(state, s.cfg); err != nil {
		return err
	}
	b, err := json.Marshal(state)
	if err != nil || len(b) > maxStoreBytes {
		return fmt.Errorf("%w: hosting snapshot exceeds storage bounds", ErrStore)
	}
	if err := s.store.write(b); err != nil {
		if errors.Is(err, errCommitUncertain) {
			s.storeFailed = true
			s.tickets = nil
		}
		return fmt.Errorf("%w: saving hosting state: %w", ErrStore, err)
	}
	return nil
}

func (s *Service) restore(b []byte) error {
	if len(b) == 0 || len(b) > maxStoreBytes || !utf8.Valid(b) {
		return fmt.Errorf("%w: invalid hosting snapshot size", ErrStore)
	}
	// encoding/json normally accepts duplicate object fields. Reject them before
	// decoding so altered revocation, owner or resource fields are unambiguous.
	keys := json.NewDecoder(bytes.NewReader(b))
	if err := checkJSONKeys(keys, 0); err != nil {
		return fmt.Errorf("%w: ambiguous or invalid hosting schema", ErrStore)
	}
	var state diskState
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&state); err != nil {
		return fmt.Errorf("%w: invalid hosting schema", ErrStore)
	}
	var extra any
	if !errors.Is(dec.Decode(&extra), io.EOF) {
		return fmt.Errorf("%w: trailing hosting data", ErrStore)
	}
	hosts, servers, err := validatedRecords(state, s.cfg)
	if err != nil {
		return err
	}
	s.hosts, s.servers = hosts, servers
	return nil
}

// Only lower-case schema keys are accepted; JSON's case-insensitive struct field
// matching must not allow a second spelling of a security-sensitive property.
func checkJSONKeys(dec *json.Decoder, depth int) error {
	if depth > 8 {
		return ErrStore
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for dec.More() {
			token, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || seen[key] || key != strings.ToLower(key) {
				return ErrStore
			}
			seen[key] = true
			if err := checkJSONKeys(dec, depth+1); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return ErrStore
		}
	case '[':
		for dec.More() {
			if err := checkJSONKeys(dec, depth+1); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return ErrStore
		}
	default:
		return ErrStore
	}
	return nil
}

func validatedRecords(state diskState, cfg Config) (map[string]*hostRecord, map[string]ServerAllocation, error) {
	bad := func() (map[string]*hostRecord, map[string]ServerAllocation, error) {
		return nil, nil, fmt.Errorf("%w: hosting snapshot violates policy bounds or integrity", ErrStore)
	}
	if state.Version != storeVersion || state.Hosts == nil || state.Servers == nil || len(state.Hosts) > cfg.MaxHosts || len(state.Servers) > cfg.MaxServers {
		return bad()
	}
	hosts := make(map[string]*hostRecord, len(state.Hosts))
	digests := make(map[[32]byte]bool, len(state.Hosts))
	for _, value := range state.Hosts {
		if !validIdentifier(value.ID) || !validOwner(value.OwnerID) || !validText(value.Name, 80) || value.Revoked == nil || !validStoreTime(value.CreatedAt) || !validStoreTime(value.CredentialExpiresAt) {
			return bad()
		}
		if !value.CredentialExpiresAt.After(value.CreatedAt) || value.CredentialExpiresAt.Sub(value.CreatedAt) > 30*24*time.Hour {
			return bad()
		}
		if _, duplicate := hosts[value.ID]; duplicate {
			return bad()
		}
		digestBytes, err := hex.DecodeString(value.CredentialDigest)
		if err != nil || len(digestBytes) != 32 || hex.EncodeToString(digestBytes) != value.CredentialDigest {
			return bad()
		}
		var digest [32]byte
		copy(digest[:], digestBytes)
		if digest == ([32]byte{}) || digests[digest] {
			return bad()
		}
		digests[digest] = true
		hosts[value.ID] = &hostRecord{host: Host{
			ID: value.ID, OwnerID: value.OwnerID, Name: value.Name, CreatedAt: value.CreatedAt,
			CredentialExpiresAt: value.CredentialExpiresAt, Revoked: *value.Revoked,
		}, credentialDigest: digest}
	}
	servers := make(map[string]ServerAllocation, len(state.Servers))
	used := make(map[string]Resources)
	ports := make(map[string]map[int]bool)
	for _, allocation := range state.Servers {
		host, exists := hosts[allocation.HostID]
		if !exists || allocation.OwnerID != host.host.OwnerID || !validIdentifier(allocation.ID) || !positiveResources(allocation.Resources) || !isGamePort(allocation.GamePort) || allocation.State != "policy_registered" || !validStoreTime(allocation.CreatedAt) {
			return bad()
		}
		if allocation.CreatedAt.Before(host.host.CreatedAt) || !allocation.CreatedAt.Before(host.host.CredentialExpiresAt) {
			return bad()
		}
		if _, duplicate := servers[allocation.ID]; duplicate {
			return bad()
		}
		if ports[allocation.HostID] == nil {
			ports[allocation.HostID] = make(map[int]bool)
		}
		if ports[allocation.HostID][allocation.GamePort] || !resourcesFit(allocation.Resources, used[allocation.HostID], cfg.MaxResources) {
			return bad()
		}
		ports[allocation.HostID][allocation.GamePort] = true
		total := used[allocation.HostID]
		total.CPUCores += allocation.Resources.CPUCores
		total.MemoryMB += allocation.Resources.MemoryMB
		total.DiskMB += allocation.Resources.DiskMB
		total.PlayerSlots += allocation.Resources.PlayerSlots
		used[allocation.HostID] = total
		servers[allocation.ID] = allocation
	}
	return hosts, servers, nil
}

func validStoreTime(value time.Time) bool {
	return !value.IsZero() && value.Year() >= 1 && value.Year() <= 9999
}
