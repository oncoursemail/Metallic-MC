package benchgate

import (
	"strings"
	"testing"
	"time"
)

func TestDecodeRejectsAmbiguousOrUnboundedJSON(t *testing.T) {
	cases := map[string]string{
		"duplicate top":     `{"schemaVersion":"one","schemaVersion":"two"}`,
		"duplicate nested":  `{"baseline":{"engine":{"name":"a","name":"b"}}}`,
		"unknown":           `{"policy":{"allowSynthetic":true}}`,
		"wrong casing":      `{"SchemaVersion":"metallic.benchmark-gate/v1"}`,
		"trailing object":   `{} {}`,
		"trailing garbage":  `{} nope`,
		"infinite numeric":  `{"candidate":{"repetitions":[{"metrics":{"p99TickMs":1e309}}]}}`,
		"nan":               `{"candidate":{"repetitions":[{"metrics":{"p99TickMs":NaN}}]}}`,
		"negative unsigned": `{"candidate":{"repetitions":[{"tickSamples":-1}]}}`,
		"oversize":          strings.Repeat(" ", MaxInputBytes+1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(data)); err == nil {
				t.Fatal("accepted ambiguous or invalid JSON")
			}
		})
	}
}

func TestDecodeMissingEvidenceRemainsIneligible(t *testing.T) {
	report, err := Decode(strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	result := Screen(report, "", time.Time{})
	if result.Status != "insufficient_evidence" || result.ProductionReady || result.EvidenceMode != "summary_screening_only" {
		t.Fatalf("unsafe missing-evidence result: %+v", result)
	}
}
