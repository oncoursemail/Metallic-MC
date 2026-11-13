// Package oldservers provides a read-only inventory of legacy Wings servers
// ("Old Servers") and a gate for any later migration or deletion.
//
// The package never contacts Wings itself and never holds credentials: the
// caller supplies a Source that is built and authenticated elsewhere. Only an
// allowlist of non-secret metadata is copied out of each record; everything
// else (environment, startup commands, tokens) is dropped. Everything is
// disabled by default.
package oldservers

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrDisabled       = errors.New("oldservers: inventory is disabled")
	ErrActionDisabled = errors.New("oldservers: action is disabled")
	ErrGateNotMet     = errors.New("oldservers: safety gate not met")
)

// Config holds explicit switches. The zero value disables everything. It has
// no credential fields by design.
type Config struct {
	Enabled        bool
	AllowMigration bool
	AllowDeletion  bool
}

// RawServer is an untrusted record from a Source.
type RawServer map[string]any

// Source lists raw legacy server records read-only.
type Source interface {
	List(ctx context.Context) ([]RawServer, error)
}

// Server is the only metadata that leaves the adapter.
type Server struct {
	ID         string
	Name       string
	Node       string
	MemoryMB   int
	DiskMB     int
	CPUPercent int
	Ports      []int
	Software   string
}

type Adapter struct {
	cfg Config
	src Source
}

func New(cfg Config, src Source) *Adapter { return &Adapter{cfg: cfg, src: src} }

// Inventory returns allowlisted metadata. It does nothing unless enabled.
func (a *Adapter) Inventory(ctx context.Context) ([]Server, error) {
	if !a.cfg.Enabled || a.src == nil {
		return nil, ErrDisabled
	}
	raw, err := a.src.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("oldservers: list: %w", err)
	}
	out := make([]Server, 0, len(raw))
	for _, r := range raw {
		out = append(out, Server{
			ID:         text(r["uuid"]),
			Name:       text(r["name"]),
			Node:       text(r["node"]),
			MemoryMB:   number(r["memory_mb"]),
			DiskMB:     number(r["disk_mb"]),
			CPUPercent: number(r["cpu_percent"]),
			Ports:      ports(r["ports"]),
			Software:   text(r["software"]),
		})
	}
	return out, nil
}

func text(v any) string {
	s, ok := v.(string)
	if !ok || len(s) > 128 {
		return ""
	}
	l := strings.ToLower(s)
	for _, w := range []string{"token", "secret", "password", "passwd", "apikey", "api_key", "bearer", "key="} {
		if strings.Contains(l, w) {
			return ""
		}
	}
	return s
}

func number(v any) int {
	switch n := v.(type) {
	case int:
		if n >= 0 {
			return n
		}
	case float64:
		if n >= 0 && n < 1<<31 {
			return int(n)
		}
	}
	return 0
}

func ports(v any) []int {
	l, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []int
	for _, p := range l {
		if n := number(p); n > 0 && n < 65536 {
			out = append(out, n)
		}
	}
	return out
}

type Action string

const (
	Migrate Action = "migrate"
	Delete  Action = "delete"
)

// Plan records evidence for the safety gates. It only carries booleans and an
// approver label, never data.
type Plan struct {
	InventoryRecorded bool
	BackupVerified    bool
	ParityPassed      bool
	RollbackTested    bool
	ApprovedBy        string
}

// Authorize is a pure decision: it performs nothing. Both the action's own
// switch and every gate must be satisfied.
func Authorize(cfg Config, p Plan, act Action) error {
	switch act {
	case Migrate:
		if !cfg.Enabled || !cfg.AllowMigration {
			return ErrActionDisabled
		}
	case Delete:
		if !cfg.Enabled || !cfg.AllowDeletion {
			return ErrActionDisabled
		}
	default:
		return ErrActionDisabled
	}
	var missing []string
	if !p.InventoryRecorded {
		missing = append(missing, "inventory")
	}
	if !p.BackupVerified {
		missing = append(missing, "verified backup")
	}
	if !p.ParityPassed {
		missing = append(missing, "parity smoke test")
	}
	if !p.RollbackTested {
		missing = append(missing, "rollback")
	}
	if strings.TrimSpace(p.ApprovedBy) == "" {
		missing = append(missing, "approval")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: missing %s", ErrGateNotMet, strings.Join(missing, ", "))
	}
	return nil
}
