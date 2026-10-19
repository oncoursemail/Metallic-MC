package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordingTransport func(*http.Request) (*http.Response, error)

func (f recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestHeartbeatClientRefusesRedirects(t *testing.T) {
	// A configured HTTPS endpoint may be misrouted by a proxy. Never send its
	// credential or telemetry to the redirect target, even on the same host.
	targets := []struct{ name, url string }{
		{"same-host", "https://control.example/elsewhere"},
		{"cross-host", "https://other.example/collect"},
		{"downgrade", "http://control.example:8080/collect"},
	}
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, target := range targets {
			t.Run(fmt.Sprintf("%d/%s", status, target.name), func(t *testing.T) {
				client := newHeartbeatClient()
				requests := 0
				client.Transport = recordingTransport(func(r *http.Request) (*http.Response, error) {
					requests++
					if requests > 1 {
						t.Errorf("redirect target received %s %s; bearer forwarded: %t", r.Method, r.URL, r.Header.Get("Authorization") != "")
						return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
					}
					return &http.Response{StatusCode: status, Header: http.Header{"Location": {target.url}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				})
				req, err := http.NewRequest(http.MethodPost, "https://control.example/api/v1/heartbeat", bytes.NewReader([]byte(`{"nodeId":"fixture"}`)))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer synthetic-test-token-not-a-real-secret")
				req.Header.Set("Content-Type", "application/json")
				response, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if requests != 1 || response.StatusCode != status {
					t.Fatalf("requests=%d, HTTP %d; want one request and original HTTP %d", requests, response.StatusCode, status)
				}
			})
		}
	}
}

func TestHeartbeatClientSendsDirectRequest(t *testing.T) {
	client := newHeartbeatClient()
	const payload = `{"nodeId":"fixture"}`
	const token = "Bearer synthetic-test-token-not-a-real-secret"
	requests := 0
	client.Transport = recordingTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if r.Method != http.MethodPost || r.URL.String() != "https://control.example/api/v1/heartbeat" || r.Header.Get("Authorization") != token || r.Header.Get("Content-Type") != "application/json" || string(body) != payload {
			t.Fatalf("direct heartbeat changed: method=%s, URL=%s, body=%s", r.Method, r.URL, body)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	req, err := http.NewRequest(http.MethodPost, "https://control.example/api/v1/heartbeat", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent || requests != 1 {
		t.Fatalf("direct heartbeat: HTTP %d, requests=%d", response.StatusCode, requests)
	}
}
