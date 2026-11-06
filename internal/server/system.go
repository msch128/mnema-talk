package server

import (
	"context"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/community"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/mailer"
	"github.com/msch128/mnema-talk/internal/opwarn"
	"github.com/msch128/mnema-talk/internal/setupcheck"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/turnrelay"
	"github.com/msch128/mnema-talk/internal/update"
	"github.com/msch128/mnema-talk/internal/version"
	"github.com/msch128/mnema-talk/internal/ws"
)

// systemHandler serves the admin System tab: version, health and (see
// update.go) the update check and the optional self-update. Everything here
// is admin-only and free of secrets: no tokens, no connection strings, no
// environment dumps, no internal addresses.
type systemHandler struct {
	cfg      *config.Config
	db       *db.Pool
	hub      *ws.Hub
	sfu      *sfu.SFU
	turn     *turnrelay.Server // nil: the built-in TURN relay is not running
	storage  func() error      // nil: not checked
	hasStore bool
	version  string
	started  time.Time
	updates  *update.Checker // nil: update check disabled
	events   events.Publisher
	ops      *opsSampler
	// warnings is the operator-warning registry (opwarn.Default in production).
	warnings *opwarn.Registry
	// setup is what the server learned at startup, for the setup checks.
	setup *setupcheck.Runtime

	// mail is nil when SMTP is not configured.
	mail          *mailer.Mailer
	names         *community.Names
	testMailLimit *httpx.RateLimiter
	// probeLimit bounds connection tests per member; wsReportLimit the
	// WebSocket reports of the status tab per admin.
	probeLimit, wsReportLimit *httpx.RateLimiter
	// speed bounds the download speed test: per member and in parallel.
	speed speedTest

	selfMu sync.Mutex
	self   selfUpdate
}

// SystemStatus is the response of GET /api/admin/system.
type SystemStatus struct {
	Version SystemVersion `json:"version"`
	Health  SystemHealth  `json:"health"`
	Update  UpdateStatus  `json:"update"`
	// Mail is the outgoing mail setup (account e-mails need it).
	Mail       MailStatus       `json:"mail"`
	SelfUpdate SelfUpdateStatus `json:"self_update"`
	// Warnings are configuration or deployment problems the server noticed
	// (stable codes; the web app explains each). Empty when all is well.
	Warnings []opwarn.Warning `json:"warnings"`
}

// SystemVersion describes the running build.
type SystemVersion struct {
	// Current is the release version ("dev" for a build without one).
	Current string `json:"current"`
	// Revision is the short git commit of the build; empty when unknown.
	Revision  string `json:"revision"`
	GoVersion string `json:"go_version"`
}

// SystemHealth is a snapshot of the server's dependencies and load.
type SystemHealth struct {
	Database DatabaseHealth `json:"database"`
	Storage  StorageHealth  `json:"storage"`
	Voice    VoiceHealth    `json:"voice"`
	Runtime  RuntimeHealth  `json:"runtime"`
}

// DatabaseHealth reports reachability and the schema version.
type DatabaseHealth struct {
	Reachable bool `json:"reachable"`
	// LatestMigration is the newest applied migration file, e.g. "0012_message_count.sql".
	LatestMigration   string `json:"latest_migration"`
	AppliedMigrations int    `json:"applied_migrations"`
	// PendingMigrations counts embedded migrations not applied yet (0 when
	// the server started normally: it migrates before serving).
	PendingMigrations int `json:"pending_migrations"`
}

// StorageHealth reports the object store and how much it holds, counted
// from the database (the bucket is never listed).
type StorageHealth struct {
	Configured      bool  `json:"configured"`
	Reachable       bool  `json:"reachable"`
	Files           int64 `json:"files"`
	TotalBytes      int64 `json:"total_bytes"`
	AttachmentBytes int64 `json:"attachment_bytes"`
	AvatarBytes     int64 `json:"avatar_bytes"`
	// UsageUnavailable is true when the usage could not be read in time; the
	// counts above are then zero because they are unknown.
	UsageUnavailable bool `json:"usage_unavailable,omitempty" binding:"optional"`
}

// VoiceHealth reports the SFU and the live calls.
type VoiceHealth struct {
	// Enabled is false when the SFU could not start (voice disabled).
	Enabled bool `json:"enabled"`
	// Rooms and Participants are the calls as members see them.
	Rooms        int `json:"rooms"`
	Participants int `json:"participants"`
	// MediaConnections are the SFU's peer connections.
	MediaConnections int `json:"media_connections"`
	ScreenShares     int `json:"screen_shares"`
	Cameras          int `json:"cameras"`
	// WebSocketConnections are open realtime connections; OnlineUsers the
	// distinct users behind them.
	WebSocketConnections int `json:"websocket_connections"`
	OnlineUsers          int `json:"online_users"`
	// TURNConfigured is true when members are offered any TURN relay: the
	// external one (WEBRTC_TURN_URLS) or the one built into the server.
	TURNConfigured bool `json:"turn_configured"`
	STUNConfigured bool `json:"stun_configured"`
	// BuiltinTURN reports the TURN relay built into the server.
	BuiltinTURN BuiltinTURNHealth `json:"builtin_turn"`
}

// BuiltinTURNHealth reports the TURN relay built into the server
// (WEBRTC_BUILTIN_TURN).
type BuiltinTURNHealth struct {
	// Mode is the setting: auto (on while no external TURN is configured),
	// on or off.
	Mode string `json:"mode" enums:"auto,on,off"`
	// Enabled is whether the setting asks for the relay now; Active whether it
	// is running and offered to members.
	Enabled bool `json:"enabled"`
	Active  bool `json:"active"`
	// Port is the UDP and TCP port it is configured to listen on.
	Port int `json:"port"`
	// UDP and TCP say which transports are listening; one can fail to bind
	// while the other works.
	UDP bool `json:"udp"`
	TCP bool `json:"tcp"`
	// Allocations are the relayed sessions open now.
	Allocations int `json:"allocations"`
	// Host is the name members are told to reach the relay at (from
	// PUBLIC_URL); empty when none is known.
	Host string `json:"host"`
	// Reachability is a best-effort self-test from this machine to Host:Port:
	// reachable, unreachable (also what a router without NAT loopback shows
	// for a forwarded port) or unknown (not run, or nothing to test).
	Reachability string `json:"reachability" enums:"reachable,unreachable,unknown"`
}

// RuntimeHealth reports process resources.
type RuntimeHealth struct {
	StartedAt     time.Time `json:"started_at" format:"date-time"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	GoVersion     string    `json:"go_version"`
	Goroutines    int       `json:"goroutines"`
	// MemAllocBytes is live heap memory; MemSysBytes what the Go runtime
	// obtained from the OS.
	MemAllocBytes uint64 `json:"mem_alloc_bytes"`
	MemSysBytes   uint64 `json:"mem_sys_bytes"`
}

func (h *systemHandler) mountAdmin(r chi.Router) {
	// No role grants the system area: it is the Owner's.
	r = r.With(auth.RequireOwner)
	r.Get("/system", httpx.Handle(h.status))
	r.Get("/system/ops", httpx.Handle(h.opsStatus))
	r.Get("/system/update", httpx.Handle(h.getUpdate))
	r.Post("/system/check", httpx.Handle(h.checkNow))
	r.Post("/system/update", httpx.Handle(h.startSelfUpdate))
	r.Post("/system/mail/test", httpx.Handle(h.sendTestMail))
}

// MountAdmin mounts the admin system routes on r.
func (h *systemHandler) MountAdmin(r chi.Router) {
	h.mountAdmin(r)
}

// status handles GET /api/admin/system.
//
// @Summary System status
// @Description Version, health and load of the server for the admin System tab. Contains no secrets. Owner only (403 otherwise).
// @ID getSystemStatus
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} SystemStatus "System status."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/system [get]
func (h *systemHandler) status(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, h.snapshot(r.Context()))
	return nil
}

func (h *systemHandler) snapshot(ctx context.Context) SystemStatus {
	upd := h.updateStatus()
	return SystemStatus{
		Version:    SystemVersion{Current: h.version, Revision: version.Commit(), GoVersion: version.GoVersion()},
		Health:     h.health(ctx),
		Update:     upd,
		Mail:       h.mailStatus(),
		SelfUpdate: h.selfUpdateStatus(upd),
		Warnings:   h.warnings.List(),
	}
}

// health collects the snapshot. Failures show up as "not reachable"; their
// details go to the log only.
func (h *systemHandler) health(ctx context.Context) SystemHealth {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	dbH, storH := h.dbHealth(ctx)
	return SystemHealth{
		Database: dbH,
		Storage:  h.storageHealth(storH),
		Voice:    h.voiceHealth(),
		Runtime:  h.runtimeHealth(),
	}
}

func (h *systemHandler) dbHealth(ctx context.Context) (DatabaseHealth, StorageHealth) {
	var dbH DatabaseHealth
	var storH StorageHealth
	if h.db == nil || h.db.Ping(ctx) != nil {
		return dbH, storH
	}
	dbH.Reachable = true
	if m, err := migrationStatus(ctx, h.db); err != nil {
		slog.Warn("system health: migration status unavailable", "error", err)
	} else {
		dbH.LatestMigration, dbH.AppliedMigrations, dbH.PendingMigrations = m.latest, m.applied, m.pending
	}
	if t, err := mediaTotals(ctx, h.db); err != nil {
		slog.Warn("system health: storage usage unavailable", "error", err)
		storH.UsageUnavailable = true
	} else {
		storH.Files, storH.TotalBytes, storH.AvatarBytes = t.files, t.totalBytes, t.avatarBytes
		storH.AttachmentBytes = t.totalBytes - t.avatarBytes
	}
	return dbH, storH
}

// migrations is what the schema_migrations table says against the embedded
// migration files.
type migrations struct {
	latest  string
	applied int
	pending int
}

// migrationStatus counts the applied migrations and those still pending. It
// returns an error rather than a guess when the table cannot be read, so a
// slow database never shows every migration as pending.
func migrationStatus(ctx context.Context, q db.Querier) (migrations, error) {
	var m migrations
	if err := q.QueryRow(ctx, `SELECT COUNT(*), COALESCE(MAX(filename), '') FROM schema_migrations`).Scan(&m.applied, &m.latest); err != nil {
		return migrations{}, err
	}
	if files, err := db.MigrationFiles(); err == nil && len(files) > m.applied {
		m.pending = len(files) - m.applied
	}
	return m, nil
}

// mediaUsage is what the live (not deleted) media rows hold.
type mediaUsage struct {
	files, totalBytes, avatarBytes int64
}

// mediaTotals sums the live media rows; avatarBytes is the share of avatars
// and banners.
func mediaTotals(ctx context.Context, q db.Querier) (mediaUsage, error) {
	var t mediaUsage
	err := q.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(size_bytes), 0),
		       COALESCE(SUM(size_bytes) FILTER (WHERE s3_key LIKE 'avatars/%' OR s3_key LIKE 'banners/%'), 0)
		FROM media WHERE NOT is_deleted`).Scan(&t.files, &t.totalBytes, &t.avatarBytes)
	return t, err
}

func (h *systemHandler) storageHealth(base StorageHealth) StorageHealth {
	base.Configured = h.hasStore
	if h.hasStore {
		base.Reachable = h.storage == nil || h.storage() == nil
	}
	return base
}

func (h *systemHandler) voiceHealth() VoiceHealth {
	var v VoiceHealth
	v.Enabled = h.sfu != nil
	if h.sfu != nil {
		st := h.sfu.Stats()
		v.MediaConnections, v.ScreenShares, v.Cameras = st.Peers, st.ScreenShares, st.Cameras
	}
	if h.hub != nil {
		st := h.hub.Stats()
		v.Rooms, v.Participants = st.VoiceRooms, st.VoiceParticipants
		v.WebSocketConnections, v.OnlineUsers = st.Connections, st.OnlineUsers
	}
	v.BuiltinTURN = h.builtinTURNHealth()
	v.TURNConfigured = len(h.cfg.WebRTCTURNURLs) > 0 || v.BuiltinTURN.Active
	v.STUNConfigured = len(h.cfg.WebRTCSTUNURLs) > 0
	return v
}

func (h *systemHandler) builtinTURNHealth() BuiltinTURNHealth {
	b := BuiltinTURNHealth{
		Mode:         h.cfg.WebRTCBuiltinTURN,
		Enabled:      h.cfg.BuiltinTURNEnabled(),
		Port:         int(h.cfg.WebRTCBuiltinTURNPort),
		Host:         h.cfg.TURNPublicHost(),
		Reachability: "unknown",
	}
	if b.Mode == "" {
		b.Mode = config.BuiltinTURNAuto
	}
	if h.turn == nil {
		return b
	}
	st := h.turn.Status()
	b.Active, b.UDP, b.TCP, b.Allocations = true, st.UDP, st.TCP, st.Allocations
	if st.Probe != "" {
		b.Reachability = st.Probe
	}
	return b
}

func (h *systemHandler) runtimeHealth() RuntimeHealth {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return RuntimeHealth{
		StartedAt:     h.started.UTC(),
		UptimeSeconds: int64(time.Since(h.started).Seconds()),
		GoVersion:     version.GoVersion(),
		Goroutines:    runtime.NumGoroutine(),
		MemAllocBytes: mem.Alloc,
		MemSysBytes:   mem.Sys,
	}
}
