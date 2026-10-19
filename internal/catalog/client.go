package catalog

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxResponseBytes int64 = 2 << 20
	maxArtifactBytes int64 = 128 << 20
	requestTimeout         = 8 * time.Second
)

type Client struct {
	httpClient *http.Client
	userAgent  string
	semaphore  chan struct{}
}

func NewClient(cfg Config) (*Client, error) {
	ua := strings.TrimSpace(cfg.UserAgent)
	if len(ua) < 12 || len(ua) > 256 || strings.ContainsAny(ua, "\r\n") || !strings.Contains(ua, "/") || (!strings.Contains(ua, "https://") && !strings.Contains(ua, "@")) {
		return nil, errors.New("catalog user agent must contain a product/version and an HTTPS contact URL or email")
	}
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		MaxResponseHeaderBytes: 32 << 10, MaxIdleConns: 8, MaxIdleConnsPerHost: 4, MaxConnsPerHost: 4,
		IdleConnTimeout: 30 * time.Second, DisableCompression: true,
	}
	return &Client{userAgent: ua, semaphore: make(chan struct{}, 4), httpClient: &http.Client{Transport: transport, Timeout: requestTimeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func validateTarget(t Target) error {
	if t.Platform != "paper" && t.Platform != "folia" {
		return errors.New("target platform must be paper or folia")
	}
	if len(t.MinecraftVersion) == 0 || len(t.MinecraftVersion) > 64 {
		return errors.New("an exact Minecraft version is required")
	}
	for _, r := range t.MinecraftVersion {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
			return errors.New("invalid Minecraft version")
		}
	}
	return nil
}
func validID(s string) bool {
	if len(s) == 0 || len(s) > 160 || !utf8.ValidString(s) || strings.TrimSpace(s) != s || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || strings.ContainsRune("/\\%?#", r) {
			return false
		}
	}
	return true
}
func validateProvider(p Provider) error {
	if p != Hangar && p != Modrinth {
		return errors.New("provider must be hangar or modrinth")
	}
	return nil
}
func (c *Client) Search(ctx context.Context, req SearchRequest) (SearchResult, error) {
	out := SearchResult{Provider: req.Provider, Hits: []SearchHit{}}
	if err := validateProvider(req.Provider); err != nil {
		return out, err
	}
	if err := validateTarget(req.Target); err != nil {
		return out, err
	}
	if len(req.Query) > 200 || !utf8.ValidString(req.Query) {
		return out, errors.New("query must contain at most 200 UTF-8 bytes")
	}
	for _, r := range req.Query {
		if unicode.IsControl(r) {
			return out, errors.New("query contains control characters")
		}
	}
	if req.Limit == 0 {
		req.Limit = 10
	}
	if req.Limit < 1 || req.Limit > 25 {
		return out, errors.New("limit must be between 1 and 25")
	}
	if req.Provider == Hangar {
		return c.searchHangar(ctx, req)
	}
	return c.searchModrinth(ctx, req)
}
func (c *Client) Resolve(ctx context.Context, req ResolveRequest) (Plan, error) {
	p := Plan{Provider: req.Provider, ProjectID: req.ProjectID, VersionID: req.VersionID, Target: req.Target, Dependencies: []Dependency{}, Blockers: []Blocker{}}
	if err := validateProvider(req.Provider); err != nil {
		return p, err
	}
	if err := validateTarget(req.Target); err != nil {
		return p, err
	}
	if !validID(req.VersionID) {
		return p, errors.New("an exact, valid version ID or Hangar version name is required")
	}
	if (req.Provider == Hangar || req.ProjectID != "") && !validID(req.ProjectID) {
		return p, errors.New("invalid project ID")
	}
	if req.Provider == Hangar {
		return c.resolveHangar(ctx, req, p)
	}
	return c.resolveModrinth(ctx, req, p)
}

// get has no public endpoint override. Upstream metadata cannot supply a request URL.
func (c *Client) get(ctx context.Context, provider Provider, apiPath string, query url.Values, dst any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	select {
	case c.semaphore <- struct{}{}:
		defer func() { <-c.semaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}
	host := "api.modrinth.com"
	if provider == Hangar {
		host = "hangar.papermc.io"
	}
	endpoint := "https://" + host + apiPath
	if len(query) != 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return &UpstreamError{Provider: provider, Reason: "invalid API request"}
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &UpstreamError{Provider: provider, Reason: "request failed"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &UpstreamError{Provider: provider, Status: resp.StatusCode, Reason: "upstream did not return success; redirects are disabled"}
	}
	if resp.ContentLength > maxResponseBytes {
		return &UpstreamError{Provider: provider, Reason: "response exceeds size limit"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return &UpstreamError{Provider: provider, Reason: "response could not be read"}
	}
	if int64(len(body)) > maxResponseBytes {
		return &UpstreamError{Provider: provider, Reason: "response exceeds size limit"}
	}
	if len(body) == 0 || string(body) == "null" {
		return &UpstreamError{Provider: provider, Reason: "empty JSON response"}
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return &UpstreamError{Provider: provider, Reason: "invalid JSON response"}
	}
	return nil
}
func has(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
func validHash(s string, bytes int) bool {
	if len(s) != bytes*2 {
		return false
	}
	decoded, err := hex.DecodeString(s)
	return err == nil && len(decoded) == bytes
}
func trustedArtifactURL(provider Provider, raw string) bool {
	if len(raw) == 0 || len(raw) > 2048 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Port() != "" || u.Host != u.Hostname() || u.Path == "" {
		return false
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	if provider == Modrinth {
		return u.Host == "cdn.modrinth.com" && strings.HasPrefix(u.Path, "/data/")
	}
	return provider == Hangar && u.Host == "hangarcdn.papermc.io" && strings.HasPrefix(u.Path, "/plugins/")
}
func (p *Plan) setArtifact(a Artifact) {
	before := len(p.Blockers)
	if !trustedArtifactURL(p.Provider, a.URL) {
		p.block("artifact_url_untrusted", "Artifact must use the provider's approved HTTPS download origin and path.")
	}
	if len(a.Filename) == 0 || len(a.Filename) > 255 || path.Base(a.Filename) != a.Filename || strings.ContainsAny(a.Filename, "\\\x00\r\n") || !strings.HasSuffix(strings.ToLower(a.Filename), ".jar") {
		p.block("artifact_name_invalid", "Artifact filename must be a plain JAR filename.")
	}
	for _, r := range a.Filename {
		if unicode.IsControl(r) {
			p.block("artifact_name_invalid", "Artifact filename contains control characters.")
			break
		}
	}
	if a.Size <= 0 || a.Size > maxArtifactBytes {
		p.block("artifact_size_invalid", fmt.Sprintf("Artifact size must be between 1 and %d bytes.", maxArtifactBytes))
	}
	if p.Provider == Hangar && !validHash(a.SHA256, 32) || p.Provider == Modrinth && !validHash(a.SHA512, 64) {
		p.block("artifact_hash_invalid", "Artifact is missing a valid provider-supplied strong hash.")
	}
	if a.SHA256 != "" && !validHash(a.SHA256, 32) || a.SHA512 != "" && !validHash(a.SHA512, 64) {
		p.block("artifact_hash_invalid", "Artifact contains a malformed hash.")
	}
	if len(p.Blockers) == before {
		a.SHA256 = strings.ToLower(a.SHA256)
		a.SHA512 = strings.ToLower(a.SHA512)
		p.Artifact = &a
	}
}
