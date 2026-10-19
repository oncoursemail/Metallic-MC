package benchgate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"unicode/utf8"
)

const (
	TraceSchemaVersion = "metallic.benchmark-trace/v1"
	MaxTraceLineBytes  = 4096
	MaxTraceSamples    = 200000
	TickDeadlineNS     = uint64(50_000_000)
	maxSampleNS        = uint64(60_000_000_000)
	maxWindowNS        = uint64(86400) * 1_000_000_000
)

// TraceSummary contains calculations over imported records. Neither the header
// nor successful parsing authenticates the recorder or its claimed environment.
type TraceSummary struct {
	SchemaVersion     string       `json:"schemaVersion"`
	Kind              string       `json:"kind"`
	EvidenceKind      string       `json:"evidenceKind"`
	RunID             string       `json:"runId"`
	RepetitionID      string       `json:"repetitionId"`
	DurationNS        uint64       `json:"durationNs"`
	SampleCount       uint64       `json:"sampleCount"`
	P99NS             *uint64      `json:"p99Ns,omitempty"`
	DeadlineMissCount *uint64      `json:"deadlineMissCount,omitempty"`
	Correctness       *Correctness `json:"correctness,omitempty"`
}

type traceHeader struct {
	Type          string  `json:"type"`
	SchemaVersion string  `json:"schemaVersion"`
	Kind          string  `json:"kind"`
	EvidenceKind  string  `json:"evidenceKind"`
	RunID         string  `json:"runId"`
	RepetitionID  string  `json:"repetitionId"`
	DurationNS    *uint64 `json:"durationNs"`
}

type traceSample struct {
	Type       string  `json:"type"`
	Sequence   *uint64 `json:"sequence"`
	ElapsedNS  *uint64 `json:"elapsedNs"`
	DurationNS *uint64 `json:"durationNs"`
}

type traceCheck struct {
	Type      string  `json:"type"`
	Sequence  *uint64 `json:"sequence"`
	ElapsedNS *uint64 `json:"elapsedNs"`
	Name      string  `json:"name"`
	Passed    *bool   `json:"passed"`
}

type traceComplete struct {
	Type              string  `json:"type"`
	SampleCount       *uint64 `json:"sampleCount"`
	DroppedSamples    *uint64 `json:"droppedSamples"`
	IncompleteSamples *uint64 `json:"incompleteSamples"`
	DurationNS        *uint64 `json:"durationNs"`
}

// ReadTrace reads the entire bounded JSONL stream, requiring a header, ordered
// records and a completion footer. A missing footer, missing final newline or
// recorded sample loss fails closed. Only durations are retained for exact p99;
// correctness records are independent, explicit assertions from the recorder.
func ReadTrace(reader io.Reader) (TraceSummary, error) {
	var summary TraceSummary
	var durations []uint64
	var misses, previousElapsed uint64
	var correctness Correctness
	seenChecks := map[string]bool{}
	complete := false
	input := bufio.NewReaderSize(io.LimitReader(reader, MaxTraceBytes+1), MaxTraceLineBytes+1)
	var totalBytes int64
	lineNumber := 0
	for {
		line, err := input.ReadSlice('\n')
		totalBytes += int64(len(line))
		if totalBytes > MaxTraceBytes {
			return TraceSummary{}, errors.New("trace exceeds 16 MiB limit")
		}
		if len(line) > MaxTraceLineBytes || errors.Is(err, bufio.ErrBufferFull) {
			return TraceSummary{}, fmt.Errorf("trace line %d exceeds 4096 byte limit", lineNumber+1)
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) == 0 {
				break
			}
			if errors.Is(err, io.EOF) {
				return TraceSummary{}, errors.New("truncated trace: every record must end with a newline")
			}
			return TraceSummary{}, fmt.Errorf("read trace: %w", err)
		}
		lineNumber++
		if complete {
			return TraceSummary{}, errors.New("unexpected record after completion footer")
		}
		line = bytes.TrimSuffix(line, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(bytes.TrimSpace(line)) == 0 || !utf8.Valid(line) {
			return TraceSummary{}, fmt.Errorf("trace line %d must be nonempty UTF-8 JSON", lineNumber)
		}
		if lineNumber == 1 {
			var header traceHeader
			if err := decodeTraceLine(line, &header); err != nil {
				return TraceSummary{}, fmt.Errorf("invalid trace header: %w", err)
			}
			if header.Type != "header" || header.SchemaVersion != TraceSchemaVersion ||
				(header.Kind != "ticks" && header.Kind != "interactions" && header.Kind != "correctness") ||
				(header.EvidenceKind != "measured" && header.EvidenceKind != "synthetic") ||
				!idPattern.MatchString(header.RunID) || !idPattern.MatchString(header.RepetitionID) ||
				header.DurationNS == nil || *header.DurationNS == 0 || *header.DurationNS > maxWindowNS {
				return TraceSummary{}, errors.New("missing or invalid trace header fields")
			}
			summary = TraceSummary{SchemaVersion: header.SchemaVersion, Kind: header.Kind,
				EvidenceKind: header.EvidenceKind, RunID: header.RunID, RepetitionID: header.RepetitionID,
				DurationNS: *header.DurationNS}
			continue
		}
		// Dispatch only; the selected typed decoder below rejects duplicates,
		// unknown/case-mismatched keys and scalar values inappropriate to its type.
		var record struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			return TraceSummary{}, fmt.Errorf("invalid trace line %d: %w", lineNumber, err)
		}
		if record.Type == "complete" {
			var footer traceComplete
			if err := decodeTraceLine(line, &footer); err != nil {
				return TraceSummary{}, fmt.Errorf("invalid completion footer: %w", err)
			}
			if footer.SampleCount == nil || *footer.SampleCount == 0 || *footer.SampleCount != summary.SampleCount ||
				footer.DurationNS == nil || *footer.DurationNS != summary.DurationNS ||
				footer.DroppedSamples == nil || *footer.DroppedSamples != 0 ||
				footer.IncompleteSamples == nil || *footer.IncompleteSamples != 0 {
				return TraceSummary{}, errors.New("incomplete trace: footer count/window must match and dropped/incomplete samples must explicitly be zero")
			}
			complete = true
			continue
		}
		if summary.SampleCount >= MaxTraceSamples {
			return TraceSummary{}, errors.New("trace exceeds 200000 sample limit")
		}
		var sequence, elapsed *uint64
		switch {
		case record.Type == "sample" && summary.Kind != "correctness":
			var sample traceSample
			if err := decodeTraceLine(line, &sample); err != nil {
				return TraceSummary{}, fmt.Errorf("invalid sample at line %d: %w", lineNumber, err)
			}
			if sample.DurationNS == nil || *sample.DurationNS == 0 || *sample.DurationNS > maxSampleNS {
				return TraceSummary{}, fmt.Errorf("line %d requires integer durationNs in [1,60000000000]", lineNumber)
			}
			sequence, elapsed = sample.Sequence, sample.ElapsedNS
			durations = append(durations, *sample.DurationNS)
			if summary.Kind == "ticks" && *sample.DurationNS > TickDeadlineNS {
				misses++
			}
		case record.Type == "check" && summary.Kind == "correctness":
			var check traceCheck
			if err := decodeTraceLine(line, &check); err != nil {
				return TraceSummary{}, fmt.Errorf("invalid check at line %d: %w", lineNumber, err)
			}
			if check.Passed == nil || seenChecks[check.Name] || !assignCheck(&correctness, check.Name, check.Passed) {
				return TraceSummary{}, fmt.Errorf("line %d requires a unique known correctness check and explicit boolean result", lineNumber)
			}
			seenChecks[check.Name] = true
			sequence, elapsed = check.Sequence, check.ElapsedNS
		default:
			return TraceSummary{}, fmt.Errorf("unexpected record type or trace kind at line %d", lineNumber)
		}
		if sequence == nil || *sequence != summary.SampleCount+1 {
			return TraceSummary{}, fmt.Errorf("line %d has missing, duplicate or noncontiguous sequence", lineNumber)
		}
		if elapsed == nil || *elapsed == 0 || *elapsed > summary.DurationNS ||
			*elapsed < previousElapsed || (summary.Kind == "ticks" && *elapsed == previousElapsed) {
			return TraceSummary{}, fmt.Errorf("line %d has missing, out-of-order or out-of-window elapsedNs", lineNumber)
		}
		previousElapsed = *elapsed
		summary.SampleCount++
	}
	if !complete {
		return TraceSummary{}, errors.New("truncated trace: missing completion footer")
	}
	if summary.Kind == "correctness" {
		for _, check := range correctnessChecks(correctness) {
			if check.value == nil {
				return TraceSummary{}, errors.New("correctness trace must contain all five required checks")
			}
		}
		summary.Correctness = &correctness
	} else {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		// Exact nearest-rank p99: ceil(99*n/100), with a zero-based index.
		// The bounded sample count keeps this integer arithmetic far from overflow.
		p99 := durations[(99*len(durations)+99)/100-1]
		summary.P99NS = &p99
		if summary.Kind == "ticks" {
			summary.DeadlineMissCount = &misses
		}
	}
	return summary, nil
}

func decodeTraceLine(data []byte, target any) error {
	scan := json.NewDecoder(bytes.NewReader(data))
	scan.UseNumber()
	if err := checkShape(scan, reflect.TypeOf(target).Elem(), 0); err != nil {
		return err
	}
	if _, err := scan.Token(); err != io.EOF {
		return errors.New("record must contain exactly one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func assignCheck(correctness *Correctness, name string, passed *bool) bool {
	switch name {
	case "inventoryConserved":
		correctness.InventoryConserved = passed
	case "economyConserved":
		correctness.EconomyConserved = passed
	case "persistencePassed":
		correctness.PersistencePassed = passed
	case "transfersPassed":
		correctness.TransfersPassed = passed
	case "pluginCompatibilityPassed":
		correctness.PluginCompatibilityPassed = passed
	default:
		return false
	}
	return true
}
