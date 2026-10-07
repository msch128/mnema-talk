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

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/s3"
	"github.com/msch128/mnema-talk/internal/server"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/update"
	"github.com/msch128/mnema-talk/internal/version"
)

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
	level := logLevel(cfg)
	if cfg.LogLevel == "debug" && os.Getenv("PION_LOG_DEBUG") == "" {
		// Pion's ICE agent then logs every candidate pair it checks.
		_ = os.Setenv("PION_LOG_DEBUG", "ice")
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	slog.Info("starting mnema-talk", "version", version.Current(), "revision", version.Commit(), "env", cfg.AppEnv)

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
	if err := auth.EnsureAdminUser(ctx, pool, cfg.AdminUsername, cfg.AdminInitialPassword, cfg.IsProduction()); err != nil {
		return err
	}

	// Object storage may come up after the app (or be down for a while):
	// keep retrying in the background; until then uploads answer 503 and
	// /api/health reports the outage.
	store := startLazyStore(ctx, func(ctx context.Context) (media.Store, error) {
		return s3.New(ctx, cfg)
	}, 2*time.Second, time.Minute)
	media.StartRetentionWorker(ctx, pool, store, cfg.MediaRetentionDays)

	// Addresses browsers send media to: typically the public IP (or a
	// dynamic-DNS name for it) plus the LAN IP.
	announce, err := sfu.ResolveAnnounce(ctx, cfg.WebRTCAnnounce)
	if err != nil {
		return err
	}
	voice, err := sfu.NewSFU(cfg.WebRTCUDPPortMin, cfg.WebRTCUDPPortMax, announce, cfg.WebRTCSTUNURLs, sfu.WithUDPMuxPort(cfg.WebRTCUDPMuxPort))
	if err != nil {
		slog.Warn("webrtc sfu unavailable, voice disabled", "err", err)
		voice = nil
	} else {
		defer func() {
			if err := voice.Close(); err != nil {
				slog.Warn("close SFU", "err", err)
			}
		}()
		slog.Info("webrtc announce", "ips", announce, "ports", fmt.Sprintf("%d-%d", cfg.WebRTCUDPPortMin, cfg.WebRTCUDPPortMax), "udp_mux_port", cfg.WebRTCUDPMuxPort)
		voice.KeepAnnounceCurrent(ctx, cfg.WebRTCAnnounce, 5*time.Minute)
	}

	// Release check against GitHub (admin System tab). Disabled with
	// UPDATE_CHECK_ENABLED=false: then the server makes no request at all.
	var updates *update.Checker
	if cfg.UpdateCheck {
		updates = update.NewChecker(update.DefaultReleaseURL, version.Current())
		go updates.Run(ctx, 30*time.Second)
	}

	router, err := server.NewRouter(server.Deps{Config: cfg, DB: pool, Store: store, StorageReady: store.Ready, SFU: voice, Version: version.Current(), Updates: updates, Context: ctx})
	if err != nil {
		return err
	}
	defer router.Close()
	return server.Run(ctx, cfg, router)
}

// logLevel maps LOG_LEVEL (validated by config) to a slog level; empty means
// info in production and debug otherwise.
func logLevel(cfg *config.Config) slog.Level {
	switch cfg.LogLevel {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	if cfg.IsProduction() {
		return slog.LevelInfo
	}
	return slog.LevelDebug
}
