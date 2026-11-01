package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCPU(t *testing.T) {
	stats, err := parseCPU("cpu  10 20 30 40 50 60 70 80\ncpu0  1 2 3 4 5 6 7\n")
	if err != nil {
		t.Fatal(err)
	}
	if stats.total != 360 || stats.idle != 90 {
		t.Fatalf("stats=%+v", stats)
	}
}
func TestParseServices(t *testing.T) {
	v, err := ParseServices("proxy@127.0.0.1:25565,lobby@127.0.0.1:25566")
	if err != nil || len(v) != 2 {
		t.Fatalf("%+v %v", v, err)
	}
	for _, x := range []string{"bad", "lobby@no-port", "proxy@127.0.0.1:99999"} {
		if _, err := ParseServices(x); err == nil {
			t.Errorf("accepted %q", x)
		}
	}
}

func TestPressureSome10ParsesLinuxPSI(t *testing.T) {
	temp := t.TempDir()
	path := filepath.Join(temp, "cpu-pressure")
	if err := os.WriteFile(path, []byte("some avg10=8.31 avg60=7.12 avg300=6.01 total=1253586\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := pressureSome10(path)
	if err != nil || got != 8.31 {
		t.Fatalf("PSI read got %.2f, err %v", got, err)
	}
	if err := os.WriteFile(path, []byte("full avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := pressureSome10(path); err == nil {
		t.Fatal("expected error for missing some PSI entry")
	}
}

func TestObserveLegacyProcessNoSecrets(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "120"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "120", "comm"), []byte("wings\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "140"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "140", "comm"), []byte("llama-server\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := observeLegacyProcess(root, "wings")
	if err != nil || !got {
		t.Fatalf("expected observation, got %v %v", got, err)
	}
	got, err = observeLegacyProcess(root, "pterodactyl")
	if err != nil || got {
		t.Fatalf("must not invent old panel, got %v %v", got, err)
	}
	_, err = observeLegacyProcess(filepath.Join(root, "not-found"), "wings")
	if !os.IsNotExist(err) {
		t.Fatalf("expected explicit error for missing procfs, got %v", err)
	}
}
