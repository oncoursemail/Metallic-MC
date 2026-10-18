package main

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/control"
)

func main() {
	cfg := control.Config{
		AdminUser:     getenv("METALLIC_ADMIN_USER", "admin"),
		AdminPassword: os.Getenv("METALLIC_ADMIN_PASSWORD"),
		AgentToken:    os.Getenv("METALLIC_AGENT_TOKEN"),
		SecureCookies: os.Getenv("METALLIC_COOKIE_SECURE") == "true",
	}
	if len(cfg.AdminPassword) < 12 || len(cfg.AgentToken) < 24 {
		log.Fatal("METALLIC_ADMIN_PASSWORD (min 12 chars) and METALLIC_AGENT_TOKEN (min 24 chars) must be set")
	}
	address := getenv("METALLIC_LISTEN_ADDR", "127.0.0.1:8080")
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		log.Fatal("invalid METALLIC_LISTEN_ADDR: ", err)
	}
	if !isLoopback(host) {
		log.Fatal("Metallic Control must bind to loopback in the foundation release; put an authenticated HTTPS reverse proxy or VPN in front of it")
	}
	server := &http.Server{
		Addr:              address,
		Handler:           control.New(cfg),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	log.Printf("Metallic Control listening at %s (keep private; terminate HTTPS at a trusted reverse proxy)", address)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
func getenv(k, d string) string {
	if s := strings.TrimSpace(os.Getenv(k)); s != "" {
		return s
	}
	return d
}
func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
