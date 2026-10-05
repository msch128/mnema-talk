// Command server runs Mnema Talk: REST API, WebSocket hub, WebRTC SFU and the
// embedded web app in a single process.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/s3"
	"github.com/msch128/mnema-talk/internal/server"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if cfg.LogLevel == "debug" || (cfg.LogLevel == "" && !cfg.IsProduction()) {
		level = slog.LevelDebug
	}
	if cfg.LogLevel == "debug" && os.Getenv("PION_LOG_DEBUG") == "" {
		// Pion's ICE agent then logs every candidate pair it checks.
		_ = os.Setenv("PION_LOG_DEBUG", "ice")
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	slog.Info("starting mnema-talk", "version", version, "env", cfg.AppEnv)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := pool.Migrate(ctx); err != nil {
		return err
	}
	if err := pool.EnsureAdminUser(ctx, cfg.AdminUsername, cfg.AdminInitialPassword); err != nil {
		return err
	}

	var store media.Store
	if s3Client, err := s3.New(ctx, cfg); err != nil {
		slog.Warn("object storage unavailable, uploads disabled", "err", err)
	} else {
		store = s3Client
	}
	media.StartRetentionWorker(ctx, pool, store, cfg.MediaRetentionDays)

	// Addresses browsers send media to: typically the public IP (or a
	// dynamic-DNS name for it) plus the LAN IP.
	announce, err := sfu.ResolveAnnounce(ctx, cfg.WebRTCAnnounce)
	if err != nil {
		return err
	}
	voice, err := sfu.NewSFU(cfg.WebRTCUDPPortMin, cfg.WebRTCUDPPortMax, announce, cfg.WebRTCSTUNURLs)
	if err != nil {
		slog.Warn("webrtc sfu unavailable, voice disabled", "err", err)
		voice = nil
	} else {
		slog.Info("webrtc announce", "ips", announce, "ports", fmt.Sprintf("%d-%d", cfg.WebRTCUDPPortMin, cfg.WebRTCUDPPortMax))
		voice.KeepAnnounceCurrent(ctx, cfg.WebRTCAnnounce, 5*time.Minute)
	}

	router, err := server.NewRouter(server.Deps{Config: cfg, DB: pool, Store: store, SFU: voice, Version: version})
	if err != nil {
		return err
	}
	return server.Run(ctx, cfg, router)
}
