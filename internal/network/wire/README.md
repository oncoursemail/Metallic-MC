# Bounded Java packet framing

This package is original Metallic-MC groundwork for a future gateway. It reads
Java Edition packet frames using only the Go standard library. It does not open
a listener, authenticate a player, negotiate encryption, interpret packet IDs,
switch a backend, translate versions, or make the project a playable proxy.

## Bounds and ownership

`NewDecoder(reader, limits, budget)` requires explicit positive byte limits and a
shared `Budget`. Set `CompressionThreshold: -1` until the session protocol has
enabled compression. `Next()` returns a `Frame` whose payload includes the packet
ID but excludes framing headers. Release each frame when every consumer is done.
Neither a payload alias nor the frame may be retained for use after release.
Release is idempotent and concurrent calls to release are safe; concurrent use of
a payload while releasing it is a caller error. Serialize all decoder calls.

Construct budgets with `NewBudget`. Copying a `Budget` value preserves one shared
capacity, usage counter, lock, and notification stream. A reservation remains
bound to its original budget state even if a budget handle is later reassigned.
A nil or zero-value budget rejects `TryReserve` with `ErrReservation` and is
rejected by `NewDecoder`; its `Used` and `Limit` are zero and `Changed` is nil.

The outer length has at most three VarInt bytes and cannot exceed `2^21-1` or
`MaxFrameBytes`. A compression declaration has at most five bytes, cannot be
negative or overflow a positive signed 32-bit value, and cannot exceed
`MaxUncompressedBytes`. Non-minimal positive VarInt encodings within these bounds
are accepted. Empty packet payloads and zero-length outer frames are rejected;
this primitive does not treat zero frames as connection padding.

Headers are read first, without a body allocation. Compressed packets reserve
their encoded body length plus declared decoded length before reading that body.
Uncompressed packets reserve only their payload length. The compressed charge
remains conservatively held until `Frame.Release`, even though the temporary
encoded buffer is no longer returned. An input failure, malformed zlib stream,
size mismatch, or other body error releases the entire reservation.
Previously returned frames still belong to their consumers; a later decoder error
does not release those consumers' buffers.

The budget counts owned packet byte buffers. It is not a process RSS limit. Zlib
workspace, runtime metadata, stacks, caches, reader/socket buffering, and CPU work
are outside this charge. The session owner must enforce connection concurrency,
deadlines, inflate concurrency/work admission, and the containing cell's memory
limit. A wrapping buffered reader can prefetch bytes; that reader's buffer is also
outside this package's accounting.

## Backpressure

`ErrBudget` is temporary: the decoder retains its parsed header and has not read
the body. Retry the same decoder when capacity returns. `ErrBudgetCapacity` means
this one frame is larger than the budget's total capacity; waiting cannot fix it.
Every error other than `ErrBudget`, including an incomplete network read, makes
the decoder terminal. Dispose of that connection rather than interpreting its
remaining bytes as a new frame.

A reader can itself return or wrap `ErrBudget`. Such a failure is terminal and
is reported as `ErrReadFailure`, retaining the original diagnostic text without
unwrapping the reader's retry marker. This applies to joined errors too. As a
result, `errors.Is(err, ErrBudget)` identifies actual admission backpressure;
ordinary reader errors retain their identities for EOF, timeout, and cause checks.

Obtain a change notification **before** trying admission to avoid a lost wakeup:

```go
for {
    changed := budget.Changed()
    frame, err := decoder.Next()
    if errors.Is(err, wire.ErrBudget) {
        select {
        case <-changed:
            continue
        case <-ctx.Done():
            return ctx.Err()
        }
    }
    if err != nil {
        return err
    }
    // Process synchronously, or transfer ownership to one bounded consumer.
    err = consume(frame.Payload)
    frame.Release()
    if err != nil {
        return err
    }
}
```

The notification promises a change, not fair scheduling or a reservation.
`Next()` may block on its reader; the caller must set socket/read deadlines and
arrange cancellation. Context cancellation in the example only interrupts the
budget wait. Do not hold frames indefinitely while waiting for another frame
that needs the same budget. Downstream consumers must release independently, or
the owner must apply a deadline/overflow policy.

Compression can change through `SetCompressionThreshold` only after a completed
frame. It cannot change while a header is pending after `ErrBudget`. The higher
session state machine remains responsible for changing it at the correct packet.

## Compression policy and compatibility

With compression enabled, a zero declaration identifies an uncompressed payload
strictly smaller than the threshold. A nonzero declaration must meet or exceed
the threshold. At threshold zero all nonempty packets must be compressed. These
rules match the default checks in the inspected upstream
[Velocity decoder](https://github.com/PaperMC/Velocity/blob/5e8f92b98e6870c0118dadfdff42479d680654cc/proxy/src/main/java/com/velocitypowered/proxy/protocol/netty/MinecraftCompressDecoder.java).
That source offers a property to bypass one validation; Metallic-MC does not add
that bypass here.

Inflation reads exactly the declared payload plus at most one extra output byte
to detect overflow. It must reach checked zlib EOF with the exact size. A
checksum failure, truncated stream, dictionary requirement, trailing garbage, or
concatenated second zlib stream is rejected. The trailing-stream policy is an
intentional strict boundary, not a claim that every historical Minecraft proxy
enforces the same behavior. No interoperability matrix has yet been certified.
The standard decoder's internal bounded workspace still performs some work
beyond bytes returned to our output buffer; aggregate CPU admission is external.

The Go [compress/zlib](https://pkg.go.dev/compress/zlib) contract supports exact
stream-consumption checks when its input implements `io.ByteReader` and requires
consuming to EOF for checksum validation. This implementation uses `bytes.Reader`
for that reason and does not call unbounded `io.ReadAll`.

## Validation

Tests cover fragmented input, queued frames, compression boundary changes,
malformed headers, raw and compressed truncation, size mismatches, large expansion
with a small declaration, checksum and trailing-stream rejection, temporary
budget exhaustion and resumed parsing, permanent capacity rejection, and
concurrent reservations/releases through copied budget handles. Regressions also
cover uninitialized budgets, shared notification state, reservation ownership
after handle reassignment, and reader errors that carry a misleading retry marker.
The bounded fuzz target checks parser errors,
payload limits, and budget release with in-memory inputs. No network access is
needed.

```sh
go test -race ./internal/network/wire
go test ./internal/network/wire -run '^$' -fuzz FuzzDecoderBounded -fuzztime 10s
go test ./internal/network/wire -run '^$' -bench BenchmarkDecode -benchmem
```

The microbenchmark measures this framing primitive and its allocations in the
local runtime. It is not a player-capacity, full-gateway, authentication, crossplay,
or comparative Velocity benchmark.
