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
	"github.com/oncoursemail/metallic-mc/internal/identity"
)

func main() {
	cfg := control.Config{
		AdminUser:        getenv("METALLIC_ADMIN_USER", "admin"),
		AdminPassword:    os.Getenv("METALLIC_ADMIN_PASSWORD"),
		AgentToken:       os.Getenv("METALLIC_AGENT_TOKEN"),
		AIReadToken:      os.Getenv("METALLIC_STARAI_READ_TOKEN"),
		SecureCookies:    os.Getenv("METALLIC_COOKIE_SECURE") == "true",
		ArchitectEnabled: os.Getenv("METALLIC_ARCHITECT_ENABLED") == "true",
		ArchitectOrigin:  strings.TrimRight(os.Getenv("METALLIC_ARCHITECT_ORIGIN"), "/"),
	}
	if len(cfg.AdminPassword) < 12 || len(cfg.AgentToken) < 24 {
		log.Fatal("METALLIC_ADMIN_PASSWORD (min 12 chars) and METALLIC_AGENT_TOKEN (min 24 chars) must be set")
	}
	if cfg.AIReadToken != "" && (len(cfg.AIReadToken) < 24 || cfg.AIReadToken == cfg.AgentToken) {
		log.Fatal("METALLIC_STARAI_READ_TOKEN must be >=24 chars and distinct from the agent token")
	}
	address := getenv("METALLIC_LISTEN_ADDR", "127.0.0.1:8080")
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		log.Fatal("invalid METALLIC_LISTEN_ADDR: ", err)
	}
	if !isLoopback(host) {
		log.Fatal("Metallic Control must bind to loopback in the foundation release; put an authenticated HTTPS reverse proxy or VPN in front of it")
	}
	controller := control.NewController(cfg)
	if cfg.ArchitectEnabled {
		id, secret := os.Getenv("METALLIC_DISCORD_CLIENT_ID"), os.Getenv("METALLIC_DISCORD_CLIENT_SECRET")
		redirect, store := os.Getenv("METALLIC_DISCORD_REDIRECT_URL"), os.Getenv("METALLIC_IDENTITY_STORE_PATH")
		if id != "" || secret != "" || redirect != "" || store != "" {
			if strings.HasPrefix(redirect, "https://") && (!cfg.SecureCookies || cfg.ArchitectOrigin == "") {
				log.Fatal("HTTPS Discord linking requires secure cookies and METALLIC_ARCHITECT_ORIGIN")
			}
			provider, err := identity.NewDiscord(identity.DiscordConfig{ClientID: id, ClientSecret: secret, RedirectURL: redirect})
			if err != nil {
				log.Fatal("invalid Discord application configuration")
			}
			if err := controller.ConfigureDiscord(provider, store); err != nil {
				log.Fatal("could not initialize Discord identity store")
			}
		}
	}
	server := &http.Server{
		Addr:              address,
		Handler:           controller.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
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
