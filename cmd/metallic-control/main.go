package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/control"
	"github.com/oncoursemail/metallic-mc/internal/identity"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
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
		return errors.New("METALLIC_ADMIN_PASSWORD (min 12 chars) and METALLIC_AGENT_TOKEN (min 24 chars) must be set")
	}
	if cfg.AIReadToken != "" && (len(cfg.AIReadToken) < 24 || cfg.AIReadToken == cfg.AgentToken) {
		return errors.New("METALLIC_STARAI_READ_TOKEN must be >=24 chars and distinct from the agent token")
	}
	address := getenv("METALLIC_LISTEN_ADDR", "127.0.0.1:8080")
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("invalid METALLIC_LISTEN_ADDR")
	}
	if !isLoopback(host) {
		return errors.New("Metallic-MC Control must bind to loopback; put an authenticated HTTPS reverse proxy or VPN in front of it")
	}
	if cfg.ArchitectOrigin != "" {
		origin, err := canonicalOrigin(cfg.ArchitectOrigin)
		if err != nil {
			return err
		}
		cfg.ArchitectOrigin = origin
	}
	controller := control.NewController(cfg)
	defer controller.Close()
	if path := strings.TrimSpace(os.Getenv("METALLIC_HOSTING_STORE_PATH")); path != "" {
		if err := controller.ConfigureHostingStore(path); err != nil {
			return errors.New("could not initialize private hosting store; verify its path, permissions and exclusive access")
		}
	}
	if cfg.ArchitectEnabled {
		id, secret := os.Getenv("METALLIC_DISCORD_CLIENT_ID"), os.Getenv("METALLIC_DISCORD_CLIENT_SECRET")
		redirect, store := os.Getenv("METALLIC_DISCORD_REDIRECT_URL"), os.Getenv("METALLIC_IDENTITY_STORE_PATH")
		if id != "" || secret != "" || redirect != "" || store != "" {
			if err := validateDiscordTransport(cfg, address, redirect); err != nil {
				return err
			}
			provider, err := identity.NewDiscord(identity.DiscordConfig{ClientID: id, ClientSecret: secret, RedirectURL: redirect})
			if err != nil {
				return errors.New("invalid Discord application configuration")
			}
			if err := controller.ConfigureDiscord(provider, store); err != nil {
				return errors.New("could not initialize Discord identity store")
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
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("Metallic-MC Control listening at %s (private)", address)
	return serveUntilCanceled(ctx, server, listener)
}

// Stop accepting requests, drain active requests, then release persistent state.
// A fixed deadline keeps service-manager restarts bounded without leaving the
// host registry unlocked while an old request can still commit a mutation.
func serveUntilCanceled(ctx context.Context, server *http.Server, listener net.Listener) error {
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		if err := <-result; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
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
