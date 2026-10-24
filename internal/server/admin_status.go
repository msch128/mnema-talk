package server

import (
	"context"
	"net/http"
	"time"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/opwarn"
	"github.com/msch128/mnema-talk/internal/version"
)

// OperatorStatus is the response of GET /api/admin/status.
type OperatorStatus struct {
	Version       SystemVersion    `json:"version"`
	Update        UpdateStatus     `json:"update"`
	AutoDeploy    SelfUpdateStatus `json:"auto_deploy"`
	Database      DatabaseStatus   `json:"database"`
	Storage       StorageStatus    `json:"storage"`
	DiskFreeBytes *uint64          `json:"disk_free_bytes,omitempty" binding:"optional"`
	Backup        BackupStatus     `json:"backup"`
	Activity      ActivityStatus   `json:"activity"`
	Warnings      []opwarn.Warning `json:"warnings"`
}

// DatabaseStatus reports reachability, database size and migration progress.
type DatabaseStatus struct {
	Reachable         bool   `json:"reachable"`
	SizeBytes         *int64 `json:"size_bytes,omitempty" binding:"optional"`
	LatestMigration   string `json:"latest_migration"`
	AppliedMigrations int    `json:"applied_migrations"`
	PendingMigrations int    `json:"pending_migrations"`
}

// StorageStatus reports object storage reachability and total used bytes.
type StorageStatus struct {
	Configured bool   `json:"configured"`
	Reachable  bool   `json:"reachable"`
	UsedBytes  *int64 `json:"used_bytes,omitempty" binding:"optional"`
}

// BackupStatus reports backup age and verification status. The app runs
// isolated in containers, so backups are executed on the host.
type BackupStatus struct {
	LastBackupAge      string `json:"last_backup_age" example:"unknown"`
	LastRestoreTestAge string `json:"last_restore_test_age" example:"unknown"`
	Hint               string `json:"hint" example:"doc/operations.md#backups-and-restore"`
}

// ActivityStatus reports live sessions, websocket connections and Talks.
type ActivityStatus struct {
	ActiveSessions            int `json:"active_sessions"`
	ConnectedWebsocketClients int `json:"connected_websocket_clients"`
	ActiveTalks               int `json:"active_talks"`
	VoiceParticipants         int `json:"voice_participants"`
}

// operatorStatus handles GET /api/admin/status.
//
// @Summary Operator status
// @Description Operator status overview: server version, update state, database and storage stats, backup state, activity counts and warnings. Owner or Administrator only (403 otherwise).
// @ID getAdminStatus
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} OperatorStatus "Operator status overview."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and members with the Administrator permission may do this."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/status [get]
func (h *systemHandler) operatorStatus(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, h.operatorSnapshot(r.Context()))
	return nil
}

func (h *systemHandler) operatorSnapshot(ctx context.Context) OperatorStatus {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	upd := h.updateStatus()
	st := OperatorStatus{
		Version: SystemVersion{
			Current:   h.version,
			Revision:  version.Commit(),
			GoVersion: version.GoVersion(),
		},
		Update:     upd,
		AutoDeploy: h.selfUpdateStatus(upd),
		Database:   h.operatorDBStatus(ctx),
		Storage:    h.operatorStorageStatus(ctx),
		Backup: BackupStatus{
			LastBackupAge:      "unknown",
			LastRestoreTestAge: "unknown",
			Hint:               "doc/operations.md#backups-and-restore",
		},
		Activity: h.operatorActivityStatus(),
		Warnings: h.operatorWarnings(),
	}

	if h.cfg != nil && h.cfg.MediaDiskPath != "" {
		if free, err := media.FreeDiskBytes(h.cfg.MediaDiskPath); err == nil {
			st.DiskFreeBytes = &free
		}
	}

	return st
}

func (h *systemHandler) operatorDBStatus(ctx context.Context) DatabaseStatus {
	var st DatabaseStatus
	if h.db == nil || h.db.Ping(ctx) != nil {
		return st
	}
	st.Reachable = true

	var size int64
	if err := h.db.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&size); err == nil {
		st.SizeBytes = &size
	}

	var latest string
	var applied int
	if err := h.db.QueryRow(ctx, `SELECT COUNT(*), COALESCE(MAX(filename), '') FROM schema_migrations`).Scan(&applied, &latest); err == nil {
		st.LatestMigration, st.AppliedMigrations = latest, applied
	}
	if files, err := db.MigrationFiles(); err == nil && len(files) > applied {
		st.PendingMigrations = len(files) - applied
	}
	return st
}

func (h *systemHandler) operatorStorageStatus(ctx context.Context) StorageStatus {
	st := StorageStatus{
		Configured: h.hasStore,
	}
	if !h.hasStore {
		return st
	}
	st.Reachable = h.storage == nil || h.storage() == nil
	if st.Reachable && h.db != nil {
		var used int64
		if err := h.db.QueryRow(ctx, `SELECT COALESCE(SUM(size_bytes), 0) FROM media WHERE NOT is_deleted`).Scan(&used); err == nil {
			st.UsedBytes = &used
		}
	}
	return st
}

func (h *systemHandler) operatorActivityStatus() ActivityStatus {
	var act ActivityStatus
	if h.hub != nil {
		st := h.hub.Stats()
		act.ActiveSessions = st.Sessions
		act.ConnectedWebsocketClients = st.Connections
		act.ActiveTalks = st.VoiceRooms
		act.VoiceParticipants = st.VoiceParticipants
	}
	return act
}

func (h *systemHandler) operatorWarnings() []opwarn.Warning {
	if h.warnings == nil {
		return []opwarn.Warning{}
	}
	return h.warnings.List()
}
