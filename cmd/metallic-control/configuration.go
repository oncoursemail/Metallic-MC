package main

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/oncoursemail/metallic-mc/internal/control"
)

func canonicalOrigin(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", errors.New("Architect origin must be an HTTP or HTTPS origin without a path, credentials or query")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || (u.Scheme == "http" && !isLoopback(host)) {
		return "", errors.New("Architect origin must use HTTPS outside loopback")
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("invalid Architect origin port")
		}
	}
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host, nil
}

// Flow cookies are host-only. Reject a callback that would be sent to a
// different browser origin, and inspect the parsed scheme (URL schemes are
// case-insensitive) before allowing HTTPS with insecure cookie settings.
func validateDiscordTransport(cfg control.Config, address, redirect string) error {
	u, err := url.Parse(redirect)
	if err != nil || u.User != nil {
		return errors.New("invalid Discord redirect URL")
	}
	callbackOrigin, err := canonicalOrigin(u.Scheme + "://" + u.Host)
	if err != nil {
		return errors.New("invalid Discord callback origin")
	}
	if u.Scheme == "https" && (!cfg.SecureCookies || cfg.ArchitectOrigin == "") {
		return errors.New("HTTPS Discord linking requires secure cookies and METALLIC_ARCHITECT_ORIGIN")
	}
	expected := cfg.ArchitectOrigin
	if expected == "" {
		expected = "http://" + address
	}
	expected, err = canonicalOrigin(expected)
	if err != nil || callbackOrigin != expected {
		return errors.New("Discord callback must use the same origin as the Architect browser session")
	}
	return nil
}
