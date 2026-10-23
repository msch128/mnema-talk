package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// WebClientAPI is the contract number between a web app and this server. A
// desktop client runs a signed bundle of the web app and only connects when
// the bundle speaks the same number (doc/desktop-trusted-client.md). Raise it
// together with web/desktop-compat.json when the contract breaks.
const WebClientAPI = 1

// Health is the response of GET /api/health.
type Health struct {
	Status  string `json:"status" enums:"ok"`
	Version string `json:"version"`
	// WebClientAPI identifies the HTTPS web-client desktop integration contract.
	WebClientAPI int `json:"web_client_api"`
	// Features lists the optional features this server has, so a web app of a
	// newer release can hide what an older server lacks (for example "blocks").
	// A server of 1.0.x sends no list. See doc/api-stability.md.
	Features []string `json:"features" binding:"optional"`
}

// Features are the optional capabilities of this build, by name. Each feature
// package adds its flag here when its server side ships; names are public API
// and never change meaning.
var Features = []string{
	"blocks",
	"dm",
	"reports",
	"saved",
	"search.filters.v2",
	"threads.follow",
	"slash.builtins",
}

// healthCacheTTL is how long a health result is reused: the endpoint is
// public and unauthenticated, so callers must not be able to turn it into a
// stream of database pings.
const healthCacheTTL = 2 * time.Second

// healthCheck pings the database (and checks object storage) at most once
// per ttl; concurrent callers wait for the one check in flight.
type healthCheck struct {
	ping    func(context.Context) error
	storage func() error
	ttl     time.Duration

	mu      sync.Mutex
	checked time.Time
	err     *httpx.APIError
}

func newHealthCheck(p *db.Pool, storage func() error, ttl time.Duration) *healthCheck {
	ping := func(context.Context) error { return errors.New("no database") }
	if p != nil {
		ping = p.Ping
	}
	return &healthCheck{ping: ping, storage: storage, ttl: ttl}
}

// check returns nil when healthy, otherwise the error to answer with.
func (h *healthCheck) check(ctx context.Context) *httpx.APIError {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.checked.IsZero() && time.Since(h.checked) < h.ttl {
		return h.err
	}
	// Not tied to one caller: its result is shared with the others.
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	h.err = nil
	if err := h.ping(pctx); err != nil {
		h.err = httpx.ErrUnavailable("database unavailable")
	} else if h.storage != nil {
		if err := h.storage(); err != nil {
			h.err = httpx.ErrUnavailable("file storage unavailable")
		}
	}
	h.checked = time.Now()
	return h.err
}

// health handles GET /api/health. The result is cached for healthCacheTTL,
// and an object storage outage answers 503 as well.
//
// @Summary Health check
// @Description Pings the database.
// @ID getHealth
// @Tags System
// @Produce json
// @Success 200 {object} Health "Healthy."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/health [get]
func health(hc *healthCheck, version string) http.HandlerFunc {
	if version == "" {
		version = "dev"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := hc.check(r.Context()); err != nil {
			httpx.WriteError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, Health{Status: "ok", Version: version, WebClientAPI: WebClientAPI, Features: Features})
	}
}

// HealthHandler returns the health handler for the given dependencies.
func HealthHandler(deps Deps) http.HandlerFunc {
	return health(newHealthCheck(deps.DB, deps.StorageReady, healthCacheTTL), deps.Version)
}
