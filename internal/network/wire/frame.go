// Package wire implements bounded Java Edition packet framing. It does not
// implement packet semantics, authentication, encryption, or a playable proxy.
package wire

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"sync"
)

const (
	// MaxWireLength is the largest length representable by a three-byte VarInt.
	MaxWireLength     = 1<<21 - 1
	maxPositiveVarInt = 1<<31 - 1
)

var (
	ErrInvalidLimits  = errors.New("wire: invalid limits, reader, or budget")
	ErrInvalidVarInt  = errors.New("wire: invalid length VarInt")
	ErrEmptyFrame     = errors.New("wire: empty packet payload")
	ErrFrameTooLarge  = errors.New("wire: packet exceeds configured size limit")
	ErrThreshold      = errors.New("wire: packet violates compression threshold")
	ErrCompression    = errors.New("wire: invalid zlib stream")
	ErrSizeMismatch   = errors.New("wire: inflated size differs from declaration")
	ErrTrailingData   = errors.New("wire: trailing data after zlib stream")
	ErrPendingFrame   = errors.New("wire: cannot change compression with a pending frame")
	ErrBudget         = errors.New("wire: byte budget temporarily exhausted")
	ErrBudgetCapacity = errors.New("wire: frame exceeds total byte budget")
	ErrReservation    = errors.New("wire: invalid byte reservation")
	ErrReadFailure    = errors.New("wire: terminal reader failure")
)

// Limits applies to one direction of a connection. MaxFrameBytes bounds the
// outer frame body, including its compression header when present.
// MaxUncompressedBytes bounds the returned packet payload, including packet ID.
// CompressionThreshold is -1 when compression is disabled, or a negotiated
// nonnegative threshold. With compression enabled, an uncompressed envelope must
// be below the threshold and a compressed envelope must be at or above it.
type Limits struct {
	MaxFrameBytes        int
	MaxUncompressedBytes int
	CompressionThreshold int
}

func (l Limits) valid() bool {
	return l.MaxFrameBytes > 0 && l.MaxFrameBytes <= MaxWireLength &&
		l.MaxUncompressedBytes > 0 && l.MaxUncompressedBytes <= maxPositiveVarInt &&
		l.CompressionThreshold >= -1 && l.CompressionThreshold <= maxPositiveVarInt
}

// Budget is a concurrency-safe aggregate budget for owned packet byte buffers.
// It does not account for runtime, zlib workspace, socket buffers, or CPU work.
// Construct a Budget with NewBudget. Limits are immutable after construction.
// Value copies share one budget, including its accounting and notifications.
// A nil or zero-value Budget is uninitialized and cannot admit reservations.
type Budget struct {
	state *budgetState
}

type budgetState struct {
	mu      sync.Mutex
	limit   int64
	used    int64
	changed chan struct{}
}

func NewBudget(limit int64) (*Budget, error) {
	if limit <= 0 {
		return nil, ErrReservation
	}
	return &Budget{state: &budgetState{limit: limit, changed: make(chan struct{})}}, nil
}

// Reservation owns a byte charge until Release. Do not copy a Reservation.
type Reservation struct {
	budget *budgetState
	bytes  int64
	once   sync.Once
}

// TryReserve never waits. ErrBudget permits retry after other owners release
// buffers. ErrBudgetCapacity is permanent for this budget and requested size.
func (b *Budget) TryReserve(n int64) (*Reservation, error) {
	if b == nil || b.state == nil || n <= 0 {
		return nil, ErrReservation
	}
	s := b.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > s.limit {
		return nil, ErrBudgetCapacity
	}
	if n > s.limit-s.used {
		return nil, ErrBudget
	}
	s.used += n
	return &Reservation{budget: s, bytes: n}, nil
}

// Changed returns a notification closed on the next release. Obtain it BEFORE
// attempting an operation which can return ErrBudget to avoid a missed wakeup.
// A notification is not a reservation or a fairness guarantee: retry admission.
// An uninitialized Budget returns a nil notification channel.
func (b *Budget) Changed() <-chan struct{} {
	if b == nil || b.state == nil {
		return nil
	}
	s := b.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changed
}

// Used returns the charged bytes, or zero for an uninitialized Budget.
func (b *Budget) Used() int64 {
	if b == nil || b.state == nil {
		return 0
	}
	s := b.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.used
}

// Limit returns the immutable capacity, or zero for an uninitialized Budget.
func (b *Budget) Limit() int64 {
	if b == nil || b.state == nil {
		return 0
	}
	return b.state.limit
}

func (r *Reservation) Bytes() int64 {
	if r == nil {
		return 0
	}
	return r.bytes
}

// Release is nil-safe, idempotent, and safe for concurrent calls. All users of
// the charged buffers must have finished before the first call to Release.
func (r *Reservation) Release() {
	if r == nil || r.budget == nil {
		return
	}
	r.once.Do(func() {
		b := r.budget
		b.mu.Lock()
		defer b.mu.Unlock()
		b.used -= r.bytes
		if b.changed != nil {
			close(b.changed)
		}
		b.changed = make(chan struct{})
	})
}

// Frame owns Payload and its reservation. Payload includes the packet ID but
// excludes length/compression headers. Neither Frame nor Payload may be copied
// for use after Release. Release does not revoke previously created Go aliases.
type Frame struct {
	Payload     []byte
	reservation *Reservation
}

func (f *Frame) Release() {
	if f != nil {
		f.reservation.Release()
	}
}

// ReservedBytes includes a conservative temporary encoded+decoded charge for a
// compressed packet. The full charge remains until Frame.Release.
func (f *Frame) ReservedBytes() int64 {
	if f == nil {
		return 0
	}
	return f.reservation.Bytes()
}

type pendingFrame struct {
	bodyBytes   int
	inflated    int // zero means an uncompressed body
	reservation int64
}

// Decoder reads exactly one frame at a time without buffering later frames.
// Calls to Next and SetCompressionThreshold must be serialized by the caller.
// The caller owns the reader, cancellation, socket deadlines, and CPU admission.
type Decoder struct {
	r        io.Reader
	limits   Limits
	budget   *Budget
	pending  *pendingFrame
	terminal error
}

func NewDecoder(r io.Reader, limits Limits, budget *Budget) (*Decoder, error) {
	if r == nil || !limits.valid() || budget.Limit() <= 0 {
		return nil, ErrInvalidLimits
	}
	return &Decoder{r: r, limits: limits, budget: budget}, nil
}

// SetCompressionThreshold changes framing at a completed packet boundary. A
// pending header from ErrBudget must be completed or the connection discarded.
func (d *Decoder) SetCompressionThreshold(threshold int) error {
	if d.terminal != nil {
		return d.terminal
	}
	if d.pending != nil {
		return ErrPendingFrame
	}
	if threshold < -1 || threshold > maxPositiveVarInt {
		return ErrInvalidLimits
	}
	d.limits.CompressionThreshold = threshold
	return nil
}

// Next returns an owned payload. ErrBudget is the only retryable error: the
// parsed header is retained and no packet body has been read or allocated.
// Every other error, including EOF, makes this Decoder terminal. In particular,
// a partial network read must not be resumed as if it started a fresh frame.
// A reader error matching ErrBudget becomes ErrReadFailure with its diagnostic
// text preserved, so it cannot be mistaken for retryable admission failure.
func (d *Decoder) Next() (*Frame, error) {
	if d.terminal != nil {
		return nil, d.terminal
	}
	if d.pending == nil {
		p, err := d.readHeader()
		if err != nil {
			d.terminal = err
			return nil, err
		}
		d.pending = p
	}
	reservation, err := d.budget.TryReserve(d.pending.reservation)
	if err != nil {
		if !errors.Is(err, ErrBudget) {
			d.terminal = err
		}
		return nil, err
	}
	frame, err := d.readBody(reservation)
	if err != nil {
		d.terminal = err
		return nil, err
	}
	d.pending = nil
	return frame, nil
}

func (d *Decoder) readHeader() (*pendingFrame, error) {
	size, _, err := readLength(d.r, 3, true)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, ErrEmptyFrame
	}
	if size > d.limits.MaxFrameBytes {
		return nil, ErrFrameTooLarge
	}
	p := &pendingFrame{bodyBytes: size}
	if d.limits.CompressionThreshold >= 0 {
		// A malformed compression header must not read into the next frame.
		header := &io.LimitedReader{R: d.r, N: int64(size)}
		declared, consumed, err := readLength(header, 5, false)
		if err != nil {
			return nil, err
		}
		p.bodyBytes -= consumed
		p.inflated = declared
		if declared > d.limits.MaxUncompressedBytes {
			return nil, ErrFrameTooLarge
		}
		if declared == 0 {
			if p.bodyBytes >= d.limits.CompressionThreshold {
				return nil, ErrThreshold
			}
		} else if declared < d.limits.CompressionThreshold {
			return nil, ErrThreshold
		}
	}
	if p.bodyBytes <= 0 {
		return nil, ErrEmptyFrame
	}
	if p.inflated == 0 && p.bodyBytes > d.limits.MaxUncompressedBytes {
		return nil, ErrFrameTooLarge
	}
	// Convert before addition so this is also safe on 32-bit architectures.
	p.reservation = int64(p.bodyBytes) + int64(p.inflated)
	return p, nil
}

func (d *Decoder) readBody(reservation *Reservation) (_ *Frame, err error) {
	transferred := false
	defer func() {
		if !transferred {
			reservation.Release()
		}
	}()
	encoded := make([]byte, d.pending.bodyBytes)
	if _, err := io.ReadFull(d.r, encoded); err != nil {
		err = classifyReadError(err)
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("wire: packet body: %w", err)
	}
	payload := encoded
	if d.pending.inflated != 0 {
		payload, err = inflateExact(encoded, d.pending.inflated)
		if err != nil {
			return nil, err
		}
	}
	transferred = true
	return &Frame{Payload: payload, reservation: reservation}, nil
}

// Only the admission path may return an error matching ErrBudget. Preserve a
// colliding reader error's text without unwrapping its retry-only marker. Other
// reader errors retain their identities for EOF, timeout, and cause checks.
func classifyReadError(err error) error {
	if errors.Is(err, ErrBudget) {
		return fmt.Errorf("%w: %v", ErrReadFailure, err)
	}
	return err
}

// readLength accepts non-minimal positive encodings within the byte bound.
// Minecraft lengths must not be negative; fifth-byte sign/overflow bits fail.
func readLength(r io.Reader, maxBytes int, boundaryEOF bool) (int, int, error) {
	var value uint32
	var one [1]byte
	for i := 0; i < maxBytes; i++ {
		if _, err := io.ReadFull(r, one[:]); err != nil {
			err = classifyReadError(err)
			if errors.Is(err, io.EOF) && (i != 0 || !boundaryEOF) {
				err = io.ErrUnexpectedEOF
			}
			return 0, i, err
		}
		b := one[0]
		if i == 4 && b > 7 {
			return 0, i + 1, ErrInvalidVarInt
		}
		value |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int(value), i + 1, nil
		}
	}
	return 0, maxBytes, ErrInvalidVarInt
}

func inflateExact(encoded []byte, size int) ([]byte, error) {
	// bytes.Reader implements io.ByteReader: zlib does not consume bytes past
	// its single stream, allowing exact trailing-data validation below.
	source := bytes.NewReader(encoded)
	zr, err := zlib.NewReader(source)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCompression, err)
	}
	defer zr.Close()
	payload := make([]byte, size)
	if _, err := io.ReadFull(zr, payload); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("%w: %w", ErrSizeMismatch, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrCompression, err)
	}
	// Do not use unbounded ReadAll. A single extra byte detects excess output;
	// reaching EOF also forces verification of the zlib checksum.
	var extra [1]byte
	if n, err := io.ReadFull(zr, extra[:]); n != 0 {
		return nil, ErrSizeMismatch
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: %w", ErrCompression, err)
	}
	if source.Len() != 0 {
		return nil, ErrTrailingData
	}
	return payload, nil
}
