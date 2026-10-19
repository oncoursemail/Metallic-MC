package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/agent"
)

func main() {
	endpoint := getenv("METALLIC_CONTROL_URL", "http://127.0.0.1:8080")
	token := os.Getenv("METALLIC_AGENT_TOKEN")
	if len(token) < 24 {
		log.Fatal("METALLIC_AGENT_TOKEN must be at least 24 characters")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		log.Fatal("invalid METALLIC_CONTROL_URL")
	}
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
		if parsed.Scheme != "http" || (!loopback && os.Getenv("METALLIC_AGENT_ALLOW_HTTP") != "true") {
			log.Fatal("remote agent transport must use HTTPS; HTTP outside localhost requires METALLIC_AGENT_ALLOW_HTTP=true for isolated testing only")
		}
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		log.Fatal("invalid control URL")
	}
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}
	nodeID := getenv("METALLIC_NODE_ID", hostname)
	services, err := agent.ParseServices(os.Getenv("METALLIC_SERVICES"))
	if err != nil {
		log.Fatal(err)
	}
	diskPath := getenv("METALLIC_AGENT_DISK_PATH", "/")
	interval := 10 * time.Second
	client := newHeartbeatClient()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		hb, err := agent.Sample(ctx, nodeID, hostname, diskPath, services)
		if err == nil {
			var b []byte
			b, err = json.Marshal(hb)
			if err == nil {
				req, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/api/v1/heartbeat", bytes.NewReader(b))
				if requestErr != nil {
					err = requestErr
				} else {
					req.Header.Set("Authorization", "Bearer "+token)
					req.Header.Set("Content-Type", "application/json")
					resp, reqErr := client.Do(req)
					if reqErr != nil {
						err = reqErr
					} else {
						_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
						_ = resp.Body.Close()
						if resp.StatusCode != http.StatusNoContent {
							err = fmt.Errorf("control returned HTTP %d", resp.StatusCode)
						}
					}
				}
			}
		}
		cancel()
		if err != nil {
			log.Printf("heartbeat failed: %v", err)
		} else {
			log.Printf("heartbeat accepted: node=%s, services=%d", nodeID, len(services))
		}
		time.Sleep(interval)
	}
}
func newHeartbeatClient() *http.Client {
	return &http.Client{
		Timeout: 7 * time.Second,
		// URL validation covers the configured controller only. A redirect
		// must not move its bearer token or host telemetry to another URL.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
func getenv(k, d string) string {
	if x := strings.TrimSpace(os.Getenv(k)); x != "" {
		return x
	}
	return d
}
