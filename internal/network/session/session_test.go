package session

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"
)

var testTime = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func testMachine(t *testing.T) *Machine {
	t.Helper()
	m, err := New("authority-epoch-a")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func testToken(n byte) []byte { return bytes.Repeat([]byte{n}, 32) }

func checkpoint(version uint64) Checkpoint {
	return Checkpoint{Version: version, Digest: sha256.Sum256([]byte(fmt.Sprintf("checkpoint-%d", version)))}
}

func acquire(t *testing.T, m *Machine, player, instance string) Fence {
	t.Helper()
	fence, err := m.Acquire(testTime, player, instance)
	if err != nil {
		t.Fatal(err)
	}
	return fence
}

func reserve(t *testing.T, m *Machine, source Fence, id string, token []byte) Transfer {
	t.Helper()
	transfer, err := m.Reserve(testTime, source, Reservation{ID: id, DestinationInstance: "instance-b",
		Token: token, ExpiresAt: testTime.Add(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	return transfer
}

func freeze(t *testing.T, m *Machine, source Fence, transfer Transfer) Transfer {
	t.Helper()
	transfer, err := m.Freeze(testTime, source, transfer.Ref, checkpoint(1))
	if err != nil {
		t.Fatal(err)
	}
	return transfer
}

func wantError(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func playerState(t *testing.T, m *Machine, id string) Player {
	t.Helper()
	p, ok := m.Player(id)
	if !ok {
		t.Fatalf("player %s missing", id)
	}
	return p
}

func TestOwnershipCutAndOneTimeClaim(t *testing.T) {
	m := testMachine(t)
	source := acquire(t, m, "player-a", "instance-a")
	token := testToken(1)
	transfer := reserve(t, m, source, "transfer-a", token)
	if !m.Authorize(source) {
		t.Fatal("reservation prematurely removed source authority")
	}
	_, err := m.Claim(testTime, transfer.Ref, "instance-b", token)
	wantError(t, err, ErrConflict)
	_, err = m.Release(testTime, source)
	wantError(t, err, ErrConflict)
	frozen := freeze(t, m, source, transfer)
	if frozen.Ref.Generation != source.Generation+1 || m.Authorize(source) {
		t.Fatal("freeze did not fence the source")
	}
	p := playerState(t, m, source.PlayerID)
	if p.OwnerInstance != "" || p.Phase != Frozen {
		t.Fatalf("freeze assigned an owner: %+v", p)
	}
	_, err = m.Claim(testTime, transfer.Ref, "instance-b", token)
	wantError(t, err, ErrStale)
	_, err = m.Acquire(testTime, source.PlayerID, "instance-c")
	wantError(t, err, ErrConflict)
	destination, err := m.Claim(testTime, frozen.Ref, "instance-b", token)
	if err != nil || !m.Authorize(destination) || m.Authorize(source) {
		t.Fatalf("claim ownership incorrect: %+v, %v", destination, err)
	}
	if destination.Generation != frozen.Ref.Generation {
		t.Fatal("claim changed the reserved frozen generation")
	}
	_, err = m.Claim(testTime, frozen.Ref, "instance-b", token)
	wantError(t, err, ErrNotFound)
	_, err = m.Cancel(testTime, frozen.Ref, token)
	wantError(t, err, ErrNotFound)
	if _, ok := m.Pending(transfer.Ref.ID); ok {
		t.Fatal("terminal transfer retained token-bearing record")
	}
}

func TestReservedCancellationRetainsOwnerButRejectsReplay(t *testing.T) {
	m := testMachine(t)
	source := acquire(t, m, "player-a", "instance-a")
	token := testToken(2)
	transfer := reserve(t, m, source, "transfer-a", token)
	p, err := m.Cancel(testTime, transfer.Ref, token)
	if err != nil || p.Phase != Active || !m.Authorize(source) || p.Generation != source.Generation {
		t.Fatalf("reserved cancel changed authority: %+v, %v", p, err)
	}
	_, err = m.Reserve(testTime, source, Reservation{ID: transfer.Ref.ID, DestinationInstance: "instance-b",
		Token: token, ExpiresAt: transfer.ExpiresAt})
	wantError(t, err, ErrConflict)
	_, err = m.Cancel(testTime, transfer.Ref, token)
	wantError(t, err, ErrNotFound)
	reserve(t, m, source, "transfer-b", token)
}

func TestFrozenCancellationNeverRevivesOldOwner(t *testing.T) {
	m := testMachine(t)
	source := acquire(t, m, "player-a", "instance-a")
	token := testToken(3)
	transfer := freeze(t, m, source, reserve(t, m, source, "transfer-a", token))
	p, err := m.Cancel(testTime, transfer.Ref, token)
	if err != nil || p.OwnerInstance != "" || p.Phase != Unowned || p.Generation != transfer.Ref.Generation {
		t.Fatalf("frozen cancel restored ownership: %+v, %v", p, err)
	}
	if m.Authorize(source) {
		t.Fatal("old generation revived")
	}
	newFence := acquire(t, m, source.PlayerID, source.InstanceID)
	if newFence.Generation != source.Generation+2 || !m.Authorize(newFence) || m.Authorize(source) {
		t.Fatal("reacquisition did not establish a new generation")
	}
	_, err = m.Claim(testTime, transfer.Ref, "instance-b", token)
	wantError(t, err, ErrNotFound)
}

func TestDeadlineBoundaryAndExpiration(t *testing.T) {
	for _, frozen := range []bool{false, true} {
		t.Run(fmt.Sprintf("frozen-%v", frozen), func(t *testing.T) {
			m := testMachine(t)
			source := acquire(t, m, "player-a", "instance-a")
			token := testToken(4)
			transfer := reserve(t, m, source, "transfer-a", token)
			if frozen {
				transfer = freeze(t, m, source, transfer)
			}
			_, err := m.Claim(transfer.ExpiresAt, transfer.Ref, "instance-b", token)
			wantError(t, err, ErrExpired)
			_, err = m.Cancel(transfer.ExpiresAt, transfer.Ref, token)
			wantError(t, err, ErrExpired)
			refs, err := m.Expire(transfer.ExpiresAt)
			if err != nil || !reflect.DeepEqual(refs, []TransferRef{transfer.Ref}) {
				t.Fatalf("expiration result = %+v, %v", refs, err)
			}
			p := playerState(t, m, source.PlayerID)
			if frozen {
				if p.Phase != Unowned || p.OwnerInstance != "" || m.Authorize(source) {
					t.Fatal("expired frozen transfer reactivated source")
				}
				fence, err := m.Acquire(transfer.ExpiresAt, source.PlayerID, "instance-c")
				if err != nil || fence.Generation != transfer.Ref.Generation+1 {
					t.Fatalf("post-expiry reacquisition = %+v, %v", fence, err)
				}
			} else if p.Phase != Active || !m.Authorize(source) {
				t.Fatal("expired reservation removed source authority")
			}
		})
	}
	t.Run("claim-immediately-before-deadline", func(t *testing.T) {
		m := testMachine(t)
		source := acquire(t, m, "player-a", "instance-a")
		token := testToken(5)
		transfer := freeze(t, m, source, reserve(t, m, source, "transfer-a", token))
		if _, err := m.Claim(transfer.ExpiresAt.Add(-time.Nanosecond), transfer.Ref, "instance-b", token); err != nil {
			t.Fatal(err)
		}
	})
}

func TestWrongTokenTargetAndReferenceDoNotChangeOwnership(t *testing.T) {
	m := testMachine(t)
	source := acquire(t, m, "player-a", "instance-a")
	token := testToken(6)
	transfer := freeze(t, m, source, reserve(t, m, source, "transfer-a", token))
	before := playerState(t, m, source.PlayerID)
	for _, wrong := range [][]byte{nil, testToken(7), token[:31], append(append([]byte{}, token...), 1), make([]byte, 257)} {
		_, err := m.Claim(testTime, transfer.Ref, "instance-b", wrong)
		wantError(t, err, ErrUnauthorized)
		_, err = m.Cancel(testTime, transfer.Ref, wrong)
		wantError(t, err, ErrUnauthorized)
	}
	_, err := m.Claim(testTime, transfer.Ref, "replacement-with-same-name-new-incarnation", token)
	wantError(t, err, ErrUnauthorized)
	for _, ref := range []TransferRef{
		{AuthorityEpoch: "other-authority", ID: transfer.Ref.ID, Generation: transfer.Ref.Generation},
		{AuthorityEpoch: transfer.Ref.AuthorityEpoch, ID: transfer.Ref.ID, Generation: transfer.Ref.Generation - 1},
		{AuthorityEpoch: transfer.Ref.AuthorityEpoch, ID: transfer.Ref.ID, Generation: transfer.Ref.Generation + 1},
	} {
		_, err := m.Claim(testTime, ref, "instance-b", token)
		wantError(t, err, ErrStale)
		_, err = m.Cancel(testTime, ref, token)
		wantError(t, err, ErrStale)
	}
	if got := playerState(t, m, source.PlayerID); got != before {
		t.Fatalf("rejected operation changed player: %+v -> %+v", before, got)
	}
}

func TestFreshAuthorityEpochRejectsOldHistoryEvenWithMatchingNames(t *testing.T) {
	first := testMachine(t)
	second, err := New("authority-epoch-b")
	if err != nil {
		t.Fatal(err)
	}
	token := testToken(8)
	oldFence := acquire(t, first, "player-a", "instance-a")
	newFence := acquire(t, second, "player-a", "instance-a")
	if second.Authorize(oldFence) || first.Authorize(newFence) {
		t.Fatal("cross-history fence accepted")
	}
	oldTransfer := freeze(t, first, oldFence, reserve(t, first, oldFence, "transfer-a", token))
	newTransfer := freeze(t, second, newFence, reserve(t, second, newFence, "transfer-a", token))
	_, err = second.Claim(testTime, oldTransfer.Ref, "instance-b", token)
	wantError(t, err, ErrStale)
	_, err = second.Cancel(testTime, oldTransfer.Ref, token)
	wantError(t, err, ErrStale)
	if _, err := second.Claim(testTime, newTransfer.Ref, "instance-b", token); err != nil {
		t.Fatal(err)
	}
}

func TestFailedExpiredRequestStillPreventsClockRollback(t *testing.T) {
	m := testMachine(t)
	source := acquire(t, m, "player-a", "instance-a")
	token := testToken(9)
	transfer := freeze(t, m, source, reserve(t, m, source, "transfer-a", token))
	_, err := m.Claim(transfer.ExpiresAt, transfer.Ref, "instance-b", token)
	wantError(t, err, ErrExpired)
	_, err = m.Claim(testTime, transfer.Ref, "instance-b", token)
	wantError(t, err, ErrClockRegression)
	_, err = m.Expire(testTime)
	wantError(t, err, ErrClockRegression)
	_, err = m.Acquire(testTime, "player-other", "instance-other")
	wantError(t, err, ErrClockRegression)
	if _, err := m.Expire(transfer.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if m.Authorize(source) || playerState(t, m, source.PlayerID).OwnerInstance != "" {
		t.Fatal("expired authority revived")
	}
}

func TestMachineValueCopyCannotForkClockFromSharedAuthority(t *testing.T) {
	m := testMachine(t)
	source := acquire(t, m, "player-a", "instance-a")
	token := testToken(19)
	transfer := freeze(t, m, source, reserve(t, m, source, "transfer-a", token))
	alias := *m
	_, err := m.Claim(transfer.ExpiresAt, transfer.Ref, "instance-b", token)
	wantError(t, err, ErrExpired)
	_, err = alias.Claim(testTime, transfer.Ref, "instance-b", token)
	wantError(t, err, ErrClockRegression)
	if _, err := alias.Expire(transfer.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if got := playerState(t, m, source.PlayerID); got.Phase != Unowned || got.OwnerInstance != "" {
		t.Fatal("copied machine did not retain the same fenced history")
	}
}

func TestLimitsRetainGenerationAndTransferTombstones(t *testing.T) {
	limits := Limits{MaxPlayers: 1, MaxPending: 1, MaxTransferHistory: 2, MaxTransferTTL: 10 * time.Second}
	m, err := NewWithLimits("bounded-epoch", limits)
	if err != nil {
		t.Fatal(err)
	}
	source := acquire(t, m, "player-a", "instance-a")
	if _, err := m.Release(testTime, source); err != nil {
		t.Fatal(err)
	}
	_, err = m.Acquire(testTime, "player-b", "instance-b")
	wantError(t, err, ErrLimitExceeded)
	fresh := acquire(t, m, source.PlayerID, source.InstanceID)
	if fresh.Generation <= source.Generation || m.Authorize(source) {
		t.Fatal("player tombstone discarded generation")
	}
	for _, id := range []string{"transfer-a", "transfer-b"} {
		transfer := reserve(t, m, fresh, id, testToken(10))
		if _, err := m.Cancel(testTime, transfer.Ref, testToken(10)); err != nil {
			t.Fatal(err)
		}
	}
	_, err = m.Reserve(testTime, fresh, Reservation{ID: "transfer-c", DestinationInstance: "instance-b",
		Token: testToken(10), ExpiresAt: testTime.Add(time.Second)})
	wantError(t, err, ErrLimitExceeded)
	_, err = m.Reserve(testTime, fresh, Reservation{ID: "transfer-a", DestinationInstance: "instance-b",
		Token: testToken(10), ExpiresAt: testTime.Add(time.Second)})
	wantError(t, err, ErrConflict)
	if len(m.pending) != 0 || len(m.usedIDs) != 2 || len(m.players) != 1 {
		t.Fatal("bounds or terminal retention incorrect")
	}
}

func TestPendingLimitAndDeterministicExpiryOrder(t *testing.T) {
	limits := Limits{MaxPlayers: 3, MaxPending: 2, MaxTransferHistory: 4, MaxTransferTTL: 10 * time.Second}
	m, err := NewWithLimits("bounded-epoch", limits)
	if err != nil {
		t.Fatal(err)
	}
	a := acquire(t, m, "player-a", "instance-a")
	b := acquire(t, m, "player-b", "instance-a")
	c := acquire(t, m, "player-c", "instance-a")
	z := reserve(t, m, a, "z-first-created", testToken(11))
	aTransfer := reserve(t, m, b, "a-second-created", testToken(12))
	_, err = m.Reserve(testTime, c, Reservation{ID: "waiting", DestinationInstance: "instance-b", Token: testToken(13), ExpiresAt: z.ExpiresAt})
	wantError(t, err, ErrLimitExceeded)
	refs, err := m.Expire(z.ExpiresAt)
	if err != nil || !reflect.DeepEqual(refs, []TransferRef{aTransfer.Ref, z.Ref}) {
		t.Fatalf("expiration order = %+v, %v", refs, err)
	}
	if _, err := m.Reserve(z.ExpiresAt, c, Reservation{ID: "waiting", DestinationInstance: "instance-b", Token: testToken(13), ExpiresAt: z.ExpiresAt.Add(time.Second)}); err != nil {
		t.Fatal("failed reservation consumed history or capacity", err)
	}
}

func TestTokenAndSnapshotCopiesCannotMutateMachine(t *testing.T) {
	m := testMachine(t)
	source := acquire(t, m, "player-a", "instance-a")
	token := testToken(14)
	original := append([]byte{}, token...)
	transfer := reserve(t, m, source, "transfer-a", token)
	token[0] ^= 1
	view, _ := m.Pending(transfer.Ref.ID)
	view.DestinationInstance, view.Ref.Generation = "attacker", 999
	p := playerState(t, m, source.PlayerID)
	p.OwnerInstance, p.Generation = "attacker", 999
	if !m.Authorize(source) {
		t.Fatal("returned player view mutated authority")
	}
	frozen := freeze(t, m, source, transfer)
	_, err := m.Claim(testTime, frozen.Ref, "instance-b", token)
	wantError(t, err, ErrUnauthorized)
	if _, err := m.Claim(testTime, frozen.Ref, "instance-b", original); err != nil {
		t.Fatal("caller token mutation changed stored digest", err)
	}
}

func TestCheckpointVersionCannotRegressOrChangeAtSameVersion(t *testing.T) {
	m := testMachine(t)
	source := acquire(t, m, "player-a", "instance-a")
	token := testToken(15)
	transfer := reserve(t, m, source, "transfer-a", token)
	for _, cp := range []Checkpoint{{}, {Version: 1}, {Digest: checkpoint(1).Digest}} {
		_, err := m.Freeze(testTime, source, transfer.Ref, cp)
		wantError(t, err, ErrInvalidInput)
	}
	frozen, err := m.Freeze(testTime, source, transfer.Ref, checkpoint(2))
	if err != nil {
		t.Fatal(err)
	}
	destination, err := m.Claim(testTime, frozen.Ref, "instance-b", token)
	if err != nil {
		t.Fatal(err)
	}
	next, err := m.Reserve(testTime, destination, Reservation{ID: "transfer-b", DestinationInstance: "instance-c", Token: token, ExpiresAt: transfer.ExpiresAt})
	if err != nil {
		t.Fatal(err)
	}
	for _, cp := range []Checkpoint{checkpoint(1), {Version: 2, Digest: checkpoint(3).Digest}} {
		_, err := m.Freeze(testTime, destination, next.Ref, cp)
		wantError(t, err, ErrStale)
	}
	if _, err := m.Freeze(testTime, destination, next.Ref, checkpoint(2)); err != nil {
		t.Fatal("unchanged checkpoint should be allowed", err)
	}
}

func TestGenerationOverflowNeverWraps(t *testing.T) {
	for _, operation := range []string{"acquire", "release", "freeze"} {
		t.Run(operation, func(t *testing.T) {
			m := testMachine(t)
			fence := acquire(t, m, "player-a", "instance-a")
			p := m.players[fence.PlayerID]
			p.Generation = ^uint64(0)
			m.players[p.ID] = p
			fence.Generation = p.Generation
			var err error
			switch operation {
			case "acquire":
				p.OwnerInstance, p.Phase = "", Unowned
				m.players[p.ID] = p
				_, err = m.Acquire(testTime, p.ID, "instance-c")
			case "release":
				_, err = m.Release(testTime, fence)
			case "freeze":
				transfer := reserve(t, m, fence, "transfer-a", testToken(16))
				_, err = m.Freeze(testTime, fence, transfer.Ref, checkpoint(1))
			}
			wantError(t, err, ErrGenerationExhausted)
			if got := playerState(t, m, p.ID).Generation; got != ^uint64(0) {
				t.Fatalf("generation wrapped to %d", got)
			}
		})
	}
}

func TestInvalidInitializationAndInputs(t *testing.T) {
	for _, epoch := range []string{"", "spaces are invalid", "a/b", string(bytes.Repeat([]byte{'a'}, 129))} {
		_, err := New(epoch)
		wantError(t, err, ErrInvalidInput)
	}
	for _, limits := range []Limits{{}, {MaxPlayers: 1, MaxPending: 2, MaxTransferHistory: 1, MaxTransferTTL: time.Second},
		{MaxPlayers: 1, MaxPending: 1, MaxTransferHistory: 1, MaxTransferTTL: 6 * time.Minute}} {
		_, err := NewWithLimits("epoch", limits)
		wantError(t, err, ErrInvalidInput)
	}
	var zero Machine
	_, err := zero.Acquire(testTime, "player-a", "instance-a")
	wantError(t, err, ErrNotInitialized)
	var absent *Machine
	_, err = absent.Expire(testTime)
	wantError(t, err, ErrNotInitialized)
	if zero.Authorize(Fence{}) || absent.Authorize(Fence{}) {
		t.Fatal("uninitialized machine granted authority")
	}
	m := testMachine(t)
	_, err = m.Acquire(time.Time{}, "player-a", "instance-a")
	wantError(t, err, ErrInvalidInput)
	_, err = m.Acquire(testTime, "", "instance-a")
	wantError(t, err, ErrInvalidInput)
	source := acquire(t, m, "player-a", "instance-a")
	base := Reservation{ID: "transfer-a", DestinationInstance: "instance-b", Token: testToken(17), ExpiresAt: testTime.Add(time.Second)}
	for _, change := range []func(*Reservation){
		func(r *Reservation) { r.Token = make([]byte, 31) },
		func(r *Reservation) { r.Token = make([]byte, 257) },
		func(r *Reservation) { r.DestinationInstance = source.InstanceID },
		func(r *Reservation) { r.ExpiresAt = testTime },
		func(r *Reservation) { r.ExpiresAt = testTime.Add(31 * time.Second) },
		func(r *Reservation) { r.ID = "" },
	} {
		request := base
		change(&request)
		_, err := m.Reserve(testTime, source, request)
		wantError(t, err, ErrInvalidInput)
	}
	if len(m.usedIDs) != 0 || len(m.pending) != 0 || !m.Authorize(source) {
		t.Fatal("invalid input consumed capacity or changed ownership")
	}
}

func TestRandomizedCommandsPreserveAuthorityAndHistory(t *testing.T) {
	for _, seed := range []int64{1, 7, 41, 12345, 20261020} {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			limits := Limits{MaxPlayers: 6, MaxPending: 4, MaxTransferHistory: 256, MaxTransferTTL: 50 * time.Millisecond}
			m, err := NewWithLimits("random-epoch", limits)
			if err != nil {
				t.Fatal(err)
			}
			rng := rand.New(rand.NewSource(seed)) // deterministic fixture, never production token generation
			issued := make(map[Fence]bool)
			invalidated := make(map[Fence]bool)
			generations := make(map[string]uint64)
			tokens := make(map[string][]byte)
			var refs []TransferRef
			for step := 0; step < 2000; step++ {
				now := testTime.Add(time.Duration(step) * time.Millisecond)
				callTime := now
				if step > 2 && rng.Intn(20) == 0 {
					callTime = now.Add(-2 * time.Millisecond)
				}
				playerID := fmt.Sprintf("player-%d", rng.Intn(8))
				instance := fmt.Sprintf("instance-%d", rng.Intn(4))
				p, exists := m.Player(playerID)
				fence := fenceFor(p)
				if rng.Intn(6) == 0 {
					fence.Generation++
				}
				var ref TransferRef
				if exists && p.TransferID != "" {
					view, _ := m.Pending(p.TransferID)
					ref = view.Ref
				} else if len(refs) > 0 {
					ref = refs[rng.Intn(len(refs))]
				}
				switch rng.Intn(8) {
				case 0:
					if f, err := m.Acquire(callTime, playerID, instance); err == nil {
						issued[f] = true
					}
				case 1:
					_, _ = m.Release(callTime, fence)
				case 2:
					id := fmt.Sprintf("transfer-%d", rng.Intn(400))
					token := testToken(byte(rng.Intn(255)))
					request := Reservation{ID: id, DestinationInstance: instance, Token: token, ExpiresAt: now.Add(10 * time.Millisecond)}
					if view, err := m.Reserve(callTime, fence, request); err == nil {
						tokens[id], refs = token, append(refs, view.Ref)
					}
				case 3:
					if view, err := m.Freeze(callTime, fence, ref, checkpoint(p.Checkpoint.Version+1)); err == nil {
						refs = append(refs, view.Ref)
					}
				case 4:
					token := tokens[ref.ID]
					if rng.Intn(3) == 0 {
						token = testToken(254)
					}
					if f, err := m.Claim(callTime, ref, instance, token); err == nil {
						issued[f] = true
					}
				case 5:
					_, _ = m.Cancel(callTime, ref, tokens[ref.ID])
				case 6:
					_, _ = m.Expire(callTime)
				case 7:
					ref.AuthorityEpoch = "old-epoch"
					_, _ = m.Cancel(callTime, ref, tokens[ref.ID])
				}
				if len(m.players) > limits.MaxPlayers || len(m.pending) > limits.MaxPending || len(m.usedIDs) > limits.MaxTransferHistory {
					t.Fatalf("step %d: history bound exceeded", step)
				}
				authorized := make(map[string]int)
				for f := range issued {
					if m.Authorize(f) {
						authorized[f.PlayerID]++
						if invalidated[f] {
							t.Fatalf("step %d: previously fenced authority revived: %+v", step, f)
						}
					} else {
						invalidated[f] = true
					}
				}
				ids := make([]string, 0, len(m.players))
				for id := range m.players {
					ids = append(ids, id)
				}
				sort.Strings(ids)
				for _, id := range ids {
					p := m.players[id]
					if p.Generation < generations[id] || p.Generation == 0 || p.AuthorityEpoch != m.epoch {
						t.Fatalf("step %d: history regressed: %+v", step, p)
					}
					generations[id] = p.Generation
					expectOwner := p.Phase == Active || p.Phase == Reserved
					if authorized[id] > 1 || (authorized[id] == 1) != expectOwner || (p.OwnerInstance != "") != expectOwner {
						t.Fatalf("step %d: owner invariant failed: %+v; valid issued fences %d", step, p, authorized[id])
					}
					if (p.TransferID != "") != (p.Phase == Reserved || p.Phase == Frozen) {
						t.Fatalf("step %d: pending phase mismatch: %+v", step, p)
					}
					if p.TransferID != "" {
						tx, ok := m.pending[p.TransferID]
						if !ok || tx.view.Ref.Generation != p.Generation || tx.view.Phase != p.Phase || tx.view.Source.PlayerID != id {
							t.Fatalf("step %d: transfer and authority disagree", step)
						}
					}
				}
				for id, tx := range m.pending {
					if _, ok := m.usedIDs[id]; !ok || m.players[tx.view.Source.PlayerID].TransferID != id {
						t.Fatalf("step %d: orphaned pending transfer", step)
					}
				}
			}
		})
	}
}
