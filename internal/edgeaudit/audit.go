package edgeaudit

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"
)

type Finding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Field    string `json:"field"`
	Message  string `json:"message"`
}

type Report struct {
	SchemaVersion       int              `json:"schemaVersion"`
	Mode                string           `json:"mode"`
	Status              string           `json:"status"`
	CheckedAt           string           `json:"checkedAt"`
	ConfigurationSHA256 string           `json:"configurationSha256,omitempty"`
	ProtectionVerified  bool             `json:"protectionVerified"`
	LiveNetworkTested   bool             `json:"liveNetworkTested"`
	Scope               string           `json:"scope"`
	Findings            []Finding        `json:"findings"`
	Evidence            []EvidenceResult `json:"evidence"`
}

func (r *Report) add(severity, code, field, message string) {
	r.Findings = append(r.Findings, Finding{Severity: severity, Code: code, Field: field, Message: message})
}

// Audit compares declarations and local artifact integrity. Even a consistent
// result leaves live protection, provider claims, and exposure unverified.
// now is explicit so offline runs are reproducible and freshness is testable.
func Audit(c Config, now time.Time, reader ArtifactReader) Report {
	r := Report{
		SchemaVersion: 1, Mode: "offline_configuration_audit", Status: "configuration_consistent",
		CheckedAt: now.UTC().Format(time.RFC3339), Findings: []Finding{}, Evidence: []EvidenceResult{},
		Scope: "Checks operator declarations, endpoint consistency, evidence freshness and local artifact digests only. No DNS, scanning, deployment or live protection verification. A matching digest does not establish truth or provenance.",
	}
	if now.IsZero() {
		r.add("error", "clock_missing", "$", "A current audit time is required.")
	}
	if c.SchemaVersion != 1 {
		r.add("error", "schema_version", "schemaVersion", "Only schemaVersion 1 is supported.")
	}
	if !safeID.MatchString(c.ServiceID) {
		r.add("error", "service_id", "serviceId", "A valid service identifier is required.")
	}
	if c.Audience != "personal" && c.Audience != "community" {
		r.add("error", "audience", "audience", "Audience must be personal or community.")
	}
	if !transportValid(c.Transport) {
		r.add("error", "transport", "transport", "Transport must be java, nethernet, legacy, or both.")
	}
	if !privateOrigin(c.Origin.Address) || !portValid(c.Origin.Port) {
		r.add("error", "origin_private_only", "origin", "Origin must have a valid port and a numeric RFC1918, ULA, or loopback address; public, mapped, link-local, zoned and hostname origins are rejected.")
	}
	if !portValid(c.GamePort) {
		r.add("error", "game_port", "gamePort", "gamePort must be between 1 and 65535.")
	}
	if c.Transport == "both" {
		if !portValid(c.WebRTCPort) || c.WebRTCPort == c.GamePort {
			r.add("error", "webrtc_port", "webrtcPort", "The both transport needs a separate UDP webrtcPort, different from gamePort.")
		}
	} else if c.WebRTCPort != 0 {
		r.add("error", "unexpected_webrtc_port", "webrtcPort", "This schema permits a separate webrtcPort only for both transport.")
	}
	tcp := r.ports(c.ExposedTCPPorts, "exposedTcpPorts")
	udp := r.ports(c.ExposedUDPPorts, "exposedUdpPorts")
	wantTCP, wantUDP := map[int]bool{}, map[int]bool{}
	switch c.Transport {
	case "java":
		wantTCP[c.GamePort] = true
	case "legacy":
		wantUDP[c.GamePort] = true
		r.add("info", "legacy_transition", "transport", "RakNet is transitional; confirm current client support and migrate to NetherNet before relying on it for new clients.")
	case "nethernet":
		wantTCP[c.GamePort], wantUDP[c.GamePort] = true, true
	case "both":
		wantTCP[c.GamePort], wantUDP[c.GamePort], wantUDP[c.WebRTCPort] = true, true, true
	}
	r.comparePorts(tcp, wantTCP, "exposedTcpPorts")
	r.comparePorts(udp, wantUDP, "exposedUdpPorts")
	protected := make(map[netip.Addr]bool)
	if len(c.Edge.ProtectedAddresses) < 1 || len(c.Edge.ProtectedAddresses) > 16 {
		r.add("error", "protected_address_count", "edge.protectedAddresses", "Declare between one and sixteen exact protected IP addresses.")
	}
	for i, raw := range c.Edge.ProtectedAddresses {
		addr, ok := publicEdge(raw)
		if !ok || protected[addr] {
			r.add("error", "protected_address", fmt.Sprintf("edge.protectedAddresses[%d]", i), "Use unique global numeric edge addresses; private, special-use, hostname, CIDR, zoned and mapped addresses are rejected.")
			continue
		}
		protected[addr] = true
	}
	if len(c.AdvertisedGameplay) < 1 || len(c.AdvertisedGameplay) > 32 {
		r.add("error", "advertised_address_count", "advertisedGameplay", "Declare between one and thirty-two gameplay endpoints.")
	}
	seenEndpoints := make(map[string]bool)
	seenGameplayPorts := make(map[int]bool)
	for i, ep := range c.AdvertisedGameplay {
		field := fmt.Sprintf("advertisedGameplay[%d]", i)
		addr, ok := publicEdge(ep.Address)
		if !ok || !protected[addr] {
			r.add("error", "unprotected_advertisement", field+".address", "Every advertised gameplay address must exactly match a declared protected edge address.")
		}
		allowedPort := ep.Port == c.GamePort && portValid(ep.Port)
		if c.Transport == "both" {
			allowedPort = allowedPort || (ep.Port == c.WebRTCPort && portValid(ep.Port))
		}
		if !allowedPort {
			r.add("error", "advertised_port", field+".port", "Gameplay advertisement uses a port outside the declared transport.")
		}
		key := fmt.Sprintf("%s/%d", addr, ep.Port)
		if seenEndpoints[key] {
			r.add("error", "duplicate_advertisement", field, "Gameplay endpoint is duplicated.")
		}
		seenEndpoints[key], seenGameplayPorts[ep.Port] = true, true
	}
	if !seenGameplayPorts[c.GamePort] || (c.Transport == "both" && !seenGameplayPorts[c.WebRTCPort]) {
		r.add("error", "advertised_transport_missing", "advertisedGameplay", "Declare gameplay endpoints for each selected transport port.")
	}
	if strings.TrimSpace(c.Edge.Provider) == "" || len(c.Edge.Provider) > 120 || strings.TrimSpace(c.Edge.Plan) == "" || len(c.Edge.Plan) > 120 {
		r.add("unverified", "provider_missing", "edge", "Record the actual provider and plan; a name alone never proves protection.")
	}
	covered := make(map[string]bool)
	for _, t := range c.Edge.CoveredTransports {
		if (t != "java" && t != "nethernet" && t != "legacy") || covered[t] {
			r.add("error", "provider_transport", "edge.coveredTransports", "Coverage must list unique concrete transports: java, nethernet, or legacy.")
		}
		covered[t] = true
	}
	required := []string{c.Transport}
	if c.Transport == "both" {
		required = []string{"nethernet", "legacy"}
	}
	for _, t := range required {
		if !covered[t] {
			r.add("unverified", "provider_coverage_missing", "edge.coveredTransports", "Provider evidence must explicitly cover every selected transport; generic Bedrock or UDP coverage is insufficient for NetherNet.")
		}
	}
	if c.Audience == "community" {
		r.assert(c.Edge.CommunityHostingPermitted, true, "edge.communityHostingPermitted")
		r.assert(c.Boundaries.TenantEgressControlled, true, "boundaries.tenantEgressControlled")
	}
	r.assert(c.Boundaries.DirectOriginReachable, false, "boundaries.directOriginReachable")
	r.assert(c.Boundaries.IPv6Reviewed, true, "boundaries.ipv6Reviewed")
	r.assert(c.Boundaries.PublicAdministration, false, "boundaries.publicAdministration")
	r.assert(c.Boundaries.ResourcePackLeaksOrigin, false, "boundaries.resourcePackLeaksOrigin")
	r.assert(c.Boundaries.AuxiliaryServicesLeakOrigin, false, "boundaries.auxiliaryServicesLeakOrigin")
	r.assert(c.Boundaries.AuthenticatedForwarding, true, "boundaries.authenticatedForwarding")
	r.assert(c.Boundaries.FirewallRestrictsPeers, true, "boundaries.firewallRestrictsPeers")
	if c.Transport == "nethernet" || c.Transport == "both" {
		r.assert(c.Boundaries.SignalingProtected, true, "boundaries.signalingProtected")
		r.assert(c.Boundaries.AutomaticOriginDiscovery, false, "boundaries.automaticOriginDiscovery")
	}
	evidence := r.inspectEvidence(c.Evidence, now, reader)
	for _, req := range []struct{ id, kind, field string }{
		{c.Edge.ProviderEvidenceID, "provider_coverage", "edge.providerEvidenceId"},
		{c.Edge.AssignmentEvidenceID, "edge_assignment", "edge.assignmentEvidenceId"},
		{c.Boundaries.ExposureEvidenceID, "exposure_review", "boundaries.exposureEvidenceId"},
	} {
		e, ok := evidence[req.id]
		if !ok || e.Kind != req.kind {
			r.add("unverified", "evidence_binding", req.field, "Reference fresh, digest-matched evidence of the required kind.")
		}
	}
	for _, f := range r.Findings {
		if f.Severity == "unverified" && r.Status != "blocked" {
			r.Status = "unverified"
		}
		if f.Severity == "error" {
			r.Status = "blocked"
		}
	}
	return r
}

func (r *Report) assert(value *bool, required bool, field string) {
	if value == nil {
		r.add("unverified", "boundary_unverified", field, "An explicit, evidence-backed assertion is required.")
	} else if *value != required {
		r.add("error", "unsafe_boundary", field, "The declared boundary does not satisfy the private-origin protection model.")
	}
}

func (r *Report) ports(ports []int, field string) map[int]bool {
	result := make(map[int]bool)
	if len(ports) > 8 {
		r.add("error", "port_limit", field, "Too many ports for one service profile.")
	}
	for _, port := range ports {
		if !portValid(port) || result[port] {
			r.add("error", "invalid_port", field, "Ports must be unique integers between 1 and 65535.")
		}
		result[port] = true
	}
	return result
}

func (r *Report) comparePorts(actual, wanted map[int]bool, field string) {
	for _, port := range sortedPorts(wanted) {
		if !actual[port] {
			r.add("error", "required_port_missing", field, fmt.Sprintf("Required port %d is missing for this transport.", port))
		}
	}
	for _, port := range sortedPorts(actual) {
		if !wanted[port] {
			r.add("error", "unexpected_exposure", field, fmt.Sprintf("Port %d is outside this single-service transport profile; audit auxiliary services separately.", port))
		}
	}
}

func sortedPorts(ports map[int]bool) []int {
	result := make([]int, 0, len(ports))
	for port := range ports {
		result = append(result, port)
	}
	sort.Ints(result)
	return result
}

func portValid(p int) bool { return p >= 1 && p <= 65535 }

func transportValid(t string) bool {
	return t == "java" || t == "nethernet" || t == "legacy" || t == "both"
}

func parseAddress(raw string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(raw)
	return a, err == nil && a.Zone() == "" && !a.Is4In6()
}

func privateOrigin(raw string) bool {
	a, ok := parseAddress(raw)
	return ok && (a.IsPrivate() || a.IsLoopback())
}

// Deliberately conservative special-use exclusions, not an allocation/ownership
// database. Exact edge assignment must still be supported by supplied evidence.
var excludedEdgePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

func publicEdge(raw string) (netip.Addr, bool) {
	a, ok := parseAddress(raw)
	if !ok || !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return a, false
	}
	// Limit IPv6 to the currently used global unicast block; this intentionally
	// excludes local translation, discard and unallocated future-use blocks.
	if a.Is6() && !netip.MustParsePrefix("2000::/3").Contains(a) {
		return a, false
	}
	for _, prefix := range excludedEdgePrefixes {
		if prefix.Contains(a) {
			return a, false
		}
	}
	return a, true
}
