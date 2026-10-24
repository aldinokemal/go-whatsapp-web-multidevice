// Command hermes-cs-bridge connects the GOWA CS WhatsApp number to a Hermes
// Agent profile: incoming messages go to Hermes, replies go back to WhatsApp,
// and sensitive topics are handed over to a human admin.
//
// Setup guide: docs/cs-ai-hermes.md in this repository.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // TZ works in the minimal image
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Error("invalid configuration", "err", err)
		os.Exit(1)
	}
	store, err := LoadStore(cfg.StateFile)
	if err != nil {
		log.Error("cannot load state", "file", cfg.StateFile, "err", err)
		os.Exit(1)
	}

	settings, err := LoadSettings(cfg.SettingsFile, cfg.MediaDir, defaultSettings(cfg))
	if err != nil {
		log.Error("cannot load settings", "file", cfg.SettingsFile, "err", err)
		os.Exit(1)
	}
	if len(settings.Get().AdminNumbers) == 0 {
		log.Warn("no admin numbers configured; handoff notices go nowhere until they are set at /cs-ai")
	}
	if cfg.AdminToken == "" {
		log.Warn("ADMIN_TOKEN is empty; the /cs-ai settings page is disabled")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bridge := NewBridge(ctx, cfg, NewGowaClient(cfg), NewHermesClient(cfg), store, settings, log)
	outbox, err := NewDashboardOutbox(cfg, log)
	if err != nil {
		log.Error("cannot open dashboard outbox", "dir", cfg.DashboardOutboxDir, "err", err)
		os.Exit(1)
	}
	if outbox != nil {
		bridge.events = outbox
		go outbox.Run(ctx)
		log.Info("dashboard CRM sync on", "url", cfg.DashboardURL)
	} else {
		log.Warn("DASHBOARD_URL or DASHBOARD_INGEST_TOKEN is empty; leads are not sent to the Dashboard CRM")
	}
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: bridge.Routes(), ReadHeaderTimeout: 10 * time.Second}

	go func() {
		log.Info("hermes-cs-bridge listening", "addr", cfg.ListenAddr, "device", cfg.GowaDeviceID,
			"hermes", cfg.HermesURL, "admins", len(settings.Get().AdminNumbers), "settings_page", cfg.AdminToken != "")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server stopped", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down; finishing in-flight replies")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	done := make(chan struct{})
	go func() { bridge.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		log.Warn("timed out waiting for in-flight replies")
	}
}

// healthcheck is used by the Docker HEALTHCHECK.
func healthcheck() int {
	addr := env("LISTEN_ADDR", "127.0.0.1:8650")
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
