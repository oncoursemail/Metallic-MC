// Package hosting implements an in-memory BYOH enrollment and routing policy.
// It does not start servers, authenticate players, open listeners, or relay traffic.
package hosting

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

var (
	ErrInvalidInput  = errors.New("invalid hosting input")
	ErrUnauthorized  = errors.New("unauthorized host credential or pairing ticket")
	ErrNotFound      = errors.New("owned hosting resource not found")
	ErrExpired       = errors.New("hosting credential or ticket expired")
	ErrRevoked       = errors.New("host credential revoked")
	ErrLimitExceeded = errors.New("hosting capacity limit exceeded")
	ErrConflict      = errors.New("hosting resource already registered")
)

type Config struct {
	TicketTTL     time.Duration
	CredentialTTL time.Duration
	MaxRouteTTL   time.Duration
	MaxTickets    int
	MaxHosts      int
	MaxServers    int
	MaxResources  Resources
	Now           func() time.Time
}
type Resources struct {
	CPUCores    int `json:"cpu_cores"`
	MemoryMB    int `json:"memory_mb"`
	DiskMB      int `json:"disk_mb"`
	PlayerSlots int `json:"player_slots"`
}
type PairingTicket struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Host struct {
	ID                  string    `json:"id"`
	OwnerID             string    `json:"owner_id"`
	Name                string    `json:"name"`
	CreatedAt           time.Time `json:"created_at"`
	CredentialExpiresAt time.Time `json:"credential_expires_at"`
	Revoked             bool      `json:"revoked"`
}
type Enrollment struct {
	Host       Host   `json:"host"`
	Credential string `json:"credential"`
}
type AllocationRequest struct {
	HostID    string    `json:"host_id"`
	ServerID  string    `json:"server_id"`
	Resources Resources `json:"resources"`
	GamePort  int       `json:"game_port"`
}
type ServerAllocation struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"owner_id"`
	HostID    string    `json:"host_id"`
	Resources Resources `json:"resources"`
	GamePort  int       `json:"game_port"`
	CreatedAt time.Time `json:"created_at"`
	State     string    `json:"state"`
}
type RouteRequest struct {
	ServerID   string `json:"server_id"`
	HostID     string `json:"host_id"`
	Target     string `json:"target"`
	TTLSeconds int    `json:"ttl_seconds"`
}
type RouteDecision struct {
	ServerID  string    `json:"server_id"`
	HostID    string    `json:"host_id"`
	Target    string    `json:"target"`
	ExpiresAt time.Time `json:"expires_at"`
	Decision  string    `json:"decision"`
}
type pairingRecord struct {
	ownerID   string
	digest    [32]byte
	expiresAt time.Time
}
type hostRecord struct {
	host             Host
	credentialDigest [32]byte
}

// Service is safe for concurrent use. All state is lost on process restart.
// Revoked and expired hosts remain bounded records; there is no renewal or deletion.
type Service struct {
	mu      sync.Mutex
	cfg     Config
	tickets map[[32]byte]pairingRecord
	hosts   map[string]*hostRecord
	servers map[string]ServerAllocation
}

func DefaultConfig() Config {
	return Config{
		TicketTTL: 5 * time.Minute, CredentialTTL: 24 * time.Hour, MaxRouteTTL: 5 * time.Minute,
		MaxTickets: 64, MaxHosts: 128, MaxServers: 1024,
		MaxResources: Resources{CPUCores: 16, MemoryMB: 32768, DiskMB: 524288, PlayerSlots: 200},
		Now:          time.Now,
	}
}
func NewService(cfg Config) (*Service, error) {
	defaults := DefaultConfig()
	if cfg.TicketTTL == 0 {
		cfg.TicketTTL = defaults.TicketTTL
	}
	if cfg.CredentialTTL == 0 {
		cfg.CredentialTTL = defaults.CredentialTTL
	}
	if cfg.MaxRouteTTL == 0 {
		cfg.MaxRouteTTL = defaults.MaxRouteTTL
	}
	if cfg.MaxTickets == 0 {
		cfg.MaxTickets = defaults.MaxTickets
	}
	if cfg.MaxHosts == 0 {
		cfg.MaxHosts = defaults.MaxHosts
	}
	if cfg.MaxServers == 0 {
		cfg.MaxServers = defaults.MaxServers
	}
	if cfg.MaxResources == (Resources{}) {
		cfg.MaxResources = defaults.MaxResources
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.TicketTTL <= 0 || cfg.TicketTTL > 5*time.Minute || cfg.CredentialTTL <= 0 || cfg.CredentialTTL > 30*24*time.Hour || cfg.MaxRouteTTL < time.Second || cfg.MaxRouteTTL > 5*time.Minute {
		return nil, fmt.Errorf("%w: invalid credential or route lifetime", ErrInvalidInput)
	}
	if cfg.MaxTickets < 1 || cfg.MaxTickets > 16384 || cfg.MaxHosts < 1 || cfg.MaxHosts > 4096 || cfg.MaxServers < 1 || cfg.MaxServers > 65536 {
		return nil, fmt.Errorf("%w: invalid record bounds", ErrInvalidInput)
	}
	hardLimit := Resources{CPUCores: 256, MemoryMB: 1048576, DiskMB: 16777216, PlayerSlots: 10000}
	if !positiveResources(cfg.MaxResources) || !resourcesFit(cfg.MaxResources, Resources{}, hardLimit) {
		return nil, fmt.Errorf("%w: invalid resource bounds", ErrInvalidInput)
	}
	return &Service{cfg: cfg, tickets: make(map[[32]byte]pairingRecord), hosts: make(map[string]*hostRecord), servers: make(map[string]ServerAllocation)}, nil
}

// IssuePairingTicket receives an owner established by caller authentication.
// The random 256-bit ticket is returned once; only its SHA-256 digest is stored.
func (s *Service) IssuePairingTicket(ownerID string) (PairingTicket, error) {
	if !validOwner(ownerID) {
		return PairingTicket{}, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Now()
	for digest, ticket := range s.tickets {
		if !now.Before(ticket.expiresAt) {
			delete(s.tickets, digest)
		}
	}
	if len(s.tickets) >= s.cfg.MaxTickets {
		return PairingTicket{}, ErrLimitExceeded
	}
	for attempt := 0; attempt < 8; attempt++ {
		token, err := randomToken("mtp_", 32)
		if err != nil {
			return PairingTicket{}, err
		}
		digest := sha256.Sum256([]byte(token))
		if _, exists := s.tickets[digest]; exists {
			continue
		}
		expiresAt := now.Add(s.cfg.TicketTTL)
		s.tickets[digest] = pairingRecord{ownerID: ownerID, digest: digest, expiresAt: expiresAt}
		return PairingTicket{Token: token, ExpiresAt: expiresAt}, nil
	}
	return PairingTicket{}, ErrConflict
}

// RedeemPairingTicket atomically consumes a ticket for its authenticated owner.
// The independent random credential is returned once and stored only hashed.
func (s *Service) RedeemPairingTicket(ownerID, ticket, hostName string) (Enrollment, error) {
	if !validOwner(ownerID) || !validText(hostName, 80) {
		return Enrollment{}, ErrInvalidInput
	}
	digest, ok := tokenDigest(ticket, "mtp_")
	if !ok {
		return Enrollment{}, ErrUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.tickets[digest]
	if !exists || subtle.ConstantTimeCompare(record.digest[:], digest[:]) != 1 || record.ownerID != ownerID {
		return Enrollment{}, ErrUnauthorized
	}
	now := s.cfg.Now()
	if !now.Before(record.expiresAt) {
		delete(s.tickets, digest)
		return Enrollment{}, ErrExpired
	}
	if len(s.hosts) >= s.cfg.MaxHosts {
		return Enrollment{}, ErrLimitExceeded
	}
	var hostID string
	for attempt := 0; attempt < 8; attempt++ {
		candidate, err := randomToken("host_", 16)
		if err != nil {
			return Enrollment{}, err
		}
		if _, exists := s.hosts[candidate]; !exists {
			hostID = candidate
			break
		}
	}
	if hostID == "" {
		return Enrollment{}, ErrConflict
	}
	credential, err := randomToken("mhc_", 32)
	if err != nil {
		return Enrollment{}, err
	}
	host := Host{ID: hostID, OwnerID: ownerID, Name: hostName, CreatedAt: now, CredentialExpiresAt: now.Add(s.cfg.CredentialTTL)}
	s.hosts[hostID] = &hostRecord{host: host, credentialDigest: sha256.Sum256([]byte(credential))}
	delete(s.tickets, digest)
	return Enrollment{Host: host, Credential: credential}, nil
}
func (s *Service) ListHosts(ownerID string) ([]Host, error) {
	if !validOwner(ownerID) {
		return nil, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Host, 0)
	for _, record := range s.hosts {
		if record.host.OwnerID == ownerID {
			result = append(result, record.host)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
func (s *Service) RevokeHost(ownerID, hostID string) error {
	if !validOwner(ownerID) || !validIdentifier(hostID) {
		return ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.hosts[hostID]
	if !exists || record.host.OwnerID != ownerID {
		return ErrNotFound
	}
	record.host.Revoked = true
	return nil
}

// AllocateServer records a policy allocation, not a deployed or running server.
// Resources are positive integers and bounded in aggregate for the enrolled host.
func (s *Service) AllocateServer(ownerID string, request AllocationRequest) (ServerAllocation, error) {
	if !validOwner(ownerID) || !validIdentifier(request.HostID) || !validIdentifier(request.ServerID) || !positiveResources(request.Resources) || !isGamePort(request.GamePort) {
		return ServerAllocation{}, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	host, exists := s.hosts[request.HostID]
	if !exists || host.host.OwnerID != ownerID {
		return ServerAllocation{}, ErrNotFound
	}
	now := s.cfg.Now()
	if err := activeHost(host, now); err != nil {
		return ServerAllocation{}, err
	}
	if _, exists := s.servers[request.ServerID]; exists {
		return ServerAllocation{}, ErrConflict
	}
	if len(s.servers) >= s.cfg.MaxServers {
		return ServerAllocation{}, ErrLimitExceeded
	}
	used := Resources{}
	for _, allocation := range s.servers {
		if allocation.HostID != request.HostID {
			continue
		}
		if allocation.GamePort == request.GamePort {
			return ServerAllocation{}, ErrConflict
		}
		used.CPUCores += allocation.Resources.CPUCores
		used.MemoryMB += allocation.Resources.MemoryMB
		used.DiskMB += allocation.Resources.DiskMB
		used.PlayerSlots += allocation.Resources.PlayerSlots
	}
	if !resourcesFit(request.Resources, used, s.cfg.MaxResources) {
		return ServerAllocation{}, ErrLimitExceeded
	}
	allocation := ServerAllocation{ID: request.ServerID, OwnerID: ownerID, HostID: request.HostID, Resources: request.Resources, GamePort: request.GamePort, CreatedAt: now, State: "policy_registered"}
	s.servers[allocation.ID] = allocation
	return allocation, nil
}
func (s *Service) ListServers(ownerID string) ([]ServerAllocation, error) {
	if !validOwner(ownerID) {
		return nil, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]ServerAllocation, 0)
	for _, allocation := range s.servers {
		if allocation.OwnerID == ownerID {
			result = append(result, allocation)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// PlanRoute is a decision only, not a signed capability or live network route.
// Owner, exact host, credential, allocation, port, loopback and lifetime must match.
func (s *Service) PlanRoute(ownerID, hostCredential string, request RouteRequest) (RouteDecision, error) {
	if !validOwner(ownerID) || !validIdentifier(request.HostID) || !validIdentifier(request.ServerID) || request.TTLSeconds <= 0 || request.TTLSeconds > int(s.cfg.MaxRouteTTL/time.Second) || len(request.Target) > 64 {
		return RouteDecision{}, ErrInvalidInput
	}
	target, err := netip.ParseAddrPort(request.Target)
	if err != nil || target.Addr().Zone() != "" || (target.Addr() != netip.MustParseAddr("127.0.0.1") && target.Addr() != netip.MustParseAddr("::1")) || !isGamePort(int(target.Port())) {
		return RouteDecision{}, ErrInvalidInput
	}
	digest, ok := tokenDigest(hostCredential, "mhc_")
	if !ok {
		return RouteDecision{}, ErrUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	host, exists := s.hosts[request.HostID]
	if !exists || host.host.OwnerID != ownerID {
		return RouteDecision{}, ErrNotFound
	}
	if subtle.ConstantTimeCompare(host.credentialDigest[:], digest[:]) != 1 {
		return RouteDecision{}, ErrUnauthorized
	}
	now := s.cfg.Now()
	if err := activeHost(host, now); err != nil {
		return RouteDecision{}, err
	}
	allocation, exists := s.servers[request.ServerID]
	if !exists || allocation.OwnerID != ownerID || allocation.HostID != host.host.ID {
		return RouteDecision{}, ErrNotFound
	}
	if allocation.GamePort != int(target.Port()) {
		return RouteDecision{}, ErrInvalidInput
	}
	expiresAt := now.Add(time.Duration(request.TTLSeconds) * time.Second)
	if expiresAt.After(host.host.CredentialExpiresAt) {
		return RouteDecision{}, ErrExpired
	}
	return RouteDecision{ServerID: allocation.ID, HostID: host.host.ID, Target: target.String(), ExpiresAt: expiresAt, Decision: "policy_allowed"}, nil
}
func activeHost(record *hostRecord, now time.Time) error {
	if record.host.Revoked {
		return ErrRevoked
	}
	if !now.Before(record.host.CredentialExpiresAt) {
		return ErrExpired
	}
	return nil
}
func randomToken(prefix string, bytes int) (string, error) {
	entropy := make([]byte, bytes)
	if _, err := rand.Read(entropy); err != nil {
		return "", fmt.Errorf("generate hosting secret: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(entropy), nil
}
func tokenDigest(token, prefix string) ([32]byte, bool) {
	if len(token) != len(prefix)+43 || !strings.HasPrefix(token, prefix) {
		return [32]byte{}, false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token[len(prefix):])
	if err != nil || len(decoded) != 32 {
		return [32]byte{}, false
	}
	return sha256.Sum256([]byte(token)), true
}
func validOwner(ownerID string) bool { return validText(ownerID, 128) }
func validText(value string, max int) bool {
	return len(value) > 0 && len(value) <= max && strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}
func validIdentifier(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		if i > 0 && (r == '-' || r == '_' || r == '.') {
			continue
		}
		return false
	}
	return true
}
func positiveResources(value Resources) bool {
	return value.CPUCores > 0 && value.MemoryMB > 0 && value.DiskMB > 0 && value.PlayerSlots > 0
}
func resourcesFit(request, used, limit Resources) bool {
	return request.CPUCores <= limit.CPUCores-used.CPUCores && request.MemoryMB <= limit.MemoryMB-used.MemoryMB && request.DiskMB <= limit.DiskMB-used.DiskMB && request.PlayerSlots <= limit.PlayerSlots-used.PlayerSlots
}
func isGamePort(port int) bool {
	return port == 19132 || port == 19133 || (port >= 25565 && port <= 25574)
}
