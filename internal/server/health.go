package server

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/internal/community"
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
	Status string `json:"status" enums:"ok"`
	ServerInfo
}

// HealthUnavailable is the 503 answer of GET /api/health: the error every
// other route answers with, and beside it what the server says about itself
// in a 200. A client that only needs to know what it talks to (the feature
// list, the desktop contract) reads it from either answer.
type HealthUnavailable struct {
	Error  *httpx.APIError `json:"error"`
	Status string          `json:"status" enums:"unavailable"`
	// Unavailable names every dependency that is down. Known values are
	// "database" and "storage"; a later server may name more, so a client
	// ignores the names it does not know. With only "storage" in it, uploads,
	// attachments and pictures fail while chat and Talks work.
	Unavailable []string `json:"unavailable"`
	ServerInfo
}

// Liveness is the response of GET /api/health/live.
type Liveness struct {
	Status string `json:"status" enums:"ok"`
}

// ServerInfo is what a server says about itself, in the 200 and the 503
// answer of GET /api/health alike.
type ServerInfo struct {
	Version string `json:"version"`
	// WebClientAPI identifies the HTTPS web-client desktop integration contract.
	WebClientAPI int `json:"web_client_api"`
	// CommunityName is what this server calls itself: the name the Owner set
	// in the welcome settings, else the host it is served from. It is public
	// on purpose, so a client can label a server before anyone signs in.
	CommunityName string `json:"community_name" binding:"optional"`
	// CommunityIcon is the version of the community icon the Owner set, left
	// out while there is none. GET /api/community/icon serves it (public); the
	// version changes with every upload, so a client refetches when it differs
	// from the one it has.
	CommunityIcon string `json:"community_icon,omitempty" binding:"optional"`
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
	"forward",
	"search.filters.v2",
	"search.filters.v3",
	"threads.follow",
	"slash.builtins",
	"profile.banner",
	"profile.avatar_remove",
	"status.expiry",
	"reminders",
	"messages.scheduled",
	"polls.anonymous",
	"channels.auto_delete",
	"talks.lobby",
	"automod",
	"moderation.history",
	"ops.status",
	"import.discord",
	"talks.stage",
	"talks.reactions",
	"channels.forum",
	"events",
	"events.roles",
	"events.recurrence",
	"wiki",
	"notify.levels.v2",
	"dm.v2",
	// "messages.nonce": POST /api/channels/{id}/messages accepts a client nonce
	// and answers a repeat of it with the first message. Older servers reject
	// the unknown field, so a client sends it only when this is listed.
	"messages.nonce",
	// "reports.open_count": GET /api/admin/reports/open-count. Older servers
	// read that path as a report id and answer 400, so a client asks for the
	// count only when this is listed.
	"reports.open_count",
	// "messages.lifetime": messages, uploads, scheduled messages and bot
	// posts take lifetime_seconds and carry expires_at; GET
	// /api/message-settings says whether and how long (doc/timed-messages.md).
	"messages.lifetime",
	// "channels.auto_delete.v2": auto-delete in hours with a scope and a
	// community default, and the dry runs before a change.
	"channels.auto_delete.v2",
}

// healthCacheTTL is how long a health result is reused: the endpoint is
// public and unauthenticated, so callers must not be able to turn it into a
// stream of database pings.
const healthCacheTTL = 2 * time.Second

// The dependencies GET /api/health names in a 503.
const (
	depDatabase = "database"
	depStorage  = "storage"
)

// healthCheck pings the database and checks object storage at most once per
// ttl; concurrent callers wait for the one check in flight.
type healthCheck struct {
	ping    func(context.Context) error
	storage func() error
	ttl     time.Duration

	mu      sync.Mutex
	checked time.Time
	down    []string
}

func newHealthCheck(p *db.Pool, storage func() error, ttl time.Duration) *healthCheck {
	ping := func(context.Context) error { return errors.New("no database") }
	if p != nil {
		ping = p.Ping
	}
	return &healthCheck{ping: ping, storage: storage, ttl: ttl}
}

// check returns the dependencies that are down, none when healthy.
func (h *healthCheck) check(ctx context.Context) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.checked.IsZero() && time.Since(h.checked) < h.ttl {
		return h.down
	}
	// Not tied to one caller: its result is shared with the others.
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	h.down = nil
	if err := h.ping(pctx); err != nil {
		h.down = append(h.down, depDatabase)
	}
	if h.storage != nil && h.storage() != nil {
		h.down = append(h.down, depStorage)
	}
	h.checked = time.Now()
	return h.down
}

// unavailableError is the error a 503 carries: the database when it is down
// (nothing works then), else file storage.
func unavailableError(down []string) *httpx.APIError {
	if slices.Contains(down, depDatabase) {
		return httpx.ErrUnavailable("database unavailable")
	}
	return httpx.ErrUnavailable("file storage unavailable")
}

// health handles GET /api/health. The result is cached for healthCacheTTL,
// and an object storage outage answers 503 as well.
//
// @Summary Health check
// @Description Pings the database and checks object storage; the result is cached for 2 s.
// @Description A 503 carries the usual error and, beside it, everything the 200 says about the server, plus the dependencies that are down in `unavailable`.
// @Description With only `storage` in that list, uploads, attachments and pictures fail while chat and Talks keep working. An older server answers a 503 with the error alone.
// @ID getHealth
// @Tags System
// @Produce json
// @Success 200 {object} Health "Healthy."
// @Failure 503 {object} HealthUnavailable "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/health [get]
func health(hc *healthCheck, version string, names *community.Names) http.HandlerFunc {
	if version == "" {
		version = "dev"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		info := ServerInfo{Version: version, WebClientAPI: WebClientAPI, Features: Features}
		down := hc.check(r.Context())
		if slices.Contains(down, depDatabase) {
			// No read that would wait for the database: the last known name does.
			info.CommunityName, info.CommunityIcon = names.Cached()
		} else {
			info.CommunityName, info.CommunityIcon = names.Get(r.Context()), names.Icon(r.Context())
		}
		if len(down) > 0 {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, HealthUnavailable{Error: unavailableError(down), Status: "unavailable", Unavailable: down, ServerInfo: info})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, Health{Status: "ok", ServerInfo: info})
	}
}

// live handles GET /api/health/live.
//
// @Summary Liveness check
// @Description Answers 200 whenever the process serves requests. It asks neither the database nor object storage, so it stays 200 while GET /api/health answers 503.
// @Description For orchestrators that restart a container on a failed probe; whether the server is ready for members is what GET /api/health says.
// @ID getHealthLive
// @Tags System
// @Produce json
// @Success 200 {object} Liveness "The process serves requests."
// @Router /api/health/live [get]
func live(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, Liveness{Status: "ok"})
}

// HealthHandler returns the health handler for the given dependencies.
func HealthHandler(deps Deps) http.HandlerFunc {
	names := deps.Names
	if names == nil && deps.DB != nil {
		names = community.New(deps.DB, deps.Config.PublicURL)
	}
	return health(newHealthCheck(deps.DB, deps.StorageReady, healthCacheTTL), deps.Version, names)
}
