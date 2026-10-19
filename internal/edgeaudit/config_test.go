package edgeaudit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestReadConfigRejectsAmbiguousOrUnboundedInput(t *testing.T) {
	cases := map[string]string{
		"duplicate root":       `{"schemaVersion":1,"schemaVersion":2}`,
		"duplicate nested":     `{"edge":{"provider":"one","provider":"two"}}`,
		"escaped duplicate":    `{"edge":{"provider":"one","\u0070rovider":"two"}}`,
		"case alias":           `{"edge":{"provider":"one","Provider":"two"}}`,
		"unknown root":         `{"ddosProtected":true}`,
		"unknown nested":       `{"boundaries":{"ipv6reviewed":true}}`,
		"unknown array field":  `{"advertisedGameplay":[{"address":"8.8.8.8","port":1,"trusted":true}]}`,
		"second object":        `{} {}`,
		"trailing primitive":   `{} true`,
		"trailing garbage":     `{} x`,
		"top array":            `[]`,
		"top null":             `null`,
		"null struct":          `{"origin":null}`,
		"null number":          `{"gamePort":null}`,
		"null string":          `{"serviceId":null}`,
		"null array":           `{"evidence":null}`,
		"string port":          `{"gamePort":"19132"}`,
		"fractional port":      `{"gamePort":19132.5}`,
		"overflow port":        `{"gamePort":99999999999999999999999999999999999999}`,
		"string assertion":     `{"boundaries":{"ipv6Reviewed":"true"}}`,
		"object for array":     `{"exposedTcpPorts":{}}`,
		"invalid utf8":         "{\"serviceId\":\"\xff\"}",
		"empty":                "",
		"oversized":            "{}" + strings.Repeat(" ", MaxConfigBytes),
		"excessive key length": `{"` + strings.Repeat("a", 129) + `":0}`,
		"excessive depth":      `{"edge":` + strings.Repeat("[", maxJSONDepth+1) + "0" + strings.Repeat("]", maxJSONDepth+1) + `}`,
		"excessive values":     `{"exposedTcpPorts":[` + strings.Repeat("1,", maxJSONValues) + `1]}`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ReadConfig(strings.NewReader(input)); err == nil {
				t.Fatal("ambiguous, invalid, or unbounded configuration was accepted")
			}
		})
	}
}

func TestReadConfigPreservesUnknownAssertionsAndRawDigest(t *testing.T) {
	input := []byte(" {\"schemaVersion\":1,\"boundaries\":{\"ipv6Reviewed\":null}}\n")
	c, digest, err := ReadConfig(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if c.Boundaries.IPv6Reviewed != nil {
		t.Fatal("null assertion must remain unknown")
	}
	want := sha256.Sum256(input)
	if digest != hex.EncodeToString(want[:]) {
		t.Fatal("digest must cover the exact original file bytes")
	}
	maximal := "{}" + strings.Repeat(" ", MaxConfigBytes-2)
	if _, _, err := ReadConfig(strings.NewReader(maximal)); err != nil {
		t.Fatalf("file exactly at the documented size cap was rejected: %v", err)
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestReadConfigReturnsReaderErrors(t *testing.T) {
	if _, _, err := ReadConfig(brokenReader{}); err == nil {
		t.Fatal("read failure was ignored")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	c, _ := consistentFixture("both")
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := ReadConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got.Transport != "both" || got.WebRTCPort != c.WebRTCPort || len(got.Evidence) != 3 {
		t.Fatal("typed schema failed to round trip")
	}
}

func FuzzReadConfig(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"edge":{"provider":"one","provider":"two"}}`))
	f.Add([]byte(`{"boundaries":{"directOriginReachable":null}}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		_, _, _ = ReadConfig(bytes.NewReader(input))
	})
}
