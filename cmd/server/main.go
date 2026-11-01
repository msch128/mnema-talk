// Command server runs Mnema Talk: REST API, WebSocket hub, WebRTC SFU and the
// embedded web app in a single process.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/debugsrv"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/memlimit"
	"github.com/msch128/mnema-talk/internal/opwarn"
	"github.com/msch128/mnema-talk/internal/portconflict"
	"github.com/msch128/mnema-talk/internal/s3"
	"github.com/msch128/mnema-talk/internal/server"
	"github.com/msch128/mnema-talk/internal/setupcheck"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/stunprobe"
	"github.com/msch128/mnema-talk/internal/turnrelay"
	"github.com/msch128/mnema-talk/internal/update"
	"github.com/msch128/mnema-talk/internal/version"
)

func main() {
	if err := run(); err != nil {
		if errors.Is(err, errDoctorRed) {
			os.Exit(1)
		}
		var ce *config.ConfigError
		if errors.As(err, &ce) {
			os.Exit(reportConfigError(os.Stderr, ce, isServerStart(os.Args)))
		}
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// configErrorDelay is how long a server that refuses to start for its settings
// waits before it exits. Compose restarts the container (restart:
// unless-stopped); without the pause it would loop in milliseconds and bury
// the message in repeats. A test sets it to zero.
var configErrorDelay = 5 * time.Second

// isServerStart reports whether the command line starts the server rather than
// one of the subcommands (setup, doctor, unlock, reset-2fa).
func isServerStart(args []string) bool {
	if len(args) < 2 || strings.HasPrefix(args[1], "-") {
		return true
	}
	switch args[1] {
	case "setup", "doctor", "unlock", "reset-2fa":
		return false
	}
	return true
}

// reportConfigError prints the list of settings to fix as plain text, the
// first thing in the container log, and returns the exit status. A server
// start pauses first so a restart loop stays readable. Nothing listens in the
// meantime: a "config error" web page would be an unauthenticated surface and
// a half-alive process every health check has to special-case.
func reportConfigError(w io.Writer, ce *config.ConfigError, server bool) int {
	fmt.Fprintln(w, ce.Error())
	if server {
		fmt.Fprintf(w, "\nEdit .env, then run: docker compose up -d   (the full list of settings: doc/operations.md; check an .env with: docker compose run --rm app mnema-talk doctor)\nThis container exits now and is restarted by Docker; the message above repeats until the settings are fixed.\n")
		time.Sleep(configErrorDelay)
	}
	return config.ExitCode
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if len(os.Args) > 1 && os.Args[1] == "unlock" {
		return unlockCommand(ctx, os.Args[2:], os.Stdout)
	}
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		return setupCommand(ctx, os.Args[2:], os.Stdin, os.Stdout)
	}
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		return doctorCommand(ctx, os.Args[2:], os.Stdout)
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
	memlimit.Apply(slog.Default())
	if addr, err := debugsrv.Start(ctx, cfg.PprofAddr); err != nil {
		return err
	} else if addr != "" {
		slog.Info("pprof listening", "addr", addr)
	}
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
	media.StartStagedSweep(ctx, pool, store)

	setup := &setupcheck.Runtime{}
	voice, closeVoice, err := startVoice(ctx, cfg, setup)
	if err != nil {
		return err
	}
	defer closeVoice()

	relay, closeRelay := startTURN(cfg, voice)
	defer closeRelay()
	if n := cfg.WebRTCRelayNoticeWith(relay != nil && relay.Status().TCP); n != "" && voice != nil {
		slog.Info("webrtc relay: " + n)
	}

	updates := startUpdateChecker(ctx, cfg)
	startSetupChecks(ctx, cfg, setup, pool.Ping, store.Ready, relay)

	router, err := server.NewRouter(server.Deps{Config: cfg, DB: pool, Store: store, StorageReady: store.Ready, SFU: voice, TURN: relay, Version: version.Current(), Updates: updates, Context: ctx, Setup: setup})
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
func startVoice(ctx context.Context, cfg *config.Config, setup *setupcheck.Runtime) (*sfu.SFU, func(), error) {
	// Addresses browsers send media to: typically the public IP (or a
	// dynamic-DNS name for it) plus the LAN IP. Without WEBRTC_NAT_1TO1_IP a
	// production server works them out itself.
	host := setupcheck.SystemHost()
	ann := setupcheck.ResolveAnnouncement(ctx, cfg, stunprobe.Detect, host)
	setupcheck.JudgeNetwork(ctx, cfg, ann, host, stunprobe.Detect6, setup)
	announce, err := sfu.ResolveAnnounce(ctx, ann.Entries)
	if err != nil {
		return nil, nil, err
	}
	setup.SetAnnounce(ann.Source, announce, ann.DetectErr)
	voice, err := sfu.NewSFU(cfg.WebRTCUDPPortMin, cfg.WebRTCUDPPortMax, announce, cfg.WebRTCSTUNURLs, sfu.WithUDPMuxPort(cfg.WebRTCUDPMuxPort))
	if err != nil {
		slog.Warn("webrtc sfu unavailable, voice disabled", "err", portconflict.Wrap(err, fmt.Sprintf("%d-%d", cfg.WebRTCUDPPortMin, cfg.WebRTCUDPPortMax), "UDP", "WEBRTC_UDP_PORT_MIN and WEBRTC_UDP_PORT_MAX (and the router forward)"))
		opwarn.Raise(opwarn.VoiceUnavailable, 0)
		return nil, func() {}, nil
	}
	if cfg.VoiceUploadLimitMbit > 0 {
		voice.StartUploadGovernor(ctx, cfg.VoiceUploadLimitMbit)
		slog.Info("voice upload limit on: viewers on automatic are stepped down the layers to stay under it", "limit_mbit", cfg.VoiceUploadLimitMbit)
	}
	voice.SetICEConnectedHandler(func(ev sfu.ICEConnected) {
		if provesOutsideReach(ev, voice.AnnouncedIPs()) {
			setup.RecordICEConnected(ev.Remote, time.Now())
		}
	})
	closeVoice := func() {
		if err := voice.Close(); err != nil {
			slog.Warn("close SFU", "err", err)
		}
	}
	slog.Info("webrtc announce", "ips", announce, "ports", fmt.Sprintf("%d-%d", cfg.WebRTCUDPPortMin, cfg.WebRTCUDPPortMax), "udp_mux_port", cfg.WebRTCUDPMuxPort)
	if w := cfg.WebRTCReachabilityWarning(); w != "" && len(announce) == 0 {
		slog.Warn(w)
		opwarn.Raise(opwarn.WebRTCReachability, 0)
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
	if ann.Source == setupcheck.SourceDetected {
		voice.KeepAnnounceDetected(ctx, 10*time.Minute, ann.LAN, func(ctx context.Context) (string, error) {
			return setupcheck.DetectPublicIP(ctx, cfg)
		})
	}
	return voice, closeVoice, nil
}

// startTURN starts the TURN relay built into the server when the settings ask
// for it (WEBRTC_BUILTIN_TURN) and returns it with the function that stops it.
// It never fails the start: a relay that cannot run is logged, raised as an
// operator warning, and the server carries on without it.
func startTURN(cfg *config.Config, voice *sfu.SFU) (*turnrelay.Server, func()) {
	none := func() {}
	if !cfg.BuiltinTURNEnabled() {
		slog.Info("built-in TURN relay off", "mode", cfg.WebRTCBuiltinTURN, "external_turn", len(cfg.WebRTCTURNURLs) > 0)
		return nil, none
	}
	if voice == nil {
		return nil, none // voice is already reported as unavailable
	}
	port := int(cfg.WebRTCBuiltinTURNPort)
	relay, err := turnrelay.Start(turnrelay.Options{
		Port:       port,
		PublicHost: cfg.TURNPublicHost(),
		Target:     func() turnrelay.Target { return turnTarget(voice.Endpoints()) },
	})
	if err != nil {
		slog.Warn("built-in TURN relay unavailable, continuing without it: members behind strict firewalls may get no voice",
			"port", port, "err", portconflict.Wrap(err, strconv.Itoa(port), "UDP/TCP", "WEBRTC_BUILTIN_TURN_PORT"))
		opwarn.Raise(opwarn.TURNUnavailable, port)
		if cfg.WebRTCReachabilityWarningWith(false) != "" {
			opwarn.Raise(opwarn.WebRTCReachability, 0)
		}
		return nil, none
	}
	st := relay.Status()
	if !st.UDP || !st.TCP {
		slog.Warn("built-in TURN relay could not listen on every transport"+turnConflictHint(st, port),
			"port", port, "udp", st.UDP, "tcp", st.TCP, "udp_error", st.UDPError, "tcp_error", st.TCPErr)
		opwarn.Raise(opwarn.TURNUnavailable, port)
	}
	slog.Info("built-in TURN relay listening", "port", port, "udp", st.UDP, "tcp", st.TCP, "host", cfg.TURNPublicHost())
	if cfg.BuiltinTURNWithExternal() {
		slog.Info("WEBRTC_BUILTIN_TURN=on with WEBRTC_TURN_URLS set: members are offered both TURN servers")
	}
	return relay, func() {
		if err := relay.Close(); err != nil {
			slog.Warn("close built-in TURN relay", "err", err)
		}
	}
}

// turnConflictHint names the port conflict when a relay transport failed
// because another program holds the port.
func turnConflictHint(st turnrelay.Status, port int) string {
	if portconflict.InUseText(st.UDPError) || portconflict.InUseText(st.TCPErr) {
		return ": " + portconflict.Hint(strconv.Itoa(port), "UDP/TCP", "WEBRTC_BUILTIN_TURN_PORT")
	}
	return ""
}

// turnTarget tells the relay where the SFU's media sockets are.
func turnTarget(ep sfu.Endpoints) turnrelay.Target {
	return turnrelay.Target{LocalIPs: ep.Local, Announced: ep.Announced, PortMin: ep.PortMin, PortMax: ep.PortMax}
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

// provesOutsideReach says whether a finished ICE connection shows that the
// voice port is reachable from outside. A relayed connection does not: the
// built-in TURN relay's own address is the server's public one. Neither does a
// peer-reflexive remote at one of the server's own announced addresses, which
// is the relay seen from the server.
func provesOutsideReach(ev sfu.ICEConnected, announced []string) bool {
	if ev.RemoteType == "relay" {
		return false
	}
	for _, a := range announced {
		if ip, err := netip.ParseAddr(a); err == nil && ip == ev.Remote {
			return false
		}
	}
	return true
}
