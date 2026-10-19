package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}
func testClient(t *testing.T, f roundTripFunc) *Client {
	t.Helper()
	c, e := NewClient(Config{UserAgent: "Metallic/0.1 (+https://example.com/metallic)"})
	if e != nil {
		t.Fatal(e)
	}
	c.httpClient.Transport = f
	return c
}
func paperTarget() Target { return Target{MinecraftVersion: "1.21.4", Platform: "paper"} }
func searchRequest(p Provider) SearchRequest {
	return SearchRequest{Provider: p, Query: "tiny & safe?", Target: paperTarget(), Limit: 25}
}
func hasBlock(p Plan, code string) bool {
	for _, b := range p.Blockers {
		if b.Code == code {
			return true
		}
	}
	return false
}
func TestUserAgent(t *testing.T) {
	for _, ua := range []string{"", "Go-http-client/1.1", "Metallic/1.0\r\nBad: yes (+https://example.com)"} {
		if _, e := NewClient(Config{UserAgent: ua}); e == nil {
			t.Errorf("accepted UA %q", ua)
		}
	}
}
func TestSearchFiltersAndEncodedQuery(t *testing.T) {
	for _, provider := range []Provider{Hangar, Modrinth} {
		t.Run(string(provider), func(t *testing.T) {
			c := testClient(t, func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.Header.Get("User-Agent") != "Metallic/0.1 (+https://example.com/metallic)" {
					t.Error("unexpected request")
				}
				q := r.URL.Query()
				if q.Get("query") != "tiny & safe?" || q.Get("limit") != "25" {
					t.Error("query round trip")
				}
				if provider == Hangar {
					if r.URL.Host != "hangar.papermc.io" || r.URL.Path != "/api/v1/projects" || q.Get("platform") != "PAPER" || q.Get("version") != "1.21.4" {
						t.Error("Hangar filters")
					}
					return jsonResponse(200, `{"result":[{"name":"Tiny Plugin","description":"A small plugin","namespace":{"owner":"Author","slug":"TinyPlugin"}}],"pagination":{"count":1,"limit":25,"offset":0}}`), nil
				}
				if r.URL.Host != "api.modrinth.com" || r.URL.Path != "/v2/search" {
					t.Error("Modrinth endpoint")
				}
				var facets [][]string
				if e := json.Unmarshal([]byte(q.Get("facets")), &facets); e != nil || len(facets) != 3 || facets[0][0] != "all_project_types:plugin" || facets[1][0] != "categories:paper" || facets[2][0] != "versions:1.21.4" {
					t.Errorf("facets %s", q.Get("facets"))
				}
				return jsonResponse(200, `{"hits":[{"project_id":"project42","slug":"tiny-plugin","title":"Tiny Plugin","description":"A small plugin","author":"Author","project_type":"mod","categories":["paper","utility"]}],"offset":0,"limit":25,"total_hits":1}`), nil
			})
			out, e := c.Search(context.Background(), searchRequest(provider))
			if e != nil || len(out.Hits) != 1 || out.Hits[0].Provider != provider {
				t.Fatalf("search %+v %v", out, e)
			}
		})
	}
}
func TestUpstreamFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body      string
		status          int
		oversizedLength bool
	}{
		{"redirect", `{}`, 302, false}, {"rate_limit", `{}`, 429, false}, {"malformed", `{"hits":`, 200, false}, {"wrong_shape", `{"hits":"wrong"}`, 200, false}, {"missing_array", `{}`, 200, false}, {"null", ` null `, 200, false}, {"trailing_json", `{"hits":[]} {}`, 200, false}, {"body_limit", strings.Repeat(" ", int(maxResponseBytes)+1), 200, false}, {"length_limit", `{}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(r *http.Request) (*http.Response, error) {
				calls++
				resp := jsonResponse(tc.status, tc.body)
				resp.Header.Set("Location", "http://127.0.0.1/private")
				resp.ContentLength = -1
				if tc.oversizedLength {
					resp.ContentLength = maxResponseBytes + 1
				}
				return resp, nil
			})
			if _, e := c.Search(context.Background(), searchRequest(Modrinth)); e == nil {
				t.Fatal("accepted invalid upstream")
			}
			if calls != 1 {
				t.Fatalf("unexpected retry/redirect %d", calls)
			}
		})
	}
}
func TestInputValidationPreventsRequests(t *testing.T) {
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Error("invalid input reached network")
		return jsonResponse(500, `{}`), nil
	})
	for _, r := range []SearchRequest{{Provider: "unknown", Target: paperTarget()}, {Provider: Modrinth, Target: Target{Platform: "paper"}}, {Provider: Modrinth, Target: paperTarget(), Limit: 26}, {Provider: Modrinth, Target: paperTarget(), Query: strings.Repeat("q", 201)}, {Provider: Modrinth, Target: paperTarget(), Query: "a\nb"}} {
		if _, e := c.Search(context.Background(), r); e == nil {
			t.Error("invalid search accepted")
		}
	}
	for _, id := range []string{"", "..", "../version", "%2fescape", "a?query", "https://evil.example/x"} {
		if _, e := c.Resolve(context.Background(), ResolveRequest{Provider: Hangar, ProjectID: "plugin", VersionID: id, Target: paperTarget()}); e == nil {
			t.Errorf("ID %q accepted", id)
		}
	}
}
func TestConcurrencyBoundAndCancellation(t *testing.T) {
	var active, peak atomic.Int32
	var wg sync.WaitGroup
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return jsonResponse(200, `{"hits":[]}`), nil
	})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := c.Search(context.Background(), searchRequest(Modrinth)); e != nil {
				t.Error(e)
			}
		}()
	}
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(release)
			wg.Wait()
			t.Fatal("requests did not enter")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.Search(ctx, searchRequest(Modrinth)); !errors.Is(e, context.Canceled) {
		t.Errorf("canceled: %v", e)
	}
	select {
	case <-entered:
		t.Error("over four concurrent requests")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	if peak.Load() > 4 {
		t.Errorf("peak %d", peak.Load())
	}
}
func TestArtifactOriginBoundaries(t *testing.T) {
	for _, tc := range []struct {
		p   Provider
		raw string
		ok  bool
	}{
		{Modrinth, "https://cdn.modrinth.com/data/p/versions/v/a.jar", true}, {Hangar, "https://hangarcdn.papermc.io/plugins/A/P/versions/v/PAPER/a.jar", true},
		{Modrinth, "http://cdn.modrinth.com/data/p/a.jar", false}, {Modrinth, "https://cdn.modrinth.com.evil.example/data/p/a.jar", false}, {Modrinth, "https://cdn.modrinth.com@evil.example/data/p/a.jar", false}, {Modrinth, "https://cdn.modrinth.com:443/data/p/a.jar", false}, {Modrinth, "https://cdn.modrinth.com/data/%2e%2e/private.jar", false}, {Modrinth, "https://cdn.modrinth.com/data/p/a.jar#fragment", false}, {Modrinth, "https://127.0.0.1/data/p/a.jar", false}, {Hangar, "https://example.com/a.jar", false}, {Hangar, "https://hangar.papermc.io/api/v1/projects/A/versions/1/PAPER/download", false},
	} {
		if got := trustedArtifactURL(tc.p, tc.raw); got != tc.ok {
			t.Errorf("URL %q: %v want %v", tc.raw, got, tc.ok)
		}
	}
}
