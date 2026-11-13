package oldservers

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeSource struct{ calls int }

func (f *fakeSource) List(context.Context) ([]RawServer, error) {
	f.calls++
	return []RawServer{{
		"uuid": "abc-1", "name": "survival", "node": "n1", "memory_mb": 4096, "disk_mb": 10000,
		"cpu_percent": 200, "ports": []any{25565, 25566}, "software": "paper",
		"env": map[string]any{"TOKEN": "s3cret"}, "api_key": "s3cret", "password": "s3cret",
		"startup": "java -jar x.jar --token s3cret",
	}}, nil
}

type sourceFunc func() []RawServer

func (f sourceFunc) List(context.Context) ([]RawServer, error) { return f(), nil }

func TestDisabledByDefault(t *testing.T) {
	src := &fakeSource{}
	a := New(Config{}, src)
	if _, err := a.Inventory(context.Background()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
	if src.calls != 0 {
		t.Fatal("source must not be contacted while disabled")
	}
}

func TestInventoryKeepsOnlyNonSecretMetadata(t *testing.T) {
	a := New(Config{Enabled: true}, &fakeSource{})
	got, err := a.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Server{{ID: "abc-1", Name: "survival", Node: "n1", MemoryMB: 4096, DiskMB: 10000, CPUPercent: 200, Ports: []int{25565, 25566}, Software: "paper"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestInventoryDropsSecretLookingValues(t *testing.T) {
	src := sourceFunc(func() []RawServer {
		return []RawServer{{"uuid": "a", "name": "token=abcdef", "node": "n", "software": "paper"}}
	})
	got, err := New(Config{Enabled: true}, src).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "" {
		t.Fatalf("secret-looking name kept: %q", got[0].Name)
	}
}

func TestActionsDisabledByDefault(t *testing.T) {
	full := Plan{InventoryRecorded: true, BackupVerified: true, ParityPassed: true, RollbackTested: true, ApprovedBy: "owner"}
	for _, act := range []Action{Migrate, Delete} {
		if err := Authorize(Config{}, full, act); !errors.Is(err, ErrActionDisabled) {
			t.Fatalf("%s: want ErrActionDisabled, got %v", act, err)
		}
	}
}

func TestAuthorizeRequiresEveryGate(t *testing.T) {
	cfg := Config{Enabled: true, AllowMigration: true, AllowDeletion: true}
	full := Plan{InventoryRecorded: true, BackupVerified: true, ParityPassed: true, RollbackTested: true, ApprovedBy: "owner"}
	if err := Authorize(cfg, full, Delete); err != nil {
		t.Fatal(err)
	}
	for _, mut := range []func(*Plan){
		func(p *Plan) { p.InventoryRecorded = false },
		func(p *Plan) { p.BackupVerified = false },
		func(p *Plan) { p.ParityPassed = false },
		func(p *Plan) { p.RollbackTested = false },
		func(p *Plan) { p.ApprovedBy = "" },
	} {
		p := full
		mut(&p)
		if err := Authorize(cfg, p, Migrate); !errors.Is(err, ErrGateNotMet) {
			t.Fatalf("want ErrGateNotMet, got %v", err)
		}
	}
	if err := Authorize(Config{Enabled: true, AllowMigration: true}, full, Delete); !errors.Is(err, ErrActionDisabled) {
		t.Fatal("deletion needs its own flag")
	}
}
