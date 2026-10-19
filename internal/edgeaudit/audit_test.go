package edgeaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

type memoryArtifacts map[string][]byte

func (m memoryArtifacts) ReadArtifact(name string) ([]byte, error) {
	data, ok := m[name]
	if !ok {
		return nil, errors.New("unavailable")
	}
	return data, nil
}

func boolValue(b bool) *bool { return &b }

// All fixture provider claims and evidence are deliberately fabricated. The
// global address is used only to exercise numeric-address policy. No packet is
// sent, and a consistent result must still report protectionVerified=false.
func consistentFixture(transport string) (Config, memoryArtifacts) {
	c := Config{
		SchemaVersion: 1, ServiceID: "offline-test", Audience: "community", Transport: transport,
		Origin: Endpoint{Address: "10.77.0.2", Port: 25565}, GamePort: 19132,
		ExposedTCPPorts: []int{}, ExposedUDPPorts: []int{},
		AdvertisedGameplay: []Endpoint{{Address: "8.8.8.8", Port: 19132}},
		Edge: Edge{
			Provider: "Fabricated unit-test provider", Plan: "Not a real plan",
			ProtectedAddresses: []string{"8.8.8.8"}, CoveredTransports: []string{transport},
			CommunityHostingPermitted: boolValue(true), ProviderEvidenceID: "coverage", AssignmentEvidenceID: "assignment",
		},
		Boundaries: Boundaries{
			DirectOriginReachable: boolValue(false), IPv6Reviewed: boolValue(true), PublicAdministration: boolValue(false),
			ResourcePackLeaksOrigin: boolValue(false), AuxiliaryServicesLeakOrigin: boolValue(false),
			TenantEgressControlled: boolValue(true), AuthenticatedForwarding: boolValue(true), FirewallRestrictsPeers: boolValue(true),
			SignalingProtected: boolValue(true), AutomaticOriginDiscovery: boolValue(false), ExposureEvidenceID: "exposure",
		},
	}
	switch transport {
	case "java":
		c.ExposedTCPPorts = []int{19132}
	case "legacy":
		c.ExposedUDPPorts = []int{19132}
	case "nethernet":
		c.ExposedTCPPorts, c.ExposedUDPPorts = []int{19132}, []int{19132}
	case "both":
		c.WebRTCPort = 19133
		c.ExposedTCPPorts, c.ExposedUDPPorts = []int{19132}, []int{19132, 19133}
		c.AdvertisedGameplay = append(c.AdvertisedGameplay, Endpoint{Address: "8.8.8.8", Port: 19133})
		c.Edge.CoveredTransports = []string{"nethernet", "legacy"}
	}
	artifacts := memoryArtifacts{}
	for _, item := range []struct{ id, kind string }{
		{"coverage", "provider_coverage"}, {"assignment", "edge_assignment"}, {"exposure", "exposure_review"},
	} {
		name := item.id + ".txt"
		data := []byte("Fabricated test assertion: " + item.kind)
		digest := sha256.Sum256(data)
		artifacts[name] = data
		c.Evidence = append(c.Evidence, Evidence{
			ID: item.id, Kind: item.kind, Reference: "https://example.invalid/review/" + item.id,
			ObservedAt: testNow.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: testNow.Add(time.Hour).Format(time.RFC3339),
			Artifact: name, SHA256: hex.EncodeToString(digest[:]),
		})
	}
	return c, artifacts
}

func containsFinding(report Report, code string) bool {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func requireReport(t *testing.T, report Report, status, code string) {
	t.Helper()
	if report.Status != status {
		t.Fatalf("status = %q, want %q; findings: %+v", report.Status, status, report.Findings)
	}
	if code != "" && !containsFinding(report, code) {
		t.Fatalf("missing finding %q: %+v", code, report.Findings)
	}
	if report.ProtectionVerified || report.LiveNetworkTested {
		t.Fatal("offline preflight must never claim live protection or a live test")
	}
}

func TestAllExplicitTransportProfiles(t *testing.T) {
	for _, transport := range []string{"java", "legacy", "nethernet", "both"} {
		t.Run(transport, func(t *testing.T) {
			c, artifacts := consistentFixture(transport)
			r := Audit(c, testNow, artifacts)
			requireReport(t, r, "configuration_consistent", "")
			for _, e := range r.Evidence {
				if !e.DigestMatched || !e.Fresh || e.Status != "integrity_checked_assertion" || e.Provenance != "operator_supplied_not_independently_verified" {
					t.Fatalf("incorrect evidence characterization: %+v", e)
				}
			}
		})
	}
}

func TestTransportAndExposureBypasses(t *testing.T) {
	cases := []struct {
		name, transport, status, code string
		change                        func(*Config)
	}{
		{"unsupported transport", "nethernet", "blocked", "transport", func(c *Config) { c.Transport = "udp" }},
		{"nethernet missing tcp", "nethernet", "blocked", "required_port_missing", func(c *Config) { c.ExposedTCPPorts = nil }},
		{"nethernet missing udp", "nethernet", "blocked", "required_port_missing", func(c *Config) { c.ExposedUDPPorts = nil }},
		{"both missing webrtc", "both", "blocked", "webrtc_port", func(c *Config) { c.WebRTCPort = 0 }},
		{"both collision", "both", "blocked", "webrtc_port", func(c *Config) { c.WebRTCPort = c.GamePort }},
		{"both webrtc closed", "both", "blocked", "required_port_missing", func(c *Config) { c.ExposedUDPPorts = []int{c.GamePort} }},
		{"both absent webrtc advertisement", "both", "blocked", "advertised_transport_missing", func(c *Config) { c.AdvertisedGameplay = c.AdvertisedGameplay[:1] }},
		{"separate port without both", "nethernet", "blocked", "unexpected_webrtc_port", func(c *Config) { c.WebRTCPort = 19133 }},
		{"extra admin port", "java", "blocked", "unexpected_exposure", func(c *Config) { c.ExposedTCPPorts = append(c.ExposedTCPPorts, 22) }},
		{"duplicate exposed port", "java", "blocked", "invalid_port", func(c *Config) { c.ExposedTCPPorts = []int{19132, 19132} }},
		{"port zero", "java", "blocked", "game_port", func(c *Config) { c.GamePort = 0 }},
		{"port overflow", "java", "blocked", "game_port", func(c *Config) { c.GamePort = 65536 }},
		{"negative port", "java", "blocked", "invalid_port", func(c *Config) { c.ExposedTCPPorts = []int{-1} }},
		{"too many ports", "java", "blocked", "port_limit", func(c *Config) { c.ExposedTCPPorts = []int{1, 2, 3, 4, 5, 6, 7, 8, 9} }},
		{"wrong advertised port", "java", "blocked", "advertised_port", func(c *Config) { c.AdvertisedGameplay[0].Port = 25566 }},
		{"duplicate advertisement", "java", "blocked", "duplicate_advertisement", func(c *Config) { c.AdvertisedGameplay = append(c.AdvertisedGameplay, c.AdvertisedGameplay[0]) }},
		{"undeclared public edge", "nethernet", "blocked", "unprotected_advertisement", func(c *Config) { c.AdvertisedGameplay[0].Address = "1.1.1.1" }},
		{"no advertisements", "nethernet", "blocked", "advertised_address_count", func(c *Config) { c.AdvertisedGameplay = nil }},
		{"no protected addresses", "nethernet", "blocked", "protected_address_count", func(c *Config) { c.Edge.ProtectedAddresses = nil }},
		{"duplicate edge", "nethernet", "blocked", "protected_address", func(c *Config) { c.Edge.ProtectedAddresses = []string{"8.8.8.8", "8.8.8.8"} }},
		{"generic bedrock coverage", "nethernet", "blocked", "provider_transport", func(c *Config) { c.Edge.CoveredTransports = []string{"bedrock"} }},
		{"legacy-only provider", "nethernet", "unverified", "provider_coverage_missing", func(c *Config) { c.Edge.CoveredTransports = []string{"legacy"} }},
		{"missing provider", "java", "unverified", "provider_missing", func(c *Config) { c.Edge.Provider = "" }},
		{"unknown community permission", "java", "unverified", "boundary_unverified", func(c *Config) { c.Edge.CommunityHostingPermitted = nil }},
		{"community prohibited", "java", "blocked", "unsafe_boundary", func(c *Config) { c.Edge.CommunityHostingPermitted = boolValue(false) }},
		{"unknown ipv6", "java", "unverified", "boundary_unverified", func(c *Config) { c.Boundaries.IPv6Reviewed = nil }},
		{"direct origin", "java", "blocked", "unsafe_boundary", func(c *Config) { c.Boundaries.DirectOriginReachable = boolValue(true) }},
		{"public admin", "java", "blocked", "unsafe_boundary", func(c *Config) { c.Boundaries.PublicAdministration = boolValue(true) }},
		{"pack origin leak", "java", "blocked", "unsafe_boundary", func(c *Config) { c.Boundaries.ResourcePackLeaksOrigin = boolValue(true) }},
		{"auxiliary origin leak", "java", "blocked", "unsafe_boundary", func(c *Config) { c.Boundaries.AuxiliaryServicesLeakOrigin = boolValue(true) }},
		{"tenant egress leak", "java", "blocked", "unsafe_boundary", func(c *Config) { c.Boundaries.TenantEgressControlled = boolValue(false) }},
		{"untrusted forwarding", "java", "blocked", "unsafe_boundary", func(c *Config) { c.Boundaries.AuthenticatedForwarding = boolValue(false) }},
		{"firewall unknown", "java", "unverified", "boundary_unverified", func(c *Config) { c.Boundaries.FirewallRestrictsPeers = nil }},
		{"signaling not protected", "nethernet", "blocked", "unsafe_boundary", func(c *Config) { c.Boundaries.SignalingProtected = boolValue(false) }},
		{"nxs discovers home address", "nethernet", "blocked", "unsafe_boundary", func(c *Config) { c.Boundaries.AutomaticOriginDiscovery = boolValue(true) }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c, artifacts := consistentFixture(tt.transport)
			tt.change(&c)
			requireReport(t, Audit(c, testNow, artifacts), tt.status, tt.code)
		})
	}
}

func TestNumericAddressPolicyRejectsBypasses(t *testing.T) {
	for _, address := range []string{
		"origin.example.invalid", "https://8.8.8.8", "8.8.8.8:19132", "8.8.8.8/32", "8.8.8.8 ", "008.008.008.008",
		"0.0.0.0", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.1.1", "172.16.0.1", "192.168.1.1",
		"192.0.0.9", "192.0.2.1", "192.88.99.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:8.8.8.8", "::ffff:10.0.0.1", "fc00::1", "fe80::1", "ff02::1", "fe80::1%eth0",
		"2001:4860:4860::8888%eth0", "2001:db8::1", "2001:2::1", "2002:808:808::1", "3fff::1", "64:ff9b::808:808", "5f00::1",
	} {
		t.Run("edge/"+address, func(t *testing.T) {
			c, artifacts := consistentFixture("nethernet")
			c.Edge.ProtectedAddresses = []string{address}
			c.AdvertisedGameplay[0].Address = address
			requireReport(t, Audit(c, testNow, artifacts), "blocked", "protected_address")
		})
	}
	for _, address := range []string{"8.8.8.8", "2001:4860:4860::8888", "origin.example.invalid", "100.64.0.1", "169.254.1.1", "fe80::1", "fd00::1%eth0", "::ffff:10.0.0.1", "10.1.0.0/16", "10.0.0.1:25565", "::"} {
		t.Run("origin/"+address, func(t *testing.T) {
			c, artifacts := consistentFixture("java")
			c.Origin.Address = address
			requireReport(t, Audit(c, testNow, artifacts), "blocked", "origin_private_only")
		})
	}
	for _, address := range []string{"10.77.0.2", "172.16.0.2", "192.168.1.2", "127.0.0.1", "::1", "fd77::2"} {
		t.Run("private origin/"+address, func(t *testing.T) {
			c, artifacts := consistentFixture("java")
			c.Origin.Address = address
			requireReport(t, Audit(c, testNow, artifacts), "configuration_consistent", "")
		})
	}
}

func TestMultipleEdgesAndCanonicalIPv6Comparison(t *testing.T) {
	c, artifacts := consistentFixture("nethernet")
	c.Edge.ProtectedAddresses = append(c.Edge.ProtectedAddresses, "2001:4860:4860::8888")
	c.AdvertisedGameplay = append(c.AdvertisedGameplay, Endpoint{Address: "2001:4860:4860:0:0:0:0:8888", Port: c.GamePort})
	requireReport(t, Audit(c, testNow, artifacts), "configuration_consistent", "")
	c.AdvertisedGameplay = append(c.AdvertisedGameplay, Endpoint{Address: "2001:4860:4860::8888", Port: c.GamePort})
	requireReport(t, Audit(c, testNow, artifacts), "blocked", "duplicate_advertisement")
}

func TestUnknownConfigCannotPass(t *testing.T) {
	requireReport(t, Audit(Config{}, testNow, nil), "blocked", "evidence_missing")
	c, artifacts := consistentFixture("java")
	c.Evidence = nil
	requireReport(t, Audit(c, testNow, artifacts), "unverified", "evidence_missing")
	c, artifacts = consistentFixture("java")
	requireReport(t, Audit(c, time.Time{}, artifacts), "blocked", "clock_missing")
}

func TestReportOrderIsDeterministic(t *testing.T) {
	c, artifacts := consistentFixture("both")
	c.ExposedTCPPorts, c.ExposedUDPPorts = []int{22, 443}, []int{53, 123}
	want := Audit(c, testNow, artifacts)
	for range 16 {
		if got := Audit(c, testNow, artifacts); !reflect.DeepEqual(got, want) {
			t.Fatal("same offline snapshot produced different report ordering")
		}
	}
}

func ExampleAudit() {
	report := Audit(Config{}, time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC), nil)
	fmt.Println(report.Status, report.ProtectionVerified, report.LiveNetworkTested)
	// Output: blocked false false
}
