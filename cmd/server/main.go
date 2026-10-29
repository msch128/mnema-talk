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
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/opwarn"
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if len(os.Args) > 1 && os.Args[1] == "unlock" {
		return unlockCommand(ctx, os.Args[2:], os.Stdout)
	}
	if len(os.Args) > 1 && os.Args[1] == "reset-2fa" {
		return reset2faCommand(ctx, os.Args[2:], os.Stdout)
	}
	return runWithContext(ctx)
}

// runWithContext owns the application lifetime. Cancelling the supplied context
// stops the server and its background workers.
func runWithContext(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	setupLogging(cfg)
	warnBroadTrustedProxies(cfg)
	warnInsecurePublicURL(cfg)

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

	if _, err := chat.LocalizeSeedLayout(ctx, pool, cfg.InitialLanguage); err != nil {
		return err
	}

	// Object storage may come up after the app (or be down for a while):
	// keep retrying in the background; until then uploads answer 503 and
	// /api/health reports the outage.
	store := startLazyStore(ctx, func(ctx context.Context) (media.Store, error) {
		return s3.New(ctx, cfg)
	}, 2*time.Second, time.Minute)
	media.StartRetentionWorker(ctx, pool, store, cfg.MediaRetentionDays)
	media.StartOrphanScan(ctx, pool, store)

	voice, closeVoice, err := startVoice(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeVoice()

	updates := startUpdateChecker(ctx, cfg)

	router, err := server.NewRouter(server.Deps{Config: cfg, DB: pool, Store: store, StorageReady: store.Ready, SFU: voice, Version: version.Current(), Updates: updates, Context: ctx})
	if err != nil {
		return err
	}
	defer router.Close()
	return server.Run(ctx, cfg, router)
}

// setupLogging installs the JSON logger and announces the start.
func setupLogging(cfg *config.Config) {
	if cfg.LogLevel == "debug" && os.Getenv("PION_LOG_DEBUG") == "" {
		// Pion's ICE agent then logs every candidate pair it checks.
		_ = os.Setenv("PION_LOG_DEBUG", "ice")
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel(cfg)})))
	slog.Info("starting mnema-talk", "version", version.Current(), "revision", version.Commit(), "env", cfg.AppEnv)
}

// warnBroadTrustedProxies flags trusted proxy ranges that let any host in them
// forge X-Forwarded-For when the listener is reachable from outside.
func warnBroadTrustedProxies(cfg *config.Config) {
	broad := cfg.BroadTrustedProxies()
	if len(broad) == 0 || config.IsLoopbackAddr(cfg.BindAddr) {
		return
	}
	slog.Warn("TRUSTED_PROXY_CIDRS contains broad ranges and the listener is not loopback-only: any host in those ranges can forge X-Forwarded-For and evade rate limits and login lockout; list only your reverse proxy's address",
		"ranges", broad)
	opwarn.Raise(opwarn.TrustedProxyBroad, len(broad))
}

// warnInsecurePublicURL flags a production-like deployment whose PUBLIC_URL is
// plain http, which drops the Secure and __Host- cookie protections.
func warnInsecurePublicURL(cfg *config.Config) {
	if w := cfg.InsecurePublicURLWarning(); w != "" {
		slog.Warn(w, "public_url_scheme", "http")
		opwarn.Raise(opwarn.InsecurePublicURL, 0)
	}
}

// startVoice starts the SFU and returns it with the function that closes it.
// When it cannot start, voice is disabled: the SFU is nil and the close
// function does nothing.
func startVoice(ctx context.Context, cfg *config.Config) (*sfu.SFU, func(), error) {
	// Addresses browsers send media to: typically the public IP (or a
	// dynamic-DNS name for it) plus the LAN IP.
	announce, err := sfu.ResolveAnnounce(ctx, cfg.WebRTCAnnounce)
	if err != nil {
		return nil, nil, err
	}
	voice, err := sfu.NewSFU(cfg.WebRTCUDPPortMin, cfg.WebRTCUDPPortMax, announce, cfg.WebRTCSTUNURLs, sfu.WithUDPMuxPort(cfg.WebRTCUDPMuxPort))
	if err != nil {
		slog.Warn("webrtc sfu unavailable, voice disabled", "err", err)
		opwarn.Raise(opwarn.VoiceUnavailable, 0)
		return nil, func() {}, nil
	}
	closeVoice := func() {
		if err := voice.Close(); err != nil {
			slog.Warn("close SFU", "err", err)
		}
	}
	slog.Info("webrtc announce", "ips", announce, "ports", fmt.Sprintf("%d-%d", cfg.WebRTCUDPPortMin, cfg.WebRTCUDPPortMax), "udp_mux_port", cfg.WebRTCUDPMuxPort)
	if w := cfg.WebRTCReachabilityWarning(); w != "" {
		slog.Warn(w)
		opwarn.Raise(opwarn.WebRTCReachability, 0)
	}
	if n := cfg.WebRTCRelayNotice(); n != "" {
		slog.Info("webrtc relay: " + n)
	}
	if cfg.WebRTCUDPMuxPort == 0 {
		// Each peer takes its own port(s) from the range, so the range
		// bounds how many people can be in calls at once.
		ports := int(cfg.WebRTCUDPPortMax) - int(cfg.WebRTCUDPPortMin) + 1
		slog.Warn("WEBRTC_UDP_MUX_PORT=0: every media peer takes its own UDP port(s) from the range, which caps how many people can be in calls at once; leave it empty to share one port",
			"ports", ports)
		opwarn.Raise(opwarn.WebRTCUDPMuxDisabled, ports)
	}
	voice.KeepAnnounceCurrent(ctx, cfg.WebRTCAnnounce, 5*time.Minute)
	return voice, closeVoice, nil
}

// startUpdateChecker starts the release check against GitHub (admin System
// tab). Disabled with UPDATE_CHECK_ENABLED=false: then the server makes no
// request at all and the checker is nil.
func startUpdateChecker(ctx context.Context, cfg *config.Config) *update.Checker {
	if !cfg.UpdateCheck {
		return nil
	}
	updates := update.NewChecker(update.DefaultReleaseURL, version.Current())
	go updates.Run(ctx, 30*time.Second)
	return updates
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
