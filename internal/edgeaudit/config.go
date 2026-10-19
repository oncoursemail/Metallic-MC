// Package edgeaudit checks an operator-supplied edge configuration offline.
// It never scans, resolves DNS, opens a listener, changes a firewall, or proves
// that a provider will mitigate an attack. Evidence digests establish byte
// identity, not the truth, provenance, or completeness of an assertion.
package edgeaudit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

const (
	MaxConfigBytes   = 64 * 1024
	MaxArtifactBytes = 1024 * 1024
	MaxEvidence      = 16
	maxJSONDepth     = 24
	maxJSONValues    = 4096
)

// Endpoint deliberately accepts numeric IPs only. No DNS resolution is done.
type Endpoint struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

type Config struct {
	SchemaVersion      int        `json:"schemaVersion"`
	ServiceID          string     `json:"serviceId"`
	Audience           string     `json:"audience"`
	Transport          string     `json:"transport"`
	Origin             Endpoint   `json:"origin"`
	GamePort           int        `json:"gamePort"`
	WebRTCPort         int        `json:"webrtcPort"`
	ExposedTCPPorts    []int      `json:"exposedTcpPorts"`
	ExposedUDPPorts    []int      `json:"exposedUdpPorts"`
	AdvertisedGameplay []Endpoint `json:"advertisedGameplay"`
	Edge               Edge       `json:"edge"`
	Boundaries         Boundaries `json:"boundaries"`
	Evidence           []Evidence `json:"evidence"`
}

type Edge struct {
	Provider                  string   `json:"provider"`
	Plan                      string   `json:"plan"`
	ProtectedAddresses        []string `json:"protectedAddresses"`
	CoveredTransports         []string `json:"coveredTransports"`
	CommunityHostingPermitted *bool    `json:"communityHostingPermitted"`
	ProviderEvidenceID        string   `json:"providerEvidenceId"`
	AssignmentEvidenceID      string   `json:"assignmentEvidenceId"`
}

// Pointers distinguish an explicit assertion from an omitted/null answer.
type Boundaries struct {
	DirectOriginReachable       *bool  `json:"directOriginReachable"`
	IPv6Reviewed                *bool  `json:"ipv6Reviewed"`
	PublicAdministration        *bool  `json:"publicAdministration"`
	ResourcePackLeaksOrigin     *bool  `json:"resourcePackLeaksOrigin"`
	AuxiliaryServicesLeakOrigin *bool  `json:"auxiliaryServicesLeakOrigin"`
	TenantEgressControlled      *bool  `json:"tenantEgressControlled"`
	AuthenticatedForwarding     *bool  `json:"authenticatedForwarding"`
	FirewallRestrictsPeers      *bool  `json:"firewallRestrictsPeers"`
	SignalingProtected          *bool  `json:"signalingProtected"`
	AutomaticOriginDiscovery    *bool  `json:"automaticOriginDiscovery"`
	ExposureEvidenceID          string `json:"exposureEvidenceId"`
}

type Evidence struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Reference  string `json:"reference"`
	ObservedAt string `json:"observedAt"`
	ExpiresAt  string `json:"expiresAt"`
	Artifact   string `json:"artifact"`
	SHA256     string `json:"sha256"`
}

// ReadConfig accepts exactly one bounded UTF-8 JSON object. It rejects duplicate
// keys (including escaped aliases), unknown or incorrectly cased keys, trailing
// documents, excessive nesting and invalid primitive types.
func ReadConfig(r io.Reader) (Config, string, error) {
	var c Config
	data, err := io.ReadAll(io.LimitReader(r, MaxConfigBytes+1))
	if err != nil {
		return c, "", fmt.Errorf("read config: %w", err)
	}
	if len(data) > MaxConfigBytes {
		return c, "", fmt.Errorf("config exceeds %d bytes", MaxConfigBytes)
	}
	if !utf8.Valid(data) {
		return c, "", fmt.Errorf("config must be valid UTF-8")
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return c, "", fmt.Errorf("config must be a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	budget := maxJSONValues
	if err := checkJSONValue(dec, 0, &budget); err != nil {
		return c, "", err
	}
	if _, err := dec.Token(); err != io.EOF {
		return c, "", fmt.Errorf("config must contain exactly one JSON document")
	}
	if err := checkExactFields(data, reflect.TypeOf(c), "$", 0); err != nil {
		return c, "", err
	}
	dec = json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, "", fmt.Errorf("decode config: %w", err)
	}
	digest := sha256.Sum256(data)
	return c, hex.EncodeToString(digest[:]), nil
}

func checkJSONValue(dec *json.Decoder, depth int, budget *int) error {
	if depth > maxJSONDepth || *budget <= 0 {
		return fmt.Errorf("JSON depth or value limit exceeded")
	}
	*budget--
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return fmt.Errorf("invalid JSON object: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok || len(key) > 128 {
				return fmt.Errorf("invalid JSON object key")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = true
			if err := checkJSONValue(dec, depth+1, budget); err != nil {
				return err
			}
		}
		close, err := dec.Token()
		if err != nil || close != json.Delim('}') {
			return fmt.Errorf("invalid JSON object terminator")
		}
	case '[':
		for dec.More() {
			if err := checkJSONValue(dec, depth+1, budget); err != nil {
				return err
			}
		}
		close, err := dec.Token()
		if err != nil || close != json.Delim(']') {
			return fmt.Errorf("invalid JSON array terminator")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
	return nil
}

// encoding/json matches struct fields without regard to case; the schema here
// intentionally does not, so Provider/provider cannot override each other.
func checkExactFields(raw []byte, t reflect.Type, path string, depth int) error {
	if depth > maxJSONDepth {
		return fmt.Errorf("JSON depth limit exceeded")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if t.Kind() == reflect.Pointer {
			return nil // An unknown boundary assertion remains unverified.
		}
		return fmt.Errorf("%s must not be null", path)
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return fmt.Errorf("%s must be an object", path)
		}
		fields := make(map[string]reflect.Type)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				fields[name] = f.Type
			}
		}
		for key, value := range obj {
			field, ok := fields[key]
			if !ok {
				return fmt.Errorf("unknown JSON field %q", path+"."+key)
			}
			if err := checkExactFields(value, field, path+"."+key, depth+1); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return fmt.Errorf("%s must be an array", path)
		}
		for i, value := range values {
			if err := checkExactFields(value, t.Elem(), fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
