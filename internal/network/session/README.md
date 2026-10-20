# Metallic-MC session authority model

This package is an **offline, deterministic state machine** for one player's ownership during an instance transfer. It is an executable contract for later controller and game-adapter work. It does not run a service, authenticate a Minecraft player, persist state, launch containers, flush a world, or prevent a JVM from writing files.

## Ownership transitions

| Operation | Before | After | Authority |
| --- | --- | --- | --- |
| `Acquire` | Unknown or unowned player at generation g | Active at g+1 | New named instance |
| `Reserve` | Active A,g | Reserved A,g | A remains authorized |
| `Freeze` | Reserved A,g with matching reference | Frozen at g+1 | No active owner |
| `Claim` | Frozen at g+1 | Active B,g+1 | Only the named destination, with matching token |
| `Cancel` / `Expire`, before freeze | Reserved A,g | Active A,g | Source remains authorized |
| `Cancel` / `Expire`, after freeze | Frozen at g+1 | Unowned at g+1 | No owner; reacquisition must advance again |
| `Release` | Active A,g, no pending transfer | Unowned at g+1 | Old fence is invalid |

The invariant is **at most one authorized instance**, including intentional periods without any owner. `Freeze` is the ownership cut. A canceled or expired frozen transfer never revives its source generation. `Release` requires a pending transfer to be explicitly resolved first.

Every fence and transfer reference carries an `AuthorityEpoch`. `New(epoch)` starts an empty history, so the caller must supply an epoch that has never identified another history. Recreating a machine with the same epoch after losing its state is unsafe. The package cannot establish global uniqueness. Instance IDs must also identify unique incarnations rather than reusable names or PIDs.

## API responsibilities

Use `New` for the default limits, or `NewWithLimits` for an explicitly bounded laboratory experiment. Mutation methods take an injected `time.Time`; the package never reads a clock, generates randomness, or starts a goroutine. A timestamp before the highest observed timestamp is rejected, including after a failed expired claim. An equal timestamp is allowed. The clock high-water mark may advance on an otherwise rejected command; ownership does not.

`Reserve` accepts a caller-generated token of 32 through 256 bytes. The caller must use a cryptographically secure random generator and deliver the token through an authenticated confidential channel. The state machine checks length, not entropy. It retains only a SHA-256 digest and uses a constant-time digest comparison for claim/cancel. Returned transfer views expose neither raw tokens nor their digests. The transfer ID is a lookup key, not authorization.

`Freeze` returns a new transfer reference whose generation has advanced. `Claim` and `Cancel` require that current reference. A destination must match the reserved instance exactly. The exclusive deadline means a transfer is expired when `now == ExpiresAt`. Expired claim/cancel operations reject without performing an implicit cleanup; call `Expire` to retire expired records. Its result is sorted by transfer ID for determinism.

Checkpoint version and digest are opaque caller-supplied evidence. Both must be nonzero. Versions cannot regress; a repeated version cannot carry a different digest. A digest does **not** establish that Minecraft saved the world or that a database committed. `Authorize` checks only current in-memory ownership; active sessions do not have an automatic lease timeout in this model.

All snapshots are returned by value. Copies of a `Machine` refer to the same private history, including its clock; there is no clone or reset API. Callers must serialize access; the machine has no mutex and makes no concurrent-use guarantee. The caller also owns authentication, authorization to initiate transitions, capacity reservations, durable commit, replay protection across restart, actual packet routing, and the recipient-side enforcement of fences. A successful in-memory transition must not be acknowledged as durable before the future storage transaction commits.

## Bounded histories and duplicate commands

Player records remain as generation tombstones even after release. Used transfer IDs remain as bounded tombstones after a pending record is claimed, canceled, or expired. Terminal token digests and full transfer records are removed. Reusing a used transfer ID rejects, including an exact repeated reservation after a pre-freeze cancellation. Duplicate claim/cancel commands reject; there is no success receipt cache.

Default limits are 1,024 player histories, 128 simultaneous pending transfers, 4,096 used transfer IDs, and a 30-second maximum transfer lifetime. Reaching a history limit rejects new history instead of evicting a generation and silently reusing authority. A deliberate reset requires a fresh authority epoch and corresponding external invalidation. These limits suit an offline laboratory slice; a durable production service needs its own reviewed history-retention and idempotency protocol.

## Integration and recovery boundary

A future transactional implementation must make the same compare-and-swap checks and persist the resulting state before reporting success. Every protected recipient must check the epoch, generation, and instance incarnation. A TTL or a controller's belief that an old worker died is insufficient. Chubby's published sequencer design places this validity check at the server receiving the protected operation: [Burrows, OSDI 2006, section 2.4](https://www.usenix.org/legacy/event/osdi06/tech/full_papers/burrows/burrows.pdf).

Database fencing does not fence Minecraft world files. Keep a persistent world assigned to one proven writer until the executor/storage layer can prevent an old process from writing after reassignment. The checkpoint object and `Authorize` method provide no such physical guarantee.

Run the focused tests with:

```bash
go test -race ./internal/network/session
```

The tests explore transfer ordering, failures around freeze/claim, deadline boundaries, wrong tokens and incarnations, clock regression, history bounds, generation overflow, immutable views, and randomized command sequences. They do not simulate successful Minecraft login or prove durable recovery.
