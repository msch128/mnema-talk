package media

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// stats handles GET /api/admin/media/stats.
//
// @Summary Storage totals
// @Description Owner only (403 otherwise).
// @ID getMediaStats
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} Stats "Totals."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/media/stats [get]
func (h *Handler) stats(w http.ResponseWriter, r *http.Request) error {
	stats, err := GetStats(r.Context(), h.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, stats)
	return nil
}

// list handles GET /api/admin/media.
//
// @Summary List all media, newest first
// @Description Owner only (403 otherwise).
// @ID listMedia
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param limit query int false "Page size. Values below 1 or non-numeric fall back to the default; larger values are clamped to 100." minimum(1) maximum(100) default(50)
// @Param offset query int false "Number of items to skip." minimum(0) default(0)
// @Success 200 {array} DashboardItem "Media page."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/media [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}
	items, err := ListMedia(r.Context(), h.DB, httpx.QueryLimit(r, "limit", 50, 100), offset)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, items)
	return nil
}

// prune handles POST /api/admin/media/prune.
//
// @Summary Delete old chat attachments
// @Description Avatars are never pruned. Without days, uses MEDIA_RETENTION_DAYS; 400 when that is disabled. Owner only (403 otherwise).
// @ID pruneMedia
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param days query int false "Delete attachments older than this many days; defaults to MEDIA_RETENTION_DAYS." minimum(1) maximum(3650)
// @Success 200 {object} PruneResult "Prune result."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/admin/media/prune [post]
func (h *Handler) prune(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	days := h.RetentionDays
	if raw := r.URL.Query().Get("days"); raw != "" {
		d, err := strconv.Atoi(raw)
		if err != nil || d < 1 || d > 3650 {
			return httpx.ErrInvalidInput("days must be between 1 and 3650")
		}
		days = d
	}
	if days < 1 {
		return httpx.ErrInvalidInput("retention is disabled; pass days explicitly")
	}
	n, err := PruneOlderThan(r.Context(), h.DB, h.Store, days)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, PruneResult{PrunedCount: n, CutoffDays: days})
	return nil
}

// delete handles DELETE /api/admin/media/{id}.
//
// @Summary Delete a media object
// @Description Removes the object from storage and marks it deleted; chat shows a placeholder. Idempotent. Owner only (403 otherwise).
// @ID deleteMedia
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/admin/media/{id} [delete]
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := DeleteMedia(r.Context(), h.DB, h.Store, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// PruneResult is the response of POST /api/admin/media/prune.
type PruneResult struct {
	PrunedCount int `json:"pruned_count"`
	CutoffDays  int `json:"cutoff_days"`
}

// orphans handles GET /api/admin/media/orphans.
//
// @Summary Count orphaned media objects
// @Description Objects in storage (uploads/, avatars/) that no media row refers to, older than 24 hours: left behind by a failed delete or a crash during an upload. Nothing shows them; they only take space. Lists the whole bucket prefix, so it may take a while. Owner only (403 otherwise).
// @ID getMediaOrphans
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} Orphans "Orphaned objects (deleted is 0)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/admin/media/orphans [get]
func (h *Handler) orphans(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	o, err := FindOrphans(r.Context(), h.DB, h.Store, false)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, o)
	return nil
}

// cleanupOrphans handles POST /api/admin/media/orphans/cleanup.
//
// @Summary Delete orphaned media objects
// @Description Deletes the objects GET /api/admin/media/orphans counts. Never touches an object a media row refers to, nor one younger than 24 hours. Owner only (403 otherwise).
// @ID cleanupMediaOrphans
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} Orphans "Orphaned objects found and deleted."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/admin/media/orphans/cleanup [post]
func (h *Handler) cleanupOrphans(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	admin := auth.UserFrom(r.Context())
	o, err := FindOrphans(r.Context(), h.DB, h.Store, true)
	if o != nil && o.Deleted > 0 {
		slog.Info("orphaned media removed", "audit", "media_orphans", "admin_id", admin.ID, "admin", admin.Username, "deleted", o.Deleted, "bytes", o.Bytes)
	}
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, o)
	return nil
}
