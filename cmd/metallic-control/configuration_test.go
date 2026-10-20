package main

import (
	"testing"

	"github.com/oncoursemail/metallic-mc/internal/control"
)

func TestDiscordTransportRequiresSecureMatchingOrigin(t *testing.T) {
	const callback = "/api/v1/architect/discord/callback"
	cases := []struct {
		name, origin, redirect string
		secure, valid          bool
	}{
		{"loopback", "", "http://127.0.0.1:8080" + callback, false, true},
		{"https", "https://control.example", "https://control.example" + callback, true, true},
		{"canonical-default-port", "https://control.example", "HTTPS://CONTROL.EXAMPLE:443" + callback, true, true},
		{"uppercase-insecure", "https://control.example", "HTTPS://control.example" + callback, false, false},
		{"mixed-case-insecure", "https://control.example", "hTtPs://control.example" + callback, false, false},
		{"missing-origin", "", "https://control.example" + callback, true, false},
		{"wrong-host", "https://control.example", "https://other.example" + callback, true, false},
		{"wrong-port", "https://control.example:8443", "https://control.example" + callback, true, false},
		{"wrong-scheme", "https://127.0.0.1:8080", "http://127.0.0.1:8080" + callback, true, false},
		{"wrong-local-port", "", "http://127.0.0.1:8081" + callback, false, false},
		{"public-http", "http://control.example", "http://control.example" + callback, false, false},
		{"malformed-origin", "https://control.example/path", "https://control.example" + callback, true, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cfg := control.Config{ArchitectOrigin: test.origin, SecureCookies: test.secure}
			if err := validateDiscordTransport(cfg, "127.0.0.1:8080", test.redirect); (err == nil) != test.valid {
				t.Fatalf("valid=%t, error=%v", test.valid, err)
			}
		})
	}
}

func TestCanonicalOriginRejectsAmbiguousConfiguration(t *testing.T) {
	for _, value := range []string{"https://control.example?", "https://control.example?x=1", "https://control.example/#fragment", "https://user@control.example", "https://control.example:65536", "ftp://control.example", "https://", "http://public.example", "https://control.example/path"} {
		if _, err := canonicalOrigin(value); err == nil {
			t.Errorf("accepted invalid origin %q", value)
		}
	}
	got, err := canonicalOrigin("HTTPS://CONTROL.EXAMPLE:443/")
	if err != nil || got != "https://control.example" {
		t.Fatalf("canonical origin %q: %v", got, err)
	}
}
