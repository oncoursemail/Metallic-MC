//go:build !linux

package hosting

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestPersistentModeFailsExplicitlyOnUnsupportedPlatforms(t *testing.T) {
	s, err := NewPersistent(Config{}, filepath.Join(t.TempDir(), "state.json"))
	if s != nil || !errors.Is(err, ErrStore) {
		t.Fatalf("unsupported durable mode: service=%v, error=%v", s, err)
	}
	memory, err := NewService(Config{})
	if err != nil || memory.Persistent() {
		t.Fatal("memory-only hosting was affected by the durable platform boundary")
	}
	_ = memory.Close()
}
