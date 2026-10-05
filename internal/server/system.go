package server

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/sfu"
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
	storage  func() error // nil: not checked
	hasStore bool
	version  string
	started  time.Time
}

// SystemStatus is the response of GET /api/admin/system.
type SystemStatus struct {
	Version SystemVersion `json:"version"`
	Health  SystemHealth  `json:"health"`
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
	WebSocketConnections int  `json:"websocket_connections"`
	OnlineUsers          int  `json:"online_users"`
	TURNConfigured       bool `json:"turn_configured"`
	STUNConfigured       bool `json:"stun_configured"`
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
	r.Get("/system", httpx.Handle(h.status))
}

// status handles GET /api/admin/system.
//
// @Summary System status
// @Description Version, health and load of the server for the admin System tab. Contains no secrets. Requires role admin (403 otherwise).
// @ID getSystemStatus
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} SystemStatus "System status."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
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
	return SystemStatus{
		Version: SystemVersion{Current: h.version, Revision: version.Commit(), GoVersion: version.GoVersion()},
		Health:  h.health(ctx),
	}
}

// health collects the snapshot. Failures show up as "not reachable"; their
// details go to the log only.
func (h *systemHandler) health(ctx context.Context) SystemHealth {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var out SystemHealth

	if h.db != nil && h.db.Ping(ctx) == nil {
		out.Database.Reachable = true
		var latest string
		var applied int
		if err := h.db.QueryRow(ctx, `SELECT COUNT(*), COALESCE(MAX(filename), '') FROM schema_migrations`).Scan(&applied, &latest); err == nil {
			out.Database.LatestMigration, out.Database.AppliedMigrations = latest, applied
		}
		if files, err := db.MigrationFiles(); err == nil && len(files) > applied {
			out.Database.PendingMigrations = len(files) - applied
		}
		_ = h.db.QueryRow(ctx, `
			SELECT COUNT(*), COALESCE(SUM(size_bytes), 0),
			       COALESCE(SUM(size_bytes) FILTER (WHERE s3_key LIKE 'avatars/%'), 0)
			FROM media WHERE NOT is_deleted`).Scan(&out.Storage.Files, &out.Storage.TotalBytes, &out.Storage.AvatarBytes)
		out.Storage.AttachmentBytes = out.Storage.TotalBytes - out.Storage.AvatarBytes
	}

	out.Storage.Configured = h.hasStore
	if h.hasStore {
		out.Storage.Reachable = h.storage == nil || h.storage() == nil
	}

	out.Voice.Enabled = h.sfu != nil
	if h.sfu != nil {
		st := h.sfu.Stats()
		out.Voice.MediaConnections, out.Voice.ScreenShares, out.Voice.Cameras = st.Peers, st.ScreenShares, st.Cameras
	}
	if h.hub != nil {
		st := h.hub.Stats()
		out.Voice.Rooms, out.Voice.Participants = st.VoiceRooms, st.VoiceParticipants
		out.Voice.WebSocketConnections, out.Voice.OnlineUsers = st.Connections, st.OnlineUsers
	}
	out.Voice.TURNConfigured = len(h.cfg.WebRTCTURNURLs) > 0
	out.Voice.STUNConfigured = len(h.cfg.WebRTCSTUNURLs) > 0

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	out.Runtime = RuntimeHealth{
		StartedAt:     h.started.UTC(),
		UptimeSeconds: int64(time.Since(h.started).Seconds()),
		GoVersion:     version.GoVersion(),
		Goroutines:    runtime.NumGoroutine(),
		MemAllocBytes: mem.Alloc,
		MemSysBytes:   mem.Sys,
	}
	return out
}
