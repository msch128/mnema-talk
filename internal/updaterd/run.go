package updaterd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/msch128/mnema-talk/internal/imageverify"
	"github.com/msch128/mnema-talk/internal/safego"
)

// HTTPRecreator calls Watchtower's HTTP API. Watchtower runs with
// WATCHTOWER_NO_PULL: it recreates the app only if the local image behind the
// app's tag changed, which is the image this updater has just verified and
// tagged. It never fetches anything itself.
type HTTPRecreator struct {
	Endpoint string
	Token    string
	Client   *http.Client
}

// Recreate implements Recreator.
func (h *HTTPRecreator) Recreate(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.Endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.Token)
	res, err := h.Client.Do(req)
	if err != nil {
		return fmt.Errorf("recreator unreachable: %w", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
		return fmt.Errorf("recreator answered HTTP %d", res.StatusCode)
	}
	return nil
}

// Config is the updater's environment (documented in doc/operations.md).
type Config struct {
	Token        string
	Listen       string
	Socket       string
	Scope        string
	RecreatorURL string
	TrustedRoot  string
	CacheDir     string
	Policy       imageverify.Policy
}

func envOr(get func(string) string, key, def string) string {
	if v := strings.TrimSpace(get(key)); v != "" {
		return v
	}
	return def
}

// origins are the default release workflows (after and before the move to
// mnema-xyz), or exactly one origin when UPDATER_CERT_IDENTITY_REGEXP and
// UPDATER_SOURCE_URL are set (a fork's own workflow). Setting only one of the
// two is refused: filling the other from a default would pair one owner's
// signature with another owner's provenance.
func origins(get func(string) string) ([]imageverify.Origin, error) {
	identity := envOr(get, "UPDATER_CERT_IDENTITY_REGEXP", "")
	source := envOr(get, "UPDATER_SOURCE_URL", "")
	switch {
	case identity == "" && source == "":
		return imageverify.DefaultOrigins(), nil
	case identity == "" || source == "":
		return nil, errors.New("set UPDATER_CERT_IDENTITY_REGEXP and UPDATER_SOURCE_URL together (the workflow that signs and the repository it builds from)")
	}
	return []imageverify.Origin{{IdentityRegexp: identity, Source: source}}, nil
}

// ParseConfig reads and validates the environment.
func ParseConfig(get func(string) string) (Config, error) {
	def := imageverify.DefaultPolicy()
	repository := envOr(get, "UPDATER_IMAGE_REPOSITORY", "")
	mirrors := def.Mirrors
	if repository != "" {
		mirrors = nil // an explicit repository is the only one trusted
	} else {
		repository = def.Repository
	}
	c := Config{
		Token:        strings.TrimSpace(get("UPDATER_TOKEN")),
		Listen:       envOr(get, "UPDATER_LISTEN", ":8080"),
		Socket:       envOr(get, "UPDATER_DOCKER_SOCKET", "/var/run/docker.sock"),
		Scope:        envOr(get, "UPDATER_SCOPE", "mnema-talk"),
		RecreatorURL: envOr(get, "UPDATER_RECREATOR_URL", "http://mnema-recreator:8080"),
		TrustedRoot:  envOr(get, "UPDATER_TRUSTED_ROOT", ""),
		CacheDir:     envOr(get, "UPDATER_TUF_CACHE", "/tmp/tuf"),
		Policy: imageverify.Policy{
			Repository: repository,
			Mirrors:    mirrors,
			Issuer:     envOr(get, "UPDATER_CERT_ISSUER", def.Issuer),
		},
	}
	o, err := origins(get)
	if err != nil {
		return c, err
	}
	c.Policy.Origins = o
	if len(c.Token) < 32 {
		return c, errors.New("UPDATER_TOKEN must be set (at least 32 characters)")
	}
	if err := c.Policy.Validate(); err != nil {
		return c, err
	}
	u, err := url.Parse(c.RecreatorURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return c, errors.New("UPDATER_RECREATOR_URL must be an http(s) URL")
	}
	return c, nil
}

// NewServer wires a Server from a Config.
func NewServer(c Config, log *slog.Logger) (*Server, error) {
	trust := imageverify.LiveTrust(c.CacheDir)
	if c.TrustedRoot != "" {
		trust = imageverify.FileTrust(c.TrustedRoot)
	}
	v, err := imageverify.New(c.Policy, trust)
	if err != nil {
		return nil, err
	}
	return &Server{
		Token:    c.Token,
		Scope:    c.Scope,
		Engine:   NewDocker(c.Socket),
		Verifier: v,
		Recreator: &HTTPRecreator{
			Endpoint: strings.TrimRight(c.RecreatorURL, "/") + "/v1/update",
			Token:    c.Token,
			Client:   &http.Client{Timeout: swapTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		},
		Log: log,
	}, nil
}

// Run is the process entry point: it returns the exit code.
func Run(getenv func(string) string, stderr io.Writer) int {
	log := slog.New(slog.NewJSONHandler(stderr, nil))
	cfg, err := ParseConfig(getenv)
	if err != nil {
		log.Error("configuration", "err", err)
		return 2
	}
	srv, err := NewServer(cfg, log)
	if err != nil {
		log.Error("setup", "err", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	hs := &http.Server{Addr: cfg.Listen, Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	stopped := make(chan struct{})
	safego.Go("updater_shutdown", safego.Crash, func() {
		defer close(stopped)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(shutdown)
	})
	log.Info("updater ready", "listen", cfg.Listen, "repository", cfg.Policy.Repository, "mirrors", cfg.Policy.Mirrors, "origins", cfg.Policy.Origins)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		return 1
	}
	// ListenAndServe returns as soon as Shutdown starts: wait for it to drain
	// the requests in flight, then give a running swap time to finish so the
	// process does not exit between retagging and recreating.
	<-stopped
	if !srv.WaitIdle(swapGrace) {
		log.Error("SWAP ABORTED BY SHUTDOWN: the update was still running; the local tag may point at the new image while the app was not recreated")
		return 1
	}
	return 0
}

// compile-time check that the real client satisfies the interfaces.
var (
	_ Engine    = (*Docker)(nil)
	_ Recreator = (*HTTPRecreator)(nil)
)
