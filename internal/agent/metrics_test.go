package agent

import "testing"

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
