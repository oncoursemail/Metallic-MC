// Package session models Metallic-MC session ownership and player transfers.
// It is an offline, deterministic state machine. It does not authenticate
// callers, persist state, fence world files, or perform network or process I/O.
// Callers must serialize access, supply time and unique authority/instance
// incarnations, generate cryptographic tokens, and durably commit transitions.
package session

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"sort"
	"time"
)

var (
	ErrInvalidInput        = errors.New("invalid session input")
	ErrNotInitialized      = errors.New("session machine requires New")
	ErrConflict            = errors.New("session transition conflicts with current state")
	ErrStale               = errors.New("stale authority epoch or generation")
	ErrUnauthorized        = errors.New("transfer token or destination does not match")
	ErrNotFound            = errors.New("no pending transfer")
	ErrExpired             = errors.New("transfer expired")
	ErrClockRegression     = errors.New("session clock moved backward")
	ErrLimitExceeded       = errors.New("session history or pending transfer limit reached")
	ErrGenerationExhausted = errors.New("session generation exhausted")
)

type Phase string

const (
	Unowned  Phase = "unowned"
	Active   Phase = "active"
	Reserved Phase = "reserved"
	Frozen   Phase = "frozen"
)

// Fence names a single instance's authority within a nonreusable history.
// InstanceID must be a unique process incarnation, not a reusable server name.
type Fence struct {
	AuthorityEpoch string
	PlayerID       string
	InstanceID     string
	Generation     uint64
}

// Checkpoint is opaque caller-provided evidence. Nonzero fields and monotonic
// versions are checked, but this package cannot prove a save or durable flush.
type Checkpoint struct {
	Version uint64
	Digest  [32]byte
}

// TransferRef is a compare-and-swap reference, not a bearer credential.
// Freeze advances its Generation; later calls must use the returned reference.
type TransferRef struct {
	AuthorityEpoch string
	ID             string
	Generation     uint64
}

type Player struct {
	AuthorityEpoch string
	ID             string
	Generation     uint64
	OwnerInstance  string
	Phase          Phase
	TransferID     string
	Checkpoint     Checkpoint
}

// Transfer is an immutable snapshot returned by value; it contains no token or
// token digest. Source records the original owner even after Freeze fences it.
type Transfer struct {
	Ref                 TransferRef
	Source              Fence
	DestinationInstance string
	Phase               Phase
	ExpiresAt           time.Time
	Checkpoint          Checkpoint
}

type Reservation struct {
	ID                  string
	DestinationInstance string
	Token               []byte
	ExpiresAt           time.Time
}

type Limits struct {
	MaxPlayers         int
	MaxPending         int
	MaxTransferHistory int
	MaxTransferTTL     time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxPlayers: 1024, MaxPending: 128, MaxTransferHistory: 4096, MaxTransferTTL: 30 * time.Second}
}

type transferRecord struct {
	view   Transfer
	digest [32]byte
}

// Machine is not safe for concurrent use. Its player generations and used
// transfer IDs are bounded tombstones that are never evicted within an epoch.
// Terminal transfers lose their detailed record and token digest immediately.
type Machine struct {
	*machineState
}

// Keeping the clock and maps behind one private pointer makes an accidental
// value copy refer to the same history rather than fork only its clock.
type machineState struct {
	epoch    string
	limits   Limits
	lastTime time.Time
	players  map[string]Player
	pending  map[string]transferRecord
	usedIDs  map[string]struct{}
}

// New starts a fresh offline history. The caller MUST supply a globally unique
// epoch; reusing an epoch after losing state can resurrect stale authority.
func New(epoch string) (*Machine, error) { return NewWithLimits(epoch, DefaultLimits()) }

func NewWithLimits(epoch string, limits Limits) (*Machine, error) {
	if !validID(epoch) || limits.MaxPlayers < 1 || limits.MaxPlayers > 1<<20 ||
		limits.MaxPending < 1 || limits.MaxPending > 1<<16 ||
		limits.MaxTransferHistory < limits.MaxPending || limits.MaxTransferHistory > 1<<20 ||
		limits.MaxTransferTTL <= 0 || limits.MaxTransferTTL > 5*time.Minute {
		return nil, ErrInvalidInput
	}
	return &Machine{machineState: &machineState{epoch: epoch, limits: limits, players: make(map[string]Player),
		pending: make(map[string]transferRecord), usedIDs: make(map[string]struct{})}}, nil
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':') {
			return false
		}
	}
	return true
}

func validToken(token []byte) bool { return len(token) >= 32 && len(token) <= 256 }

// observe advances a high-water mark even if the command subsequently fails.
// Otherwise an expired request could be retried with an earlier supplied time.
// UTC wall instants are compared after removing Go's optional monotonic payload.
func (m *Machine) observe(now time.Time) (time.Time, error) {
	if m == nil || m.machineState == nil || m.epoch == "" {
		return time.Time{}, ErrNotInitialized
	}
	now = now.Round(0).UTC()
	if now.IsZero() || now.Year() < 1 || now.Year() > 9999 {
		return time.Time{}, ErrInvalidInput
	}
	if now.Before(m.lastTime) {
		return time.Time{}, ErrClockRegression
	}
	m.lastTime = now
	return now, nil
}

func fenceFor(p Player) Fence {
	return Fence{AuthorityEpoch: p.AuthorityEpoch, PlayerID: p.ID, InstanceID: p.OwnerInstance, Generation: p.Generation}
}

// Authorize checks current in-memory ownership only. There is no session TTL,
// caller authentication, persistence guarantee, or actual storage enforcement.
func (m *Machine) Authorize(fence Fence) bool {
	if m == nil || m.machineState == nil || m.epoch == "" || fence.AuthorityEpoch != m.epoch || fence.Generation == 0 || fence.InstanceID == "" {
		return false
	}
	p, ok := m.players[fence.PlayerID]
	return ok && (p.Phase == Active || p.Phase == Reserved) &&
		p.Generation == fence.Generation && p.OwnerInstance == fence.InstanceID
}

func (m *Machine) Player(id string) (Player, bool) {
	if m == nil || m.machineState == nil {
		return Player{}, false
	}
	p, ok := m.players[id]
	return p, ok
}

func (m *Machine) Pending(id string) (Transfer, bool) {
	if m == nil || m.machineState == nil {
		return Transfer{}, false
	}
	t, ok := m.pending[id]
	return t.view, ok
}

// Acquire grants a new generation only when no owner or pending transfer exists.
// It is an authorized controller decision; this method does not verify a login.
func (m *Machine) Acquire(now time.Time, playerID, instanceID string) (Fence, error) {
	if _, err := m.observe(now); err != nil {
		return Fence{}, err
	}
	if !validID(playerID) || !validID(instanceID) {
		return Fence{}, ErrInvalidInput
	}
	p, exists := m.players[playerID]
	if exists && p.Phase != Unowned {
		return Fence{}, ErrConflict
	}
	if !exists {
		if len(m.players) >= m.limits.MaxPlayers {
			return Fence{}, ErrLimitExceeded
		}
		p = Player{AuthorityEpoch: m.epoch, ID: playerID}
	}
	if p.Generation == ^uint64(0) {
		return Fence{}, ErrGenerationExhausted
	}
	p.Generation++
	p.OwnerInstance, p.Phase = instanceID, Active
	m.players[playerID] = p
	return fenceFor(p), nil
}

// Release is a clean disconnect. A pending transfer must be canceled or expired
// explicitly first. The generation tombstone is retained permanently in epoch.
func (m *Machine) Release(now time.Time, fence Fence) (Player, error) {
	if _, err := m.observe(now); err != nil {
		return Player{}, err
	}
	if !m.Authorize(fence) {
		return Player{}, ErrStale
	}
	p := m.players[fence.PlayerID]
	if p.Phase != Active {
		return Player{}, ErrConflict
	}
	if p.Generation == ^uint64(0) {
		return Player{}, ErrGenerationExhausted
	}
	p.Generation++
	p.OwnerInstance, p.Phase = "", Unowned
	m.players[p.ID] = p
	return p, nil
}

// Reserve retains source ownership. Tokens must be 32..256 bytes generated by
// the caller's CSPRNG; only length is checked, not entropy. Only a SHA-256 digest
// is retained. A used ID can never be reused within this authority epoch.
func (m *Machine) Reserve(now time.Time, source Fence, request Reservation) (Transfer, error) {
	now, err := m.observe(now)
	if err != nil {
		return Transfer{}, err
	}
	expires := request.ExpiresAt.Round(0).UTC()
	if !validID(request.ID) || !validID(request.DestinationInstance) || !validToken(request.Token) ||
		request.DestinationInstance == source.InstanceID || !expires.After(now) || expires.After(now.Add(m.limits.MaxTransferTTL)) {
		return Transfer{}, ErrInvalidInput
	}
	if !m.Authorize(source) {
		return Transfer{}, ErrStale
	}
	p := m.players[source.PlayerID]
	if p.Phase != Active {
		return Transfer{}, ErrConflict
	}
	if _, used := m.usedIDs[request.ID]; used {
		return Transfer{}, ErrConflict
	}
	if len(m.pending) >= m.limits.MaxPending || len(m.usedIDs) >= m.limits.MaxTransferHistory {
		return Transfer{}, ErrLimitExceeded
	}
	t := Transfer{Ref: TransferRef{AuthorityEpoch: m.epoch, ID: request.ID, Generation: p.Generation},
		Source: source, DestinationInstance: request.DestinationInstance, Phase: Reserved, ExpiresAt: expires}
	m.pending[request.ID] = transferRecord{view: t, digest: sha256.Sum256(request.Token)}
	m.usedIDs[request.ID] = struct{}{}
	p.Phase, p.TransferID = Reserved, request.ID
	m.players[p.ID] = p
	return t, nil
}

func (m *Machine) transfer(now time.Time, ref TransferRef) (transferRecord, error) {
	if ref.AuthorityEpoch != m.epoch || ref.Generation == 0 {
		return transferRecord{}, ErrStale
	}
	t, ok := m.pending[ref.ID]
	if !ok {
		return transferRecord{}, ErrNotFound
	}
	if ref != t.view.Ref {
		return transferRecord{}, ErrStale
	}
	if !now.Before(t.view.ExpiresAt) {
		return transferRecord{}, ErrExpired
	}
	return t, nil
}

// Freeze is the ownership cut: advance generation and remove the source owner.
// The resulting reference is required for Claim or Cancel. This method does not
// save a Minecraft world or independently verify the supplied checkpoint.
func (m *Machine) Freeze(now time.Time, source Fence, ref TransferRef, checkpoint Checkpoint) (Transfer, error) {
	now, err := m.observe(now)
	if err != nil {
		return Transfer{}, err
	}
	t, err := m.transfer(now, ref)
	if err != nil {
		return Transfer{}, err
	}
	if !m.Authorize(source) || source != t.view.Source {
		return Transfer{}, ErrStale
	}
	if t.view.Phase != Reserved {
		return Transfer{}, ErrConflict
	}
	if checkpoint.Version == 0 || checkpoint.Digest == [32]byte{} {
		return Transfer{}, ErrInvalidInput
	}
	p := m.players[source.PlayerID]
	if checkpoint.Version < p.Checkpoint.Version || (checkpoint.Version == p.Checkpoint.Version && checkpoint.Digest != p.Checkpoint.Digest) {
		return Transfer{}, ErrStale
	}
	if p.Generation == ^uint64(0) {
		return Transfer{}, ErrGenerationExhausted
	}
	p.Generation++
	p.OwnerInstance, p.Phase, p.Checkpoint = "", Frozen, checkpoint
	t.view.Ref.Generation, t.view.Phase, t.view.Checkpoint = p.Generation, Frozen, checkpoint
	m.players[p.ID], m.pending[ref.ID] = p, t
	return t.view, nil
}

func matchesToken(t transferRecord, token []byte) bool {
	if !validToken(token) {
		return false
	}
	digest := sha256.Sum256(token)
	return subtle.ConstantTimeCompare(digest[:], t.digest[:]) == 1
}

// Claim activates only the reserved destination at the frozen generation.
// Duplicate claims reject; no success receipt cache or network identity check
// is provided. The caller must resolve lost replies from authoritative state.
func (m *Machine) Claim(now time.Time, ref TransferRef, destinationInstance string, token []byte) (Fence, error) {
	now, err := m.observe(now)
	if err != nil {
		return Fence{}, err
	}
	t, err := m.transfer(now, ref)
	if err != nil {
		return Fence{}, err
	}
	if !matchesToken(t, token) || destinationInstance != t.view.DestinationInstance {
		return Fence{}, ErrUnauthorized
	}
	if t.view.Phase != Frozen {
		return Fence{}, ErrConflict
	}
	p := m.players[t.view.Source.PlayerID]
	p.OwnerInstance, p.Phase, p.TransferID = destinationInstance, Active, ""
	m.players[p.ID] = p
	delete(m.pending, ref.ID)
	return fenceFor(p), nil
}

// Cancel requires the exact current transfer reference and token. Cancellation
// after Freeze never restores the old source generation or assigns a new owner.
func (m *Machine) Cancel(now time.Time, ref TransferRef, token []byte) (Player, error) {
	now, err := m.observe(now)
	if err != nil {
		return Player{}, err
	}
	t, err := m.transfer(now, ref)
	if err != nil {
		return Player{}, err
	}
	if !matchesToken(t, token) {
		return Player{}, ErrUnauthorized
	}
	return m.retire(t), nil
}

func (m *Machine) retire(t transferRecord) Player {
	p := m.players[t.view.Source.PlayerID]
	p.TransferID = ""
	if t.view.Phase == Reserved {
		p.Phase = Active
	} else {
		p.OwnerInstance, p.Phase = "", Unowned
	}
	m.players[p.ID] = p
	delete(m.pending, t.view.Ref.ID)
	return p
}

// Expire explicitly retires expired pending transfers in ID order. Deadlines
// are exclusive: now == ExpiresAt is expired. Player generation and used-ID
// tombstones remain; callers must never prune those histories in a live epoch.
func (m *Machine) Expire(now time.Time) ([]TransferRef, error) {
	now, err := m.observe(now)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for id, t := range m.pending {
		if !now.Before(t.view.ExpiresAt) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	refs := make([]TransferRef, 0, len(ids))
	for _, id := range ids {
		t := m.pending[id]
		refs = append(refs, t.view.Ref)
		m.retire(t)
	}
	return refs, nil
}
