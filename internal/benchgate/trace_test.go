package benchgate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// These helpers create synthetic records for parser tests; they do not collect
// game measurements. The measured label is used only to exercise import rules.
func timingTrace(kind, runID, repID string, window uint64, durations []uint64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{\"type\":\"header\",\"schemaVersion\":%q,\"kind\":%q,\"evidenceKind\":\"measured\",\"runId\":%q,\"repetitionId\":%q,\"durationNs\":%d}\n", TraceSchemaVersion, kind, runID, repID, window)
	for i, duration := range durations {
		fmt.Fprintf(&b, "{\"type\":\"sample\",\"sequence\":%d,\"elapsedNs\":%d,\"durationNs\":%d}\n", i+1, uint64(i+1)*window/uint64(len(durations)), duration)
	}
	fmt.Fprintf(&b, "{\"type\":\"complete\",\"sampleCount\":%d,\"droppedSamples\":0,\"incompleteSamples\":0,\"durationNs\":%d}\n", len(durations), window)
	return b.String()
}

func correctnessTrace(runID, repID string, window uint64, checks Correctness) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{\"type\":\"header\",\"schemaVersion\":%q,\"kind\":\"correctness\",\"evidenceKind\":\"measured\",\"runId\":%q,\"repetitionId\":%q,\"durationNs\":%d}\n", TraceSchemaVersion, runID, repID, window)
	for i, check := range correctnessChecks(checks) {
		fmt.Fprintf(&b, "{\"type\":\"check\",\"sequence\":%d,\"elapsedNs\":%d,\"name\":%q,\"passed\":%t}\n", i+1, window, check.name, *check.value)
	}
	fmt.Fprintf(&b, "{\"type\":\"complete\",\"sampleCount\":5,\"droppedSamples\":0,\"incompleteSamples\":0,\"durationNs\":%d}\n", window)
	return b.String()
}

func TestReadTraceExactNearestRankAndDeadline(t *testing.T) {
	durations := make([]uint64, 100)
	for i := range durations {
		durations[i] = 100
	}
	durations[98], durations[99] = TickDeadlineNS, TickDeadlineNS+1
	summary, err := ReadTrace(strings.NewReader(timingTrace("ticks", "run", "repeat", 1_000_000_000, durations)))
	if err != nil {
		t.Fatal(err)
	}
	if summary.SampleCount != 100 || *summary.P99NS != TickDeadlineNS || *summary.DeadlineMissCount != 1 || summary.Correctness != nil {
		t.Fatalf("wrong nearest-rank/strict-deadline calculation: %+v", summary)
	}
	// ceil(.99 * 101) = 100, which differs from interpolated quantiles.
	durations = make([]uint64, 101)
	for i := range durations {
		durations[i] = uint64(101 - i)
	}
	summary, err = ReadTrace(strings.NewReader(timingTrace("interactions", "run", "repeat", 1000, durations)))
	if err != nil || *summary.P99NS != 100 || summary.DeadlineMissCount != nil || summary.SampleCount != 101 {
		t.Fatalf("wrong interaction nearest-rank result: %+v %v", summary, err)
	}
}

func TestReadTracePreservesEvidenceKindAndAcceptsCRLF(t *testing.T) {
	data := timingTrace("ticks", "run", "repeat", 1000, []uint64{10})
	data = strings.ReplaceAll(data, `"evidenceKind":"measured"`, `"evidenceKind":"synthetic"`)
	summary, err := ReadTrace(strings.NewReader(strings.ReplaceAll(data, "\n", "\r\n")))
	if err != nil || summary.EvidenceKind != "synthetic" || *summary.P99NS != 10 {
		t.Fatalf("synthetic evidence was relabeled: %+v %v", summary, err)
	}
}

func TestReadTraceRejectsMalformedIncompleteAndAmbiguousRecords(t *testing.T) {
	base := timingTrace("ticks", "run", "repeat", 1000, []uint64{10, 20, 30})
	lines := strings.SplitAfter(base, "\n")
	cases := map[string]string{
		"empty":                  "",
		"no footer":              strings.Join(lines[:4], ""),
		"partial JSON":           strings.Join(lines[:4], "") + `{"type":"complete"` + "\n",
		"no final newline":       strings.TrimSuffix(base, "\n"),
		"blank record":           lines[0] + "\n" + strings.Join(lines[1:], ""),
		"trailing record":        base + lines[1],
		"trailing garbage":       base + "garbage\n",
		"old schema":             strings.Replace(base, TraceSchemaVersion, "unknown/v0", 1),
		"wrong kind":             strings.Replace(base, `"kind":"ticks"`, `"kind":"unknown"`, 1),
		"missing header":         strings.Join(lines[1:], ""),
		"invalid evidence kind":  strings.Replace(base, `"evidenceKind":"measured"`, `"evidenceKind":"unknown"`, 1),
		"missing identity":       strings.Replace(base, `"runId":"run",`, "", 1),
		"case mismatch":          strings.Replace(base, `"durationNs":10`, `"DurationNs":10`, 1),
		"duplicate field":        strings.Replace(base, `"durationNs":10`, `"durationNs":999,"durationNs":10`, 1),
		"extra field":            strings.Replace(base, `"durationNs":10`, `"durationNs":10,"ignored":true`, 1),
		"extra object":           strings.Replace(base, lines[1], strings.TrimSuffix(lines[1], "\n")+" {}\n", 1),
		"duplicate sequence":     strings.Replace(base, `"sequence":2`, `"sequence":1`, 1),
		"gap in sequence":        strings.Replace(base, `"sequence":2`, `"sequence":3`, 1),
		"missing sequence":       strings.Replace(base, `"sequence":2,`, "", 1),
		"missing timestamp":      strings.Replace(base, `"elapsedNs":666,`, "", 1),
		"backwards timestamp":    strings.Replace(base, `"elapsedNs":666`, `"elapsedNs":332`, 1),
		"duplicate tick time":    strings.Replace(base, `"elapsedNs":666`, `"elapsedNs":333`, 1),
		"outside window":         strings.Replace(base, `"elapsedNs":1000`, `"elapsedNs":1001`, 1),
		"zero duration":          strings.Replace(base, `"durationNs":10`, `"durationNs":0`, 1),
		"negative duration":      strings.Replace(base, `"durationNs":10`, `"durationNs":-1`, 1),
		"fractional nanoseconds": strings.Replace(base, `"durationNs":10`, `"durationNs":1.5`, 1),
		"null duration":          strings.Replace(base, `"durationNs":10`, `"durationNs":null`, 1),
		"nonfinite":              strings.Replace(base, `"durationNs":10`, `"durationNs":NaN`, 1),
		"integer overflow":       strings.Replace(base, `"durationNs":10`, `"durationNs":18446744073709551616`, 1),
		"duration limit":         strings.Replace(base, `"durationNs":10`, `"durationNs":60000000001`, 1),
		"footer count mismatch":  strings.Replace(base, `"sampleCount":3`, `"sampleCount":4`, 1),
		"missing footer count":   strings.Replace(base, `"sampleCount":3,`, "", 1),
		"missing loss count":     strings.Replace(base, `"droppedSamples":0,`, "", 1),
		"null loss count":        strings.Replace(base, `"droppedSamples":0`, `"droppedSamples":null`, 1),
		"lost samples":           strings.Replace(base, `"droppedSamples":0`, `"droppedSamples":1`, 1),
		"unfinished samples":     strings.Replace(base, `"incompleteSamples":0`, `"incompleteSamples":1`, 1),
		"window not completed":   strings.Join(lines[:4], "") + strings.Replace(lines[4], `"durationNs":1000`, `"durationNs":999`, 1),
		"invalid UTF8":           strings.Replace(base, `"runId":"run"`, "\"runId\":\"r\xffun\"", 1),
		"line limit":             lines[0] + strings.Repeat(" ", MaxTraceLineBytes) + lines[1],
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			summary, err := ReadTrace(strings.NewReader(data))
			if err == nil || summary.SchemaVersion != "" {
				t.Fatalf("invalid/partial trace returned usable summary: %+v, %v", summary, err)
			}
		})
	}
}

func TestReadTraceCorrectnessIsIndependentAndComplete(t *testing.T) {
	passed, failed := true, false
	checks := Correctness{&passed, &failed, &passed, &passed, &passed}
	base := correctnessTrace("run", "repeat", 1000, checks)
	summary, err := ReadTrace(strings.NewReader(base))
	if err != nil || summary.P99NS != nil || summary.DeadlineMissCount != nil || *summary.Correctness.EconomyConserved {
		t.Fatalf("correctness was inferred from performance or failure lost: %+v %v", summary, err)
	}
	lines := strings.SplitAfter(base, "\n")
	missing := strings.Join(lines[:5], "") + strings.Replace(lines[6], `"sampleCount":5`, `"sampleCount":4`, 1)
	for name, data := range map[string]string{
		"missing check":   missing,
		"duplicate check": strings.Replace(base, "economyConserved", "inventoryConserved", 1),
		"unknown check":   strings.Replace(base, "economyConserved", "someOtherCheck", 1),
		"missing boolean": strings.Replace(base, `,"passed":false`, "", 1),
		"null boolean":    strings.Replace(base, `"passed":false`, `"passed":null`, 1),
		"numeric boolean": strings.Replace(base, `"passed":false`, `"passed":0`, 1),
		"sample as check": strings.Replace(base, `"type":"check"`, `"type":"sample"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadTrace(strings.NewReader(data)); err == nil {
				t.Fatal("incomplete correctness accepted")
			}
		})
	}
}

type failedTraceReader struct{}

func (failedTraceReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestReadTraceRejectsReadFailureEvenAfterFooter(t *testing.T) {
	data := timingTrace("ticks", "run", "repeat", 1000, []uint64{10})
	if _, err := ReadTrace(io.MultiReader(strings.NewReader(data), failedTraceReader{})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read failure ignored after apparent completion: %v", err)
	}
}

func TestReadTraceByteLimit(t *testing.T) {
	var b bytes.Buffer
	base := timingTrace("ticks", "run", "repeat", 100000, []uint64{10})
	b.WriteString(strings.SplitAfter(base, "\n")[0])
	for i := 1; i <= 5000; i++ {
		line := fmt.Sprintf(`{"type":"sample","sequence":%d,"elapsedNs":%d,"durationNs":10}`, i, i)
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", MaxTraceLineBytes-len(line)-1))
		b.WriteByte('\n')
	}
	if _, err := ReadTrace(&b); err == nil || !strings.Contains(err.Error(), "16 MiB") {
		t.Fatalf("unbounded trace accepted or wrong limit: %v", err)
	}
}
