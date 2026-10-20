package wire

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func lengthBytes(n uint32) []byte {
	var out []byte
	for n >= 128 {
		out = append(out, byte(n)|0x80)
		n >>= 7
	}
	return append(out, byte(n))
}

func plainFrame(payload []byte) []byte {
	return append(lengthBytes(uint32(len(payload))), payload...)
}

func envelope(body []byte, declared uint32) []byte {
	header := lengthBytes(declared)
	result := lengthBytes(uint32(len(header) + len(body)))
	result = append(result, header...)
	return append(result, body...)
}

func zipped(payload []byte) []byte {
	var out bytes.Buffer
	z := zlib.NewWriter(&out)
	if _, err := z.Write(payload); err != nil {
		panic(err)
	}
	if err := z.Close(); err != nil {
		panic(err)
	}
	return out.Bytes()
}

func mustBudget(t testing.TB, n int64) *Budget {
	t.Helper()
	b, err := NewBudget(n)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustDecoder(t testing.TB, r io.Reader, limits Limits, budget *Budget) *Decoder {
	t.Helper()
	d, err := NewDecoder(r, limits, budget)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func testLimits(threshold int) Limits {
	return Limits{MaxFrameBytes: 8192, MaxUncompressedBytes: 8192, CompressionThreshold: threshold}
}

type fragmentedReader struct {
	r   io.Reader
	max int
}

func (r fragmentedReader) Read(p []byte) (int, error) {
	if len(p) > r.max {
		p = p[:r.max]
	}
	return r.r.Read(p)
}

func TestLimitsAndReservations(t *testing.T) {
	for _, n := range []int64{0, -1} {
		if _, err := NewBudget(n); !errors.Is(err, ErrReservation) {
			t.Fatalf("budget %d: %v", n, err)
		}
	}
	budget := mustBudget(t, 8192)
	invalid := []Limits{
		{},
		{MaxFrameBytes: MaxWireLength + 1, MaxUncompressedBytes: 1, CompressionThreshold: -1},
		{MaxFrameBytes: 1, MaxUncompressedBytes: 0, CompressionThreshold: -1},
		{MaxFrameBytes: 1, MaxUncompressedBytes: 1, CompressionThreshold: -2},
	}
	for _, limits := range invalid {
		if _, err := NewDecoder(bytes.NewReader(nil), limits, budget); !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("limits %+v: %v", limits, err)
		}
	}
	if _, err := NewDecoder(nil, testLimits(-1), budget); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("nil reader: %v", err)
	}
	if _, err := NewDecoder(bytes.NewReader(nil), testLimits(-1), nil); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("nil budget: %v", err)
	}
	for _, n := range []int64{0, -1} {
		if _, err := budget.TryReserve(n); !errors.Is(err, ErrReservation) {
			t.Fatalf("reservation %d: %v", n, err)
		}
	}
	if budget.Limit() != 8192 || budget.Used() != 0 {
		t.Fatal("invalid initial budget")
	}
	var frame *Frame
	frame.Release()
	if frame.ReservedBytes() != 0 {
		t.Fatal("nil frame charge")
	}
}

func TestUninitializedBudgetRejectsAdmission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		budget *Budget
	}{
		{"nil", nil},
		{"zero", &Budget{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if r, err := tc.budget.TryReserve(1); r != nil || !errors.Is(err, ErrReservation) {
				t.Fatalf("uninitialized admission: reservation=%v error=%v", r, err)
			}
			if d, err := NewDecoder(bytes.NewReader(nil), testLimits(-1), tc.budget); d != nil || !errors.Is(err, ErrInvalidLimits) {
				t.Fatalf("uninitialized decoder: decoder=%v error=%v", d, err)
			}
			if tc.budget.Used() != 0 || tc.budget.Limit() != 0 || tc.budget.Changed() != nil {
				t.Fatal("uninitialized budget reported capacity, usage, or a notification")
			}
		})
	}
}

func TestBudgetCopiesShareAccountingAndNotifications(t *testing.T) {
	budget := mustBudget(t, 10)
	before := *budget
	a, err := budget.TryReserve(6)
	if err != nil {
		t.Fatal(err)
	}
	after := *budget
	aliases := []*Budget{budget, &before, &after}
	changed := budget.Changed()
	for _, alias := range aliases {
		if alias.Limit() != 10 || alias.Used() != 6 || alias.Changed() != changed {
			t.Fatal("budget copy split capacity, accounting, or notifications")
		}
		if r, err := alias.TryReserve(5); r != nil || !errors.Is(err, ErrBudget) {
			t.Fatalf("copy escaped shared capacity: reservation=%v error=%v", r, err)
		}
	}
	b, err := before.TryReserve(4)
	if err != nil {
		t.Fatal(err)
	}
	a.Release()
	select {
	case <-changed:
	default:
		t.Fatal("release through original did not notify its copies")
	}
	nextChanged := after.Changed()
	if nextChanged == changed {
		t.Fatal("release did not advance the shared notification")
	}
	for _, alias := range aliases {
		if alias.Used() != 4 || alias.Changed() != nextChanged {
			t.Fatal("copies disagree after release")
		}
	}
	c, err := after.TryReserve(6)
	if err != nil {
		t.Fatal(err)
	}
	b.Release()
	c.Release()
	for _, alias := range aliases {
		if alias.Used() != 0 {
			t.Fatal("release through a copied budget leaked its charge")
		}
	}
}

func TestReservationRemainsBoundToOriginalStateAfterHandleAssignment(t *testing.T) {
	budget := mustBudget(t, 10)
	original := *budget
	r, err := budget.TryReserve(6)
	if err != nil {
		t.Fatal(err)
	}
	replacement := mustBudget(t, 4)
	other, err := replacement.TryReserve(2)
	if err != nil {
		t.Fatal(err)
	}
	*budget = *replacement
	r.Release()
	if original.Used() != 0 || budget.Used() != 2 || replacement.Used() != 2 {
		t.Fatal("reassigning a budget handle redirected an existing reservation")
	}
	other.Release()
}

func TestFragmentedQueuedFrames(t *testing.T) {
	want := [][]byte{{0, 1, 2}, bytes.Repeat([]byte{7}, 300), {1}}
	for _, compressed := range []bool{false, true} {
		name, threshold := "plain", -1
		if compressed {
			name, threshold = "compressed", 16
		}
		t.Run(name, func(t *testing.T) {
			var stream []byte
			for _, payload := range want {
				if !compressed {
					stream = append(stream, plainFrame(payload)...)
				} else if len(payload) < threshold {
					stream = append(stream, envelope(payload, 0)...)
				} else {
					stream = append(stream, envelope(zipped(payload), uint32(len(payload)))...)
				}
			}
			budget := mustBudget(t, 16384)
			d := mustDecoder(t, fragmentedReader{bytes.NewReader(stream), 1}, testLimits(threshold), budget)
			for _, payload := range want {
				f, err := d.Next()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(f.Payload, payload) || f.ReservedBytes() < int64(len(payload)) {
					t.Fatal("payload or charge mismatch")
				}
				f.Release()
				f.Release()
				if budget.Used() != 0 {
					t.Fatal("frame charge leaked")
				}
			}
			if _, err := d.Next(); !errors.Is(err, io.EOF) {
				t.Fatalf("end of stream: %v", err)
			}
		})
	}
}

func TestCompressionTransitionAtCompletedBoundary(t *testing.T) {
	payload := bytes.Repeat([]byte{3}, 64)
	stream := append(plainFrame([]byte{1, 2}), envelope(zipped(payload), uint32(len(payload)))...)
	budget := mustBudget(t, 16384)
	d := mustDecoder(t, bytes.NewReader(stream), testLimits(-1), budget)
	f, err := d.Next()
	if err != nil {
		t.Fatal(err)
	}
	f.Release()
	if err := d.SetCompressionThreshold(32); err != nil {
		t.Fatal(err)
	}
	f, err = d.Next()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.Payload, payload) {
		t.Fatal("wrong payload after compression transition")
	}
	f.Release()
	if err := d.SetCompressionThreshold(-2); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("invalid threshold: %v", err)
	}
}

func TestThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		size       int
		threshold  int
		compressed bool
		want       error
	}{
		{"raw below", 31, 32, false, nil},
		{"raw equal", 32, 32, false, ErrThreshold},
		{"raw above", 33, 32, false, ErrThreshold},
		{"compressed below", 31, 32, true, ErrThreshold},
		{"compressed equal", 32, 32, true, nil},
		{"compressed above", 33, 32, true, nil},
		{"zero threshold raw", 1, 0, false, ErrThreshold},
		{"zero threshold compressed", 1, 0, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := bytes.Repeat([]byte{1}, tc.size)
			packet := envelope(payload, 0)
			if tc.compressed {
				packet = envelope(zipped(payload), uint32(tc.size))
			}
			budget := mustBudget(t, 16384)
			d := mustDecoder(t, bytes.NewReader(packet), testLimits(tc.threshold), budget)
			f, err := d.Next()
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if f != nil {
				f.Release()
			}
			if budget.Used() != 0 {
				t.Fatal("charge leaked")
			}
		})
	}
}

func TestMalformedLengthsAndCaps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		limits Limits
		want   error
	}{
		{"zero frame", []byte{0}, testLimits(-1), ErrEmptyFrame},
		{"partial outer", []byte{0x80}, testLimits(-1), io.ErrUnexpectedEOF},
		{"outer too long", []byte{0x80, 0x80, 0x80, 0, 1}, testLimits(-1), ErrInvalidVarInt},
		{"negative declaration", envelope([]byte{1}, ^uint32(0)), testLimits(0), ErrInvalidVarInt},
		{"declaration overflow", []byte{6, 0x80, 0x80, 0x80, 0x80, 0x10, 1}, testLimits(0), ErrInvalidVarInt},
		{"declaration too long", []byte{7, 0x80, 0x80, 0x80, 0x80, 0x80, 0, 1}, testLimits(0), ErrInvalidVarInt},
		{"empty compression body", []byte{1, 1}, testLimits(0), ErrEmptyFrame},
		{"oversized outer", []byte{11}, Limits{10, 10, -1}, ErrFrameTooLarge},
		{"oversized raw payload", plainFrame(make([]byte, 11)), Limits{20, 10, -1}, ErrFrameTooLarge},
		{"oversized declared", envelope([]byte{1}, 11), Limits{20, 10, 0}, ErrFrameTooLarge},
		{"oversized uncompressed envelope", envelope(make([]byte, 11), 0), Limits{20, 10, 12}, ErrFrameTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget := mustBudget(t, 16384)
			r := bytes.NewReader(tc.data)
			d := mustDecoder(t, r, tc.limits, budget)
			if _, err := d.Next(); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			remaining := r.Len()
			if _, err := d.Next(); !errors.Is(err, tc.want) || r.Len() != remaining {
				t.Fatal("terminal decoder consumed more bytes or lost original error")
			}
			if budget.Used() != 0 {
				t.Fatal("error leaked charge")
			}
		})
	}
}

func TestCompressionHeaderCannotConsumeNextFrame(t *testing.T) {
	next := plainFrame([]byte{3, 4})
	r := bytes.NewReader(append([]byte{1, 0x80}, next...))
	d := mustDecoder(t, r, testLimits(0), mustBudget(t, 16384))
	if _, err := d.Next(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("incomplete compressed header: %v", err)
	}
	if r.Len() != len(next) {
		t.Fatal("compression header consumed the following frame")
	}
}

func TestNonMinimalPositiveVarInts(t *testing.T) {
	// Two-byte encoding of outer length 3, then two-byte zero declaration.
	d := mustDecoder(t, bytes.NewReader([]byte{0x83, 0, 0x80, 0, 7}), testLimits(8), mustBudget(t, 128))
	f, err := d.Next()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	if !bytes.Equal(f.Payload, []byte{7}) {
		t.Fatal("non-minimal positive length was not decoded")
	}
}

func TestBodyFailuresReleaseReservations(t *testing.T) {
	payload := bytes.Repeat([]byte{9}, 128)
	compressed := zipped(payload)
	corrupt := append([]byte(nil), compressed...)
	corrupt[len(corrupt)-1] ^= 1
	for _, tc := range []struct {
		name      string
		data      []byte
		threshold int
		want      error
	}{
		{"truncated raw", []byte{5, 1, 2}, -1, io.ErrUnexpectedEOF},
		{"missing raw body", []byte{5}, -1, io.ErrUnexpectedEOF},
		{"bad zlib header", envelope([]byte{1, 2, 3}, 128), 0, ErrCompression},
		{"truncated zlib", envelope(compressed[:len(compressed)-1], 128), 0, ErrCompression},
		{"bad checksum", envelope(corrupt, 128), 0, zlib.ErrChecksum},
		{"too little output", envelope(compressed, 129), 0, ErrSizeMismatch},
		{"too much output", envelope(compressed, 127), 0, ErrSizeMismatch},
		{"trailing bytes", envelope(append(append([]byte(nil), compressed...), 0), 128), 0, ErrTrailingData},
		{"second zlib stream", envelope(append(append([]byte(nil), compressed...), compressed...), 128), 0, ErrTrailingData},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget := mustBudget(t, 16384)
			d := mustDecoder(t, bytes.NewReader(tc.data), testLimits(tc.threshold), budget)
			if f, err := d.Next(); f != nil || !errors.Is(err, tc.want) {
				t.Fatalf("frame %v, error %v; want %v", f != nil, err, tc.want)
			}
			if budget.Used() != 0 {
				t.Fatalf("error leaked %d bytes", budget.Used())
			}
		})
	}
}

type failingReader struct {
	err   error
	calls int
}

func (r *failingReader) Read(_ []byte) (int, error) {
	r.calls++
	return 0, r.err
}

func TestReaderErrorsCannotMasqueradeAsBudgetWait(t *testing.T) {
	faults := []struct {
		name string
		err  error
	}{
		{"direct", ErrBudget},
		{"wrapped", fmt.Errorf("reader wrapper: %w", ErrBudget)},
		{"joined", errors.Join(io.ErrClosedPipe, ErrBudget)},
		{"nested", fmt.Errorf("outer reader: %w", errors.Join(io.ErrClosedPipe, fmt.Errorf("inner reader: %w", ErrBudget)))},
		{"joined EOF", errors.Join(io.EOF, ErrBudget)},
		{"ordinary error", io.ErrClosedPipe},
	}
	for _, stage := range []struct {
		name      string
		prefix    []byte
		threshold int
	}{
		{"outer header", nil, -1},
		{"partial outer header", []byte{0x80}, -1},
		{"partial compression header", []byte{3, 0x80}, 0},
		{"partial raw body", []byte{5, 1, 2}, -1},
		{"partial compressed body", []byte{7, 4, 1, 2}, 0},
	} {
		for _, fault := range faults {
			t.Run(stage.name+"/"+fault.name, func(t *testing.T) {
				reader := &failingReader{err: fault.err}
				budget := mustBudget(t, 1024)
				d := mustDecoder(t, io.MultiReader(bytes.NewReader(stage.prefix), reader), testLimits(stage.threshold), budget)
				frame, err := d.Next()
				if frame != nil || err == nil || errors.Is(err, ErrBudget) {
					t.Fatalf("reader failure looked retryable: frame=%v error=%v", frame, err)
				}
				if errors.Is(fault.err, ErrBudget) {
					if !errors.Is(err, ErrReadFailure) {
						t.Fatalf("colliding reader error lost terminal classification: %v", err)
					}
				} else if !errors.Is(err, fault.err) {
					t.Fatalf("ordinary reader error lost identity: %v", err)
				}
				if !strings.Contains(err.Error(), fault.err.Error()) {
					t.Fatalf("reader diagnostic was lost: %q", err)
				}
				if budget.Used() != 0 {
					t.Fatal("reader failure leaked a reservation")
				}
				calls := reader.calls
				if frame, repeated := d.Next(); frame != nil || repeated != err || reader.calls != calls {
					t.Fatal("terminal decoder changed its error or resumed reading")
				}
				if repeated := d.SetCompressionThreshold(32); repeated != err || reader.calls != calls {
					t.Fatal("terminal decoder permitted a compression change")
				}
			})
		}
	}
}

func TestTerminalReaderFailurePreservesEarlierFrameOwnership(t *testing.T) {
	budget := mustBudget(t, 1024)
	reader := &failingReader{err: fmt.Errorf("later reader: %w", ErrBudget)}
	d := mustDecoder(t, io.MultiReader(bytes.NewReader(plainFrame([]byte{1, 2, 3})), reader), testLimits(-1), budget)
	frame, err := d.Next()
	if err != nil {
		t.Fatal(err)
	}
	defer frame.Release()
	if _, err := d.Next(); !errors.Is(err, ErrReadFailure) || errors.Is(err, ErrBudget) {
		t.Fatalf("later read failure: %v", err)
	}
	if budget.Used() != frame.ReservedBytes() || !bytes.Equal(frame.Payload, []byte{1, 2, 3}) {
		t.Fatal("terminal decoder released or changed an earlier consumer-owned frame")
	}
	frame.Release()
	if budget.Used() != 0 {
		t.Fatal("earlier frame leaked its reservation")
	}
}

func TestLargeExpansionStopsAtDeclaration(t *testing.T) {
	// The encoded body fits; the actual output greatly exceeds the declaration.
	// The decoder reads the declared byte plus one, never an unbounded ReadAll.
	compressed := zipped(bytes.Repeat([]byte{0}, 2<<20))
	budget := mustBudget(t, int64(len(compressed)+1))
	d := mustDecoder(t, bytes.NewReader(envelope(compressed, 1)), testLimits(0), budget)
	if _, err := d.Next(); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("large expansion: %v", err)
	}
	if budget.Used() != 0 {
		t.Fatal("large expansion leaked reservation")
	}
}

func TestBudgetExhaustionRetainsHeaderAndResumes(t *testing.T) {
	first := bytes.Repeat([]byte{1}, 64)
	second := bytes.Repeat([]byte{2}, 64)
	r := bytes.NewReader(append(plainFrame(first), plainFrame(second)...))
	budget := mustBudget(t, 100)
	d := mustDecoder(t, r, testLimits(-1), budget)
	a, err := d.Next()
	if err != nil {
		t.Fatal(err)
	}
	changed := budget.Changed()
	if f, err := d.Next(); f != nil || !errors.Is(err, ErrBudget) {
		t.Fatalf("second frame should pause: %v", err)
	}
	if r.Len() != len(second) || budget.Used() != 64 {
		t.Fatal("paused decoder read/allocated the body")
	}
	if err := d.SetCompressionThreshold(32); !errors.Is(err, ErrPendingFrame) {
		t.Fatalf("compression changed during pending frame: %v", err)
	}
	if _, err := d.Next(); !errors.Is(err, ErrBudget) || r.Len() != len(second) {
		t.Fatal("retry consumed another header")
	}
	a.Release()
	select {
	case <-changed:
	default:
		t.Fatal("release did not notify waiter")
	}
	b, err := d.Next()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Payload, second) || r.Len() != 0 {
		t.Fatal("resumed decoder lost frame alignment")
	}
	b.Release()
	if budget.Used() != 0 {
		t.Fatal("resumed frame leaked charge")
	}
}

func TestCompressedReservationCoversWireAndOutputBeforeBody(t *testing.T) {
	payload := bytes.Repeat([]byte{6}, 128)
	compressed := zipped(payload)
	charge := int64(len(compressed) + len(payload))
	for _, capacity := range []int64{charge - 1, charge} {
		budget := mustBudget(t, capacity)
		r := bytes.NewReader(envelope(compressed, uint32(len(payload))))
		d := mustDecoder(t, r, testLimits(0), budget)
		f, err := d.Next()
		if capacity < charge {
			if f != nil || !errors.Is(err, ErrBudgetCapacity) || errors.Is(err, ErrBudget) {
				t.Fatalf("permanent capacity rejection: %v", err)
			}
			if r.Len() != len(compressed) || budget.Used() != 0 {
				t.Fatal("capacity rejection consumed or allocated body")
			}
			if _, err := d.Next(); !errors.Is(err, ErrBudgetCapacity) || r.Len() != len(compressed) {
				t.Fatal("permanent capacity rejection was retried or consumed body")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if budget.Used() != charge || f.ReservedBytes() != charge {
			t.Fatal("encoded+decoded peak was not reserved")
		}
		f.Release()
	}
}

func TestConcurrentBudgetAndIdempotentRelease(t *testing.T) {
	budget := mustBudget(t, 31)
	copy := *budget
	aliases := []*Budget{budget, &copy}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(n int64, owner *Budget) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				for {
					changed := owner.Changed()
					r, err := owner.TryReserve(n)
					if errors.Is(err, ErrBudget) {
						select {
						case <-changed:
							continue
						case <-ctx.Done():
							t.Error("budget wait timed out")
							return
						}
					}
					if err != nil {
						t.Error(err)
						return
					}
					if used := budget.Used(); used < 0 || used > budget.Limit() {
						t.Errorf("invalid concurrent charge %d", used)
					}
					r.Release()
					r.Release()
					break
				}
			}
		}(int64(g%8+1), aliases[g%len(aliases)])
	}
	wg.Wait()
	if budget.Used() != 0 {
		t.Fatal("concurrent reservations leaked")
	}
	r, err := budget.TryReserve(31)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.Release() }()
	}
	wg.Wait()
	if budget.Used() != 0 {
		t.Fatal("concurrent release was not idempotent")
	}
}

func FuzzDecoderBounded(f *testing.F) {
	f.Add(plainFrame([]byte{0, 1}), byte(0))
	f.Add(envelope(zipped(bytes.Repeat([]byte{1}, 128)), 128), byte(1))
	f.Add([]byte{0x80, 0x80, 0x80}, byte(0))
	f.Add([]byte{1, 0x80, 1, 2}, byte(1))
	f.Fuzz(func(t *testing.T, data []byte, mode byte) {
		if len(data) > 8192 {
			t.Skip()
		}
		threshold := int(mode) - 1
		budget := mustBudget(t, 16384)
		d := mustDecoder(t, bytes.NewReader(data), Limits{4096, 8192, threshold}, budget)
		for i := 0; i < 32; i++ {
			frame, err := d.Next()
			if err != nil {
				if errors.Is(err, ErrBudget) {
					t.Fatal("temporary exhaustion with no retained frames")
				}
				break
			}
			if len(frame.Payload) == 0 || len(frame.Payload) > 8192 || budget.Used() > budget.Limit() {
				frame.Release()
				t.Fatal("decoded frame escaped limits")
			}
			frame.Release()
		}
		if budget.Used() != 0 {
			t.Fatal("fuzzed input leaked a byte charge")
		}
	})
}

func BenchmarkDecode(b *testing.B) {
	payload := bytes.Repeat([]byte("Metallic-MC packet framing "), 160)
	for _, tc := range []struct {
		name      string
		threshold int
		packet    []byte
	}{
		{"plain", -1, plainFrame(payload)},
		{"zlib", 256, envelope(zipped(payload), uint32(len(payload)))},
	} {
		b.Run(tc.name, func(b *testing.B) {
			budget := mustBudget(b, 32768)
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				d := mustDecoder(b, bytes.NewReader(tc.packet), testLimits(tc.threshold), budget)
				frame, err := d.Next()
				if err != nil {
					b.Fatal(err)
				}
				frame.Release()
			}
		})
	}
}
