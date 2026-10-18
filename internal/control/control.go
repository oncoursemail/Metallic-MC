package control

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/resources"
)

// The control plane is an intentionally minimal, single-process prototype.
// It has no database, scheduler, billing, or remote execution API.
//
//go:embed web/*
var web embed.FS

var validID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

type Config struct {
	AdminUser     string
	AdminPassword string
	AgentToken    string
	AIReadToken   string
	SecureCookies bool
}
type Instance struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Kind    string `json:"kind"`
	Healthy bool   `json:"healthy"`
}
type Heartbeat struct {
	NodeID              string     `json:"nodeId"`
	Hostname            string     `json:"hostname"`
	CPUPercent          float64    `json:"cpuPercent"`
	CPUThreads          uint32     `json:"cpuThreads"`
	CPUPressureSome10   float64    `json:"cpuPressureSome10"`
	PressureAvailable   bool       `json:"pressureAvailable"`
	MemPressureSome10   float64    `json:"memPressureSome10"`
	IOPressureSome10    float64    `json:"ioPressureSome10"`
	IOPressureAvailable bool       `json:"ioPressureAvailable"`
	LegacyWingsObserved bool       `json:"legacyWingsObserved"`
	LegacyWingsChecked  bool       `json:"legacyWingsChecked"`
	SwapTotal           uint64     `json:"swapTotal"`
	SwapUsed            uint64     `json:"swapUsed"`
	MemoryUsed          uint64     `json:"memoryUsed"`
	MemoryTotal         uint64     `json:"memoryTotal"`
	DiskUsed            uint64     `json:"diskUsed"`
	DiskTotal           uint64     `json:"diskTotal"`
	Load1               float64    `json:"load1"`
	UptimeSeconds       uint64     `json:"uptimeSeconds"`
	Instances           []Instance `json:"instances"`
}
type Sample struct {
	At         time.Time `json:"at"`
	CPUPercent float64   `json:"cpuPercent"`
}
type Node struct {
	Heartbeat
	LastSeen time.Time `json:"lastSeen"`
	Online   bool      `json:"online"`
	History  []Sample  `json:"history"`
}
type nodeRecord struct {
	heartbeat Heartbeat
	seen      time.Time
	history   []Sample
}
type session struct{ expiry time.Time }
type loginThrottle struct {
	since    time.Time
	attempts int
}
type Controller struct {
	cfg      Config
	mu       sync.RWMutex
	nodes    map[string]nodeRecord
	sessions map[string]session
	failed   map[string]loginThrottle
	now      func() time.Time
}

func New(cfg Config) http.Handler { return NewController(cfg).Handler() }
func NewController(cfg Config) *Controller {
	return &Controller{cfg: cfg, nodes: make(map[string]nodeRecord), sessions: make(map[string]session), failed: make(map[string]loginThrottle), now: time.Now}
}
func (c *Controller) Handler() http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(web, "web")
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /login", c.loginPage)
	mux.HandleFunc("POST /login", c.login)
	mux.HandleFunc("POST /logout", c.requireAdmin(c.logout))
	mux.HandleFunc("GET /", c.requireAdmin(c.index))
	mux.HandleFunc("GET /api/v1/overview", c.requireAdmin(c.overview))
	mux.HandleFunc("GET /api/v1/resources", c.requireAdmin(c.resourcePreview))
	mux.HandleFunc("GET /api/v1/ai/context", c.aiContext)
	mux.HandleFunc("POST /api/v1/heartbeat", c.heartbeat)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	})
}
func (c *Controller) loginPage(w http.ResponseWriter, r *http.Request) {
	c.serveFile(w, "web/login.html")
}
func (c *Controller) index(w http.ResponseWriter, r *http.Request) { c.serveFile(w, "web/index.html") }
func (c *Controller) serveFile(w http.ResponseWriter, path string) {
	b, err := web.ReadFile(path)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}
func (c *Controller) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	now := c.now()
	c.mu.Lock()
	t := c.failed[ip]
	if now.Sub(t.since) > 5*time.Minute {
		t = loginThrottle{since: now}
	}
	if t.attempts >= 5 {
		c.mu.Unlock()
		http.Error(w, "too many login attempts", http.StatusTooManyRequests)
		return
	}
	c.mu.Unlock()
	userOK := subtle.ConstantTimeCompare([]byte(r.Form.Get("username")), []byte(c.cfg.AdminUser)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(r.Form.Get("password")), []byte(c.cfg.AdminPassword)) == 1
	if !userOK || !passOK {
		c.mu.Lock()
		t.attempts++
		c.failed[ip] = t
		c.mu.Unlock()
		http.Redirect(w, r, "/login?error=1", http.StatusSeeOther)
		return
	}
	var seed [32]byte
	if _, err := rand.Read(seed[:]); err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	token := hex.EncodeToString(seed[:])
	c.mu.Lock()
	delete(c.failed, ip)
	c.sessions[token] = session{expiry: now.Add(8 * time.Hour)}
	c.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "metallic_session", Value: token, Path: "/", HttpOnly: true, Secure: c.cfg.SecureCookies, SameSite: http.SameSiteStrictMode, Expires: now.Add(8 * time.Hour)})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (c *Controller) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("metallic_session")
		if err != nil {
			c.deny(w, r)
			return
		}
		now := c.now()
		c.mu.RLock()
		s, ok := c.sessions[cookie.Value]
		c.mu.RUnlock()
		if !ok || !now.Before(s.expiry) {
			c.deny(w, r)
			return
		}
		next(w, r)
	}
}
func (c *Controller) deny(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
func (c *Controller) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("metallic_session")
	if err == nil {
		c.mu.Lock()
		delete(c.sessions, cookie.Value)
		c.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "metallic_session", Path: "/", Value: "", MaxAge: -1, HttpOnly: true, Secure: c.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
func (c *Controller) heartbeat(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" || !secureEqual(tok, c.cfg.AgentToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var hb Heartbeat
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&hb); err != nil {
		http.Error(w, "invalid heartbeat", http.StatusBadRequest)
		return
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		http.Error(w, "trailing data", http.StatusBadRequest)
		return
	}
	if !validHeartbeat(hb) {
		http.Error(w, "invalid metrics", http.StatusBadRequest)
		return
	}
	now := c.now()
	c.mu.Lock()
	entry := c.nodes[hb.NodeID]
	entry.heartbeat = hb
	entry.seen = now
	entry.history = append(entry.history, Sample{At: now, CPUPercent: hb.CPUPercent})
	if len(entry.history) > 60 {
		entry.history = entry.history[len(entry.history)-60:]
	}
	c.nodes[hb.NodeID] = entry
	c.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
func secureEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
func validHeartbeat(h Heartbeat) bool {
	if !validID.MatchString(h.NodeID) || len(h.Hostname) == 0 || len(h.Hostname) > 128 {
		return false
	}
	if !okPercent(h.CPUPercent) || !okMetric(h.Load1) || !okPercent(h.CPUPressureSome10) || !okPercent(h.MemPressureSome10) || !okPercent(h.IOPressureSome10) || h.CPUThreads > 512 || h.MemoryUsed > h.MemoryTotal || h.DiskUsed > h.DiskTotal || h.SwapUsed > h.SwapTotal || len(h.Instances) > 64 {
		return false
	}
	for _, i := range h.Instances {
		if len(i.Name) == 0 || len(i.Name) > 64 || len(i.Address) > 128 || len(i.Kind) > 40 {
			return false
		}
	}
	return true
}
func okPercent(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 100 }
func okMetric(v float64) bool  { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 10000 }
func (c *Controller) overview(w http.ResponseWriter, r *http.Request) {
	now := c.now()
	nodes := make([]Node, 0)
	c.mu.RLock()
	for _, v := range c.nodes {
		if now.Sub(v.seen) > 24*time.Hour {
			continue
		}
		history := append([]Sample(nil), v.history...)
		nodes = append(nodes, Node{Heartbeat: v.heartbeat, LastSeen: v.seen, Online: now.Sub(v.seen) < 35*time.Second, History: history})
	}
	c.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Product    string    `json:"product"`
		ServerTime time.Time `json:"serverTime"`
		Nodes      []Node    `json:"nodes"`
		Stage      string    `json:"stage"`
	}{Product: "Metallic Control", ServerTime: now, Nodes: nodes, Stage: "foundation"})
}

// snapshotHosts converts authenticated live agent reports to advisory capacity
// estimates. Agents report raw metrics; no agent can request a privileged action.
func (c *Controller) snapshotHosts() []resources.Host {
	now := c.now()
	c.mu.RLock()
	defer c.mu.RUnlock()
	hosts := make([]resources.Host, 0, len(c.nodes))
	for id, entry := range c.nodes {
		if now.Sub(entry.seen) > 24*time.Hour {
			continue
		}
		h := entry.heartbeat
		hosts = append(hosts, resources.Host{
			ID: id, Online: now.Sub(entry.seen) >= 0 && now.Sub(entry.seen) < 35*time.Second,
			CPUThreads: h.CPUThreads, CPUPercent: h.CPUPercent, Load1: h.Load1,
			CPUPressureSome10: h.CPUPressureSome10, MemPressureSome10: h.MemPressureSome10, PressureAvailable: h.PressureAvailable, IOPressureSome10: h.IOPressureSome10, IOPressureAvailable: h.IOPressureAvailable,
			MemoryTotal: h.MemoryTotal, MemoryUsed: h.MemoryUsed,
			DiskTotal: h.DiskTotal, DiskUsed: h.DiskUsed,
			SwapTotal: h.SwapTotal, SwapUsed: h.SwapUsed,
		})
	}
	return hosts
}
func (c *Controller) resourcePreview(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("profile")
	if id == "" {
		id = "mc-lobby"
	}
	profile, ok := resources.FindProfile(id)
	if !ok {
		http.Error(w, "unknown approved profile", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Profiles []resources.Profile `json:"profiles"`
		Preview  resources.Preview   `json:"preview"`
	}{resources.Profiles(), resources.Compute(c.snapshotHosts(), profile)})
}

// Star AI receives a separately-scoped, read-only, deliberately redacted view.
// It cannot start servers, execute commands, read player data, or obtain agent
// tokens. No AI integration exists unless AIReadToken is explicitly configured.
func (c *Controller) aiContext(w http.ResponseWriter, r *http.Request) {
	if len(c.cfg.AIReadToken) < 24 {
		http.NotFound(w, r)
		return
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || !secureEqual(strings.TrimPrefix(header, "Bearer "), c.cfg.AIReadToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	type aiNode struct {
		NodeID               string  `json:"nodeId"`
		Online               bool    `json:"online"`
		CPUPercent           float64 `json:"cpuPercent"`
		CPUThreads           uint32  `json:"cpuThreads"`
		AvailableMemoryBytes uint64  `json:"availableMemoryBytes"`
		CPUPressureSome10    float64 `json:"cpuPressureSome10"`
	}
	hosts := c.snapshotHosts()
	nodes := make([]aiNode, 0, len(hosts))
	for _, n := range hosts {
		var avail uint64
		if n.MemoryTotal >= n.MemoryUsed {
			avail = n.MemoryTotal - n.MemoryUsed
		}
		nodes = append(nodes, aiNode{NodeID: n.ID, Online: n.Online, CPUPercent: n.CPUPercent, CPUThreads: n.CPUThreads, AvailableMemoryBytes: avail, CPUPressureSome10: n.CPUPressureSome10})
	}
	profile, _ := resources.FindProfile("mc-lobby")
	preview := resources.Compute(hosts, profile)
	// RecommendedNode is a what-if suggestion, not authorization to provision.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Stage                 string            `json:"stage"`
		ServerTime            time.Time         `json:"serverTime"`
		ReadOnly              bool              `json:"readOnly"`
		DeploymentImplemented bool              `json:"deploymentImplemented"`
		Nodes                 []aiNode          `json:"nodes"`
		ExampleAdmission      resources.Preview `json:"exampleAdmission"`
	}{Stage: "prototype", ServerTime: c.now(), ReadOnly: true, DeploymentImplemented: false, Nodes: nodes, ExampleAdmission: preview})
}
