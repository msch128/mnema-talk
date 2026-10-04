// Command server runs Mnema Talk: REST API, WebSocket hub, WebRTC SFU and the
// embedded web app in a single process.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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
	if !cfg.IsProduction() {
		level = slog.LevelDebug
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

	voice, err := sfu.NewSFU(cfg.WebRTCUDPPortMin, cfg.WebRTCUDPPortMax, cfg.WebRTCNAT1to1IP, cfg.WebRTCSTUNURLs)
	if err != nil {
		slog.Warn("webrtc sfu unavailable, voice disabled", "err", err)
		voice = nil
	}

	router, err := server.NewRouter(server.Deps{Config: cfg, DB: pool, Store: store, SFU: voice, Version: version})
	if err != nil {
		return err
	}
	return server.Run(ctx, cfg, router)
}
