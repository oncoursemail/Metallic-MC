//go:build linux

package hosting

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func persistentFixture(t *testing.T, cfg Config) (*Service, *testClock, string) {
	t.Helper()
	clock := &testClock{}
	clock.nanos.Store(time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC).UnixNano())
	cfg.Now = func() time.Time { return time.Unix(0, clock.nanos.Load()).UTC() }
	path := filepath.Join(t.TempDir(), "hosting", "state.json")
	s, err := NewPersistent(cfg, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, clock, path
}

func reopen(t *testing.T, s *Service, path string) *Service {
	t.Helper()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := NewPersistent(s.cfg, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = next.Close() })
	return next
}

func privateStatePath(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "hosting")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "state.json")
}

func TestPersistentRestartPreservesAuthorityAndAccounting(t *testing.T) {
	s, clock, path := persistentFixture(t, Config{MaxResources: Resources{4, 4096, 20480, 40}})
	active := enroll(t, s, "alice")
	activeServer := allocate(t, s, active, "active", 25565, resources())
	revoked := enroll(t, s, "bob")
	revokedServer := allocate(t, s, revoked, "revoked", 25565, resources())
	if err := s.RevokeHost("bob", revoked.Host.ID); err != nil {
		t.Fatal(err)
	}
	ticket, err := s.IssuePairingTicket("alice")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{active.Credential, revoked.Credential, ticket.Token, "mhc_", "mtp_", `"token"`, `"credential"`} {
		if bytes.Contains(before, []byte(secret)) {
			t.Fatal("persistent store contains a plaintext secret")
		}
	}
	for name, mode := range map[string]os.FileMode{path: 0600, path + ".lock": 0600, filepath.Dir(path): 0700} {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("private mode: %s: %v, %v", name, info, err)
		}
	}
	s = reopen(t, s, path)
	if !s.Persistent() {
		t.Fatal("persistent mode lost")
	}
	_, err = s.RedeemPairingTicket("alice", ticket.Token, "old pending ticket")
	wantError(t, err, ErrUnauthorized)
	hs, err := s.ListHosts("alice")
	if err != nil || len(hs) != 1 || hs[0] != active.Host {
		t.Fatalf("restored owner-scoped host: %+v, %v", hs, err)
	}
	_, err = s.PlanRoute("alice", active.Credential, route(active, activeServer))
	if err != nil {
		t.Fatalf("restored digest did not authenticate original credential: %v", err)
	}
	_, err = s.PlanRoute("bob", revoked.Credential, route(revoked, revokedServer))
	wantError(t, err, ErrRevoked)
	_, err = s.PlanRoute("bob", active.Credential, route(active, activeServer))
	wantError(t, err, ErrNotFound)
	allocate(t, s, active, "second", 25566, resources())
	_, err = s.AllocateServer("alice", AllocationRequest{HostID: active.Host.ID, ServerID: "over-limit", GamePort: 25567, Resources: resources()})
	wantError(t, err, ErrLimitExceeded)
	_, err = s.AllocateServer("alice", AllocationRequest{HostID: active.Host.ID, ServerID: "same-port", GamePort: 25565, Resources: resources()})
	wantError(t, err, ErrConflict)
	clock.advance(24 * time.Hour)
	s = reopen(t, s, path)
	_, err = s.PlanRoute("alice", active.Credential, route(active, activeServer))
	wantError(t, err, ErrExpired)
	hs, err = s.ListHosts("alice")
	if err != nil || len(hs) != 1 || !hs[0].CredentialExpiresAt.Equal(active.Host.CredentialExpiresAt) {
		t.Fatal("restart renewed or discarded an expired host")
	}
}

func TestPersistentRejectsTextThatCannotRoundTrip(t *testing.T) {
	s, _, _ := persistentFixture(t, Config{})
	invalid := string([]byte{'a', 0xff, 'b'})
	_, err := s.IssuePairingTicket(invalid)
	wantError(t, err, ErrInvalidInput)
	ticket, err := s.IssuePairingTicket("alice")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RedeemPairingTicket("alice", ticket.Token, invalid)
	wantError(t, err, ErrInvalidInput)
	if _, err = s.RedeemPairingTicket("alice", ticket.Token, "valid name"); err != nil {
		t.Fatal("invalid text consumed the pairing ticket:", err)
	}
}

func TestPersistentSingleWriterAndClosedService(t *testing.T) {
	s, _, path := persistentFixture(t, Config{})
	other, err := NewPersistent(Config{}, path)
	wantError(t, err, ErrStore)
	if other != nil {
		t.Fatal("second controller acquired a live store")
	}
	child := exec.Command(os.Args[0], "-test.run=^TestPersistentProcessHelper$")
	child.Env = append(os.Environ(), "METALLIC_TEST_STORE="+path, "METALLIC_TEST_ACTION=lock")
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("cross-process single-writer check: %v: %s", err, out)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal("close is not idempotent")
	}
	_, err = s.IssuePairingTicket("alice")
	wantError(t, err, ErrStore)
	_, err = s.ListHosts("alice")
	wantError(t, err, ErrStore)
	other, err = NewPersistent(Config{}, path)
	if err != nil {
		t.Fatal("writer lock not released:", err)
	}
	_ = other.Close()
}

// A subprocess exits without Close so the restart test exercises automatic OS
// lock release and acknowledges only snapshots synced before the abrupt exit.
func TestPersistentProcessHelper(t *testing.T) {
	path := os.Getenv("METALLIC_TEST_STORE")
	if path == "" {
		return
	}
	s, err := NewPersistent(Config{}, path)
	if os.Getenv("METALLIC_TEST_ACTION") == "lock" {
		if !errors.Is(err, ErrStore) || s != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	if err != nil {
		os.Exit(3)
	}
	h := enroll(t, s, "after-crash")
	allocate(t, s, h, "crash-server", 25565, resources())
	if err := s.RevokeHost("after-crash", h.Host.ID); err != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func TestPersistentRecoversAfterAbruptProcessExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosting", "state.json")
	child := exec.Command(os.Args[0], "-test.run=^TestPersistentProcessHelper$")
	child.Env = append(os.Environ(), "METALLIC_TEST_STORE="+path, "METALLIC_TEST_ACTION=crash")
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("crash fixture: %v: %s", err, out)
	}
	s, err := NewPersistent(Config{}, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hs, err := s.ListHosts("after-crash")
	if err != nil || len(hs) != 1 || !hs[0].Revoked {
		t.Fatal("acknowledged revocation missing after abrupt exit")
	}
	servers, err := s.ListServers("after-crash")
	if err != nil || len(servers) != 1 || servers[0].State != "policy_registered" {
		t.Fatal("allocation lost or falsely reported running after restart")
	}
}

func injectSaveFault(store *fileStore, stage string) func() {
	prior := *store
	switch stage {
	case "write":
		store.writeFile = func(*os.File, []byte) (int, error) { return 0, syscall.ENOSPC }
	case "short-write":
		store.writeFile = func(f *os.File, b []byte) (int, error) { return f.Write(b[:len(b)/2]) }
	case "file-sync":
		store.syncFile = func(*os.File) error { return syscall.EIO }
	case "close":
		store.closeFile = func(f *os.File) error { _ = f.Close(); return syscall.EIO }
	case "rename":
		store.rename = func(int, string, int, string) error { return syscall.EIO }
	case "directory-sync":
		store.syncDir = func(*os.File) error { return syscall.EIO }
	}
	return func() { *store = prior }
}

func TestPersistentSaveFailuresDoNotPublishChanges(t *testing.T) {
	for _, stage := range []string{"write", "short-write", "file-sync", "close", "rename"} {
		for _, operation := range []string{"enroll", "allocate", "revoke"} {
			t.Run(stage+"/"+operation, func(t *testing.T) {
				s, _, path := persistentFixture(t, Config{})
				h := enroll(t, s, "alice")
				server := allocate(t, s, h, "original", 25565, resources())
				ticket, err := s.IssuePairingTicket("alice")
				if err != nil {
					t.Fatal(err)
				}
				before, _ := os.ReadFile(path)
				repair := injectSaveFault(s.store.(*fileStore), stage)
				switch operation {
				case "enroll":
					var result Enrollment
					result, err = s.RedeemPairingTicket("alice", ticket.Token, "retry")
					if result != (Enrollment{}) {
						t.Fatal("failed save returned an enrollment or credential")
					}
				case "allocate":
					var result ServerAllocation
					result, err = s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: "retry", GamePort: 25566, Resources: resources()})
					if result != (ServerAllocation{}) {
						t.Fatal("failed save returned an allocation")
					}
				case "revoke":
					err = s.RevokeHost("alice", h.Host.ID)
				}
				wantError(t, err, ErrStore)
				after, _ := os.ReadFile(path)
				if !bytes.Equal(before, after) {
					t.Fatal("failed pre-commit operation changed the committed snapshot")
				}
				hs, err := s.ListHosts("alice")
				if err != nil || len(hs) != 1 || hs[0].Revoked {
					t.Fatal("failed save changed host state")
				}
				servers, err := s.ListServers("alice")
				if err != nil || len(servers) != 1 {
					t.Fatal("failed save changed allocations")
				}
				if _, err = s.PlanRoute("alice", h.Credential, route(h, server)); err != nil {
					t.Fatal("uncommitted failure damaged previous committed authority")
				}
				leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".hosting-state-*.tmp"))
				if len(leftovers) != 0 {
					t.Fatal("failed save left temporary snapshots")
				}
				repair()
				switch operation {
				case "enroll":
					_, err = s.RedeemPairingTicket("alice", ticket.Token, "retry")
				case "allocate":
					_, err = s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: "retry", GamePort: 25566, Resources: resources()})
				case "revoke":
					err = s.RevokeHost("alice", h.Host.ID)
				}
				if err != nil {
					t.Fatal("definite failed operation could not be retried:", err)
				}
				s = reopen(t, s, path)
				hs, _ = s.ListHosts("alice")
				servers, _ = s.ListServers("alice")
				if (operation == "enroll" && len(hs) != 2) || (operation == "allocate" && len(servers) != 2) || (operation == "revoke" && !hs[0].Revoked) {
					t.Fatal("retry was not durably committed")
				}
			})
		}
	}
}

func TestPersistentUncertainCommitFailsClosedUntilRestart(t *testing.T) {
	for _, operation := range []string{"enroll", "allocate", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			s, _, path := persistentFixture(t, Config{})
			h := enroll(t, s, "alice")
			server := allocate(t, s, h, "original", 25565, resources())
			ticket, _ := s.IssuePairingTicket("alice")
			_ = injectSaveFault(s.store.(*fileStore), "directory-sync")
			var err error
			switch operation {
			case "enroll":
				var result Enrollment
				result, err = s.RedeemPairingTicket("alice", ticket.Token, "uncertain")
				if result != (Enrollment{}) {
					t.Fatal("uncertain commit disclosed an enrollment")
				}
			case "allocate":
				_, err = s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: "uncertain", GamePort: 25566, Resources: resources()})
			case "revoke":
				err = s.RevokeHost("alice", h.Host.ID)
			}
			wantError(t, err, ErrStore)
			wantError(t, err, errCommitUncertain)
			if !s.storeFailed || len(s.tickets) != 0 {
				t.Fatal("uncertain commit did not seal the service and pending tickets")
			}
			_, err = s.PlanRoute("alice", h.Credential, route(h, server))
			wantError(t, err, ErrStore)
			_, err = s.IssuePairingTicket("alice")
			wantError(t, err, ErrStore)
			_, err = s.RedeemPairingTicket("alice", ticket.Token, "retry")
			wantError(t, err, ErrStore)
			_, err = s.ListHosts("alice")
			wantError(t, err, ErrStore)
			_, err = s.ListServers("alice")
			wantError(t, err, ErrStore)
			_, err = s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: "blocked", GamePort: 25567, Resources: resources()})
			wantError(t, err, ErrStore)
			wantError(t, s.RevokeHost("alice", h.Host.ID), ErrStore)
			s = reopen(t, s, path)
			hs, _ := s.ListHosts("alice")
			servers, _ := s.ListServers("alice")
			if (operation == "enroll" && len(hs) != 2) || (operation == "allocate" && len(servers) != 2) || (operation == "revoke" && !hs[0].Revoked) {
				t.Fatal("recovery did not load the actual post-rename snapshot")
			}
			if operation == "revoke" {
				_, err = s.PlanRoute("alice", h.Credential, route(h, server))
				wantError(t, err, ErrRevoked)
			}
		})
	}
}

func TestPersistentRejectsUnsafePaths(t *testing.T) {
	_, err := NewPersistent(Config{}, "relative.json")
	wantError(t, err, ErrInvalidInput)
	for _, attack := range []string{"public-parent", "public-state", "public-lock", "symlink-state", "symlink-lock", "symlink-ancestor", "hardlink-state", "fifo-state", "directory-state", "writable-ancestor", "wrong-owner"} {
		t.Run(attack, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "private")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "state.json")
			if err := os.WriteFile(path, []byte(`{"version":1,"hosts":[],"servers":[]}`), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch attack {
			case "public-parent":
				err = os.Chmod(dir, 0755)
			case "public-state":
				err = os.Chmod(path, 0644)
			case "public-lock":
				err = os.WriteFile(path+".lock", nil, 0644)
			case "symlink-state", "symlink-lock":
				target := filepath.Join(base, "target")
				if err = os.WriteFile(target, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
				name := path + ".lock"
				if attack == "symlink-state" {
					name = path
					if err = os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				err = os.Symlink(target, name)
			case "symlink-ancestor":
				link := filepath.Join(base, "link")
				err = os.Symlink(dir, link)
				path = filepath.Join(link, "state.json")
			case "hardlink-state":
				err = os.Link(path, filepath.Join(base, "other-name"))
			case "fifo-state":
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
				err = syscall.Mkfifo(path, 0600)
			case "directory-state":
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
				err = os.Mkdir(path, 0700)
			case "writable-ancestor":
				err = os.Chmod(base, 0777)
			case "wrong-owner":
				if os.Geteuid() != 0 {
					t.Skip("changing ownership requires root; other path checks run unprivileged")
				}
				err = os.Chown(path, 12345, -1)
				if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EINVAL) {
					t.Skip("this user namespace cannot assign another file owner")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			s, err := NewPersistent(Config{}, path)
			if s != nil {
				_ = s.Close()
				t.Fatal("unsafe path produced a usable service")
			}
			wantError(t, err, ErrStore)
			if attack == "symlink-state" || attack == "symlink-lock" {
				data, err := os.ReadFile(filepath.Join(base, "target"))
				if err != nil || string(data) != "untouched" {
					t.Fatal("symlink target was modified")
				}
			}
		})
	}
}

func TestPersistentRefusesReplacedStateAndChangedPermissions(t *testing.T) {
	for _, attack := range []string{"symlink", "directory-permissions", "lock-permissions"} {
		t.Run(attack, func(t *testing.T) {
			s, _, path := persistentFixture(t, Config{})
			h := enroll(t, s, "alice")
			target := filepath.Join(t.TempDir(), "untouched")
			if err := os.WriteFile(target, []byte("private target"), 0600); err != nil {
				t.Fatal(err)
			}
			switch attack {
			case "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "directory-permissions":
				if err := os.Chmod(filepath.Dir(path), 0777); err != nil {
					t.Fatal(err)
				}
			case "lock-permissions":
				if err := os.Chmod(path+".lock", 0666); err != nil {
					t.Fatal(err)
				}
			}
			wantError(t, s.RevokeHost("alice", h.Host.ID), ErrStore)
			hs, _ := s.ListHosts("alice")
			if len(hs) != 1 || hs[0].Revoked {
				t.Fatal("failed unsafe-path save changed memory")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "private target" {
				t.Fatal("unsafe save modified another file")
			}
		})
	}
}

func TestPersistentStrictSchemaAndPolicyValidation(t *testing.T) {
	s, _, source := persistentFixture(t, Config{})
	h := enroll(t, s, "alice")
	allocate(t, s, h, "first", 25565, resources())
	allocate(t, s, h, "second", 25566, resources())
	valid, _ := os.ReadFile(source)
	mutators := map[string]func(map[string]any){
		"version":              func(v map[string]any) { v["version"] = 2 },
		"unknown":              func(v map[string]any) { v["tickets"] = []any{} },
		"missing-hosts":        func(v map[string]any) { delete(v, "hosts") },
		"null-hosts":           func(v map[string]any) { v["hosts"] = nil },
		"null-servers":         func(v map[string]any) { v["servers"] = nil },
		"missing-revoked":      func(v map[string]any) { delete(jsonHost(v), "revoked") },
		"null-revoked":         func(v map[string]any) { jsonHost(v)["revoked"] = nil },
		"plaintext-credential": func(v map[string]any) { jsonHost(v)["credential"] = "mhc_test" },
		"wrong-digest":         func(v map[string]any) { jsonHost(v)["credential_digest"] = "not-a-digest" },
		"zero-digest":          func(v map[string]any) { jsonHost(v)["credential_digest"] = strings.Repeat("0", 64) },
		"invalid-owner":        func(v map[string]any) { jsonHost(v)["owner_id"] = " alice" },
		"invalid-name":         func(v map[string]any) { jsonHost(v)["name"] = "bad\nname" },
		"invalid-host-id":      func(v map[string]any) { jsonHost(v)["id"] = "../host" },
		"invalid-time":         func(v map[string]any) { jsonHost(v)["created_at"] = "invalid" },
		"zero-time":            func(v map[string]any) { jsonHost(v)["created_at"] = "0001-01-01T00:00:00Z" },
		"negative-lifetime":    func(v map[string]any) { jsonHost(v)["credential_expires_at"] = "2026-01-01T00:00:00Z" },
		"unbounded-lifetime":   func(v map[string]any) { jsonHost(v)["credential_expires_at"] = "2036-01-01T00:00:00Z" },
		"duplicate-host":       func(v map[string]any) { v["hosts"] = append(v["hosts"].([]any), jsonHost(v)) },
		"duplicate-digest": func(v map[string]any) {
			copy := make(map[string]any)
			for k, value := range jsonHost(v) {
				copy[k] = value
			}
			copy["id"] = "different-host"
			v["hosts"] = append(v["hosts"].([]any), copy)
		},
		"orphan-server":           func(v map[string]any) { jsonServer(v, 0)["host_id"] = "missing" },
		"cross-owner":             func(v map[string]any) { jsonServer(v, 0)["owner_id"] = "bob" },
		"invalid-server-id":       func(v map[string]any) { jsonServer(v, 0)["id"] = "../server" },
		"duplicate-server":        func(v map[string]any) { jsonServer(v, 1)["id"] = jsonServer(v, 0)["id"] },
		"duplicate-port":          func(v map[string]any) { jsonServer(v, 1)["game_port"] = jsonServer(v, 0)["game_port"] },
		"admin-port":              func(v map[string]any) { jsonServer(v, 0)["game_port"] = 22 },
		"false-online-state":      func(v map[string]any) { jsonServer(v, 0)["state"] = "running" },
		"invalid-server-time":     func(v map[string]any) { jsonServer(v, 0)["created_at"] = "2020-01-01T00:00:00Z" },
		"allocation-after-expiry": func(v map[string]any) { jsonServer(v, 0)["created_at"] = "2030-01-01T00:00:00Z" },
		"negative-resource":       func(v map[string]any) { jsonServer(v, 0)["resources"].(map[string]any)["cpu_cores"] = -1 },
		"aggregate-over-limit":    func(v map[string]any) { jsonServer(v, 0)["resources"].(map[string]any)["cpu_cores"] = 16 },
		"overflow-resource": func(v map[string]any) {
			jsonServer(v, 0)["resources"].(map[string]any)["memory_mb"] = json.Number("9223372036854775807")
		},
	}
	cases := make(map[string][]byte)
	for name, mutate := range mutators {
		var value map[string]any
		if err := json.Unmarshal(valid, &value); err != nil {
			t.Fatal(err)
		}
		mutate(value)
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		cases[name] = data
	}
	cases["empty"] = nil
	cases["invalid-utf8"] = bytes.Replace(valid, []byte("alice"), []byte{'a', 0xff, 'b'}, 1)
	cases["trailing-json"] = append(append([]byte{}, valid...), []byte(` {"version":1}`)...)
	cases["duplicate-key"] = bytes.Replace(valid, []byte(`"revoked":false`), []byte(`"revoked":true,"revoked":false`), 1)
	cases["case-alias"] = bytes.Replace(valid, []byte(`"revoked":false`), []byte(`"Revoked":false`), 1)
	cases["deep-data"] = []byte(`{"version":1,"hosts":[],"servers":[],"bad":[[[[[[[[[[0]]]]]]]]]]}`)
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			path := privateStatePath(t)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			broken, err := NewPersistent(s.cfg, path)
			wantError(t, err, ErrStore)
			if broken != nil {
				_ = broken.Close()
				t.Fatal("invalid store silently started")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(data, after) {
				t.Fatal("failed recovery overwrote evidence")
			}
			if err := os.WriteFile(path, valid, 0600); err != nil {
				t.Fatal(err)
			}
			fixed, err := NewPersistent(s.cfg, path)
			if err != nil {
				t.Fatal("failed open leaked its writer lock:", err)
			}
			_ = fixed.Close()
		})
	}
	for _, cfg := range []Config{{MaxServers: 1}, {MaxResources: Resources{2, 2048, 10240, 20}}} {
		path := privateStatePath(t)
		if err := os.WriteFile(path, valid, 0600); err != nil {
			t.Fatal(err)
		}
		_, err := NewPersistent(cfg, path)
		wantError(t, err, ErrStore)
	}
	path := privateStatePath(t)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(maxStoreBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	_, err = NewPersistent(Config{}, path)
	wantError(t, err, ErrStore)
}

func jsonHost(v map[string]any) map[string]any { return v["hosts"].([]any)[0].(map[string]any) }
func jsonServer(v map[string]any, index int) map[string]any {
	return v["servers"].([]any)[index].(map[string]any)
}

func TestPersistentConcurrentMutationHasNoLostUpdates(t *testing.T) {
	s, _, path := persistentFixture(t, Config{MaxResources: Resources{10, 10, 10, 10}})
	ticket, _ := s.IssuePairingTicket("alice")
	const workers = 16
	results := make(chan Enrollment, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := s.RedeemPairingTicket("alice", ticket.Token, "one host")
			if err == nil {
				results <- h
			} else {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	if len(results) != 1 || len(errs) != workers-1 {
		t.Fatal("persistent ticket redeemed more than once")
	}
	for err := range errs {
		wantError(t, err, ErrUnauthorized)
	}
	h := <-results
	errs = make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.AllocateServer("alice", AllocationRequest{HostID: h.Host.ID, ServerID: fmt.Sprintf("server-%d", i), GamePort: 25565 + i, Resources: Resources{1, 1, 1, 1}})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	s = reopen(t, s, path)
	servers, err := s.ListServers("alice")
	if err != nil || len(servers) != 10 {
		t.Fatalf("concurrent durable allocations lost: %d, %v", len(servers), err)
	}
	for _, server := range servers {
		if _, err := s.PlanRoute("alice", h.Credential, route(h, server)); err != nil {
			t.Fatal(err)
		}
	}
}
