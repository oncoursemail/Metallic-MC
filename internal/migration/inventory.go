// Package migration holds the read-only inventory bridge for the "Old Servers"
// Pterodactyl/Wings deployment. It only issues GET requests to the documented
// panel client API, never talks to Docker, and keeps keys, addresses and
// free-text fields out of its output.
//
// Credential scope: a panel client API key owned by an account that can only
// view the servers to inventory; no write permission is needed or used.
// Staleness: an Inventory is a point-in-time snapshot; use Stale before relying
// on it for a cutover decision.
package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const maxBodyBytes int64 = 4 << 20

// Server is redacted metadata for one panel server. IP addresses, aliases,
// descriptions and player data are intentionally not represented.
type Server struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Node     string `json:"node"`
	Ports    []int  `json:"ports"`
	MemoryMB int64  `json:"memoryMb"`
	DiskMB   int64  `json:"diskMb"`
	CPUPct   int64  `json:"cpuPercent"`
}

// Inventory is a point-in-time snapshot.
type Inventory struct {
	CollectedAt time.Time `json:"collectedAt"`
	Servers     []Server  `json:"servers"`
}

// Stale reports whether the snapshot is older than maxAge at time now.
func (i Inventory) Stale(now time.Time, maxAge time.Duration) bool {
	return now.Sub(i.CollectedAt) > maxAge
}

type InventoryClient struct {
	base *url.URL
	key  string
	http *http.Client
	now  func() time.Time
}

// String never reveals the key.
func (c *InventoryClient) String() string { return "InventoryClient{key: redacted}" }

// GoString never reveals the key.
func (c *InventoryClient) GoString() string { return c.String() }

type readOnlyTransport struct{ next http.RoundTripper }

func (t readOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet {
		return nil, errors.New("migration inventory is read-only: only GET is allowed")
	}
	return t.next.RoundTrip(r)
}

func NewInventoryClient(panelURL, apiKey string) (*InventoryClient, error) {
	u, err := url.Parse(strings.TrimSpace(panelURL))
	if err != nil || u.Host == "" || u.User != nil {
		return nil, errors.New("invalid panel URL")
	}
	loopback := false
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, errors.New("panel URL must use https")
	}
	if strings.TrimSpace(apiKey) == "" || strings.ContainsAny(apiKey, "\r\n ") {
		return nil, errors.New("a panel API key is required")
	}
	return &InventoryClient{base: u, key: apiKey, now: time.Now, http: &http.Client{
		Timeout:       10 * time.Second,
		Transport:     readOnlyTransport{next: &http.Transport{Proxy: http.ProxyFromEnvironment}},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

type panelList struct {
	Data []struct {
		Attributes struct {
			Identifier string `json:"identifier"`
			Name       string `json:"name"`
			Node       string `json:"node"`
			Limits     struct {
				Memory int64 `json:"memory"`
				Disk   int64 `json:"disk"`
				CPU    int64 `json:"cpu"`
			} `json:"limits"`
			Relationships struct {
				Allocations struct {
					Data []struct {
						Attributes struct {
							Port int `json:"port"`
						} `json:"attributes"`
					} `json:"data"`
				} `json:"allocations"`
			} `json:"relationships"`
		} `json:"attributes"`
	} `json:"data"`
}

// Collect lists servers with a single GET to /api/client. Errors never include
// the key or the response body.
func (c *InventoryClient) Collect(ctx context.Context) (Inventory, error) {
	endpoint := c.base.JoinPath("api", "client")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return Inventory{}, errors.New("cannot build panel request")
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Inventory{}, errors.New("panel request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Inventory{}, fmt.Errorf("panel returned status %d", resp.StatusCode)
	}
	var list panelList
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&list); err != nil {
		return Inventory{}, errors.New("panel response is not valid inventory JSON")
	}
	inv := Inventory{CollectedAt: c.now().UTC(), Servers: []Server{}}
	for _, d := range list.Data {
		a := d.Attributes
		s := Server{ID: a.Identifier, Name: a.Name, Node: a.Node, Ports: []int{},
			MemoryMB: a.Limits.Memory, DiskMB: a.Limits.Disk, CPUPct: a.Limits.CPU}
		for _, al := range a.Relationships.Allocations.Data {
			s.Ports = append(s.Ports, al.Attributes.Port)
		}
		sort.Ints(s.Ports)
		inv.Servers = append(inv.Servers, s)
	}
	return inv, nil
}
