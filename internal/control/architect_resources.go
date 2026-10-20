package control

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/catalog"
	"github.com/oncoursemail/metallic-mc/internal/hosting"
)

// All owner IDs come from the current private administrator, never request JSON.
// Public enrollment needs durable multi-account identity before these routes can
// leave the private controller.
func (c *Controller) registerArchitectResources(mux *http.ServeMux) {
	mux.HandleFunc("GET "+architectPrefix+"hosts", c.requireAdmin(c.architectHosts))
	mux.HandleFunc("GET "+architectPrefix+"servers", c.requireAdmin(c.architectServers))
	mux.HandleFunc("POST "+architectPrefix+"hosts/pairing", c.requireAdmin(c.architectMutation(c.architectPairing, true)))
	mux.HandleFunc("DELETE "+architectPrefix+"hosts/{hostID}", c.requireAdmin(c.architectMutation(c.architectRevokeHost, true)))
	mux.HandleFunc("POST "+architectPrefix+"servers", c.requireAdmin(c.architectMutation(c.architectAllocate, true)))
	// Pairing tickets and individual host credentials authorize machine calls.
	// The pre-existing shared telemetry token grants no access here.
	mux.HandleFunc("POST "+architectPrefix+"hosts/redeem", c.architectRedeem)
	mux.HandleFunc("POST "+architectPrefix+"routes/plan", c.architectRoute)
	mux.HandleFunc("GET "+architectPrefix+"catalog/search", c.requireAdmin(c.architectCatalogSearch))
	mux.HandleFunc("POST "+architectPrefix+"catalog/resolve", c.requireAdmin(c.architectMutation(c.architectCatalogResolve, false)))
}

func (c *Controller) architectHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := c.hosts.ListHosts(c.cfg.AdminUser)
	if err != nil {
		hostingError(w, err)
		return
	}
	jsonReply(w, http.StatusOK, map[string]any{"hosts": hosts, "persistent": c.hosts.Persistent()})
}
func (c *Controller) architectServers(w http.ResponseWriter, r *http.Request) {
	servers, err := c.hosts.ListServers(c.cfg.AdminUser)
	if err != nil {
		hostingError(w, err)
		return
	}
	jsonReply(w, http.StatusOK, map[string]any{"servers": servers, "execution_enabled": false})
}
func (c *Controller) architectPairing(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !decodeArchitectJSON(w, r, &input) {
		return
	}
	ticket, err := c.hosts.IssuePairingTicket(c.cfg.AdminUser)
	if err != nil {
		hostingError(w, err)
		return
	}
	jsonReply(w, http.StatusCreated, ticket)
}
func (c *Controller) architectRedeem(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Ticket   string `json:"ticket"`
		HostName string `json:"host_name"`
	}
	if !decodeArchitectJSON(w, r, &input) {
		return
	}
	result, err := c.hosts.RedeemPairingTicket(c.cfg.AdminUser, input.Ticket, input.HostName)
	if err != nil {
		hostingError(w, err)
		return
	}
	jsonReply(w, http.StatusCreated, result)
}
func (c *Controller) architectRevokeHost(w http.ResponseWriter, r *http.Request) {
	if err := c.hosts.RevokeHost(c.cfg.AdminUser, r.PathValue("hostID")); err != nil {
		hostingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (c *Controller) architectAllocate(w http.ResponseWriter, r *http.Request) {
	var input hosting.AllocationRequest
	if !decodeArchitectJSON(w, r, &input) {
		return
	}
	result, err := c.hosts.AllocateServer(c.cfg.AdminUser, input)
	if err != nil {
		hostingError(w, err)
		return
	}
	jsonReply(w, http.StatusCreated, result)
}
func (c *Controller) architectRoute(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || len(auth) > 1024 || len(strings.Fields(auth)) != 2 {
		http.Error(w, "host credential required", http.StatusUnauthorized)
		return
	}
	var input hosting.RouteRequest
	if !decodeArchitectJSON(w, r, &input) {
		return
	}
	result, err := c.hosts.PlanRoute(c.cfg.AdminUser, strings.TrimPrefix(auth, "Bearer "), input)
	if err != nil {
		hostingError(w, err)
		return
	}
	jsonReply(w, http.StatusOK, result)
}
func hostingError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, hosting.ErrStore):
		status = http.StatusServiceUnavailable
	case errors.Is(err, hosting.ErrUnauthorized), errors.Is(err, hosting.ErrExpired), errors.Is(err, hosting.ErrRevoked):
		status = http.StatusUnauthorized
	case errors.Is(err, hosting.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, hosting.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, hosting.ErrLimitExceeded):
		status = http.StatusTooManyRequests
	}
	http.Error(w, "host policy rejected this request", status)
}
func (c *Controller) architectCatalogSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	input := catalog.SearchRequest{Provider: catalog.Provider(q.Get("provider")), Query: q.Get("query"), Target: catalog.Target{MinecraftVersion: q.Get("minecraft_version"), Platform: q.Get("platform")}, Limit: 10}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	result, err := c.catalog.Search(ctx, input)
	if err != nil {
		http.Error(w, "catalog lookup unavailable or request invalid", http.StatusBadGateway)
		return
	}
	jsonReply(w, http.StatusOK, result)
}
func (c *Controller) architectCatalogResolve(w http.ResponseWriter, r *http.Request) {
	var input catalog.ResolveRequest
	if !decodeArchitectJSON(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	result, err := c.catalog.Resolve(ctx, input)
	if err != nil {
		http.Error(w, "catalog release unavailable or request invalid", http.StatusBadGateway)
		return
	}
	jsonReply(w, http.StatusOK, result)
}
