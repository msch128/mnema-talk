package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	// exportTTL is how long a finished export can be downloaded.
	exportTTL = 24 * time.Hour
	// exportBuildTimeout bounds the background work of one export.
	exportBuildTimeout = 5 * time.Minute
	// exportSlots is how many exports are built at once on the server.
	exportSlots = 2
	// exportBusyRetry is the Retry-After of a request refused for lack of a
	// build slot.
	exportBusyRetry  = time.Minute
	exportSweepEvery = 15 * time.Minute
)

// ExportStatus is where the caller's data export stands.
type ExportStatus struct {
	// State is none, pending, ready, failed, downloaded or expired.
	State       string     `json:"state" enums:"none,pending,ready,failed,downloaded,expired"`
	RequestedAt *time.Time `json:"requested_at,omitempty" binding:"optional" format:"date-time"`
	// ExpiresAt is when a ready export stops being downloadable.
	ExpiresAt *time.Time `json:"expires_at,omitempty" binding:"optional" format:"date-time"`
	SizeBytes int64      `json:"size_bytes"`
	// NextRequestAt is when a new export may be requested; omitted if now.
	NextRequestAt *time.Time `json:"next_request_at,omitempty" binding:"optional" format:"date-time"`
}

// ExportRequest is the body of POST /api/users/me/export.
type ExportRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
	// Code is a current authenticator or recovery code; needed only while
	// two-factor sign-in is on.
	Code string `json:"code,omitempty" maxLength:"32" binding:"optional"`
}

func (h *Handler) mountExport(r chi.Router) {
	r.Post("/users/me/export", httpx.Handle(h.requestExport))
	r.Get("/users/me/export", httpx.Handle(h.exportStatus))
	r.With(h.rec.downloadPerUser.By(UserKey)).Get("/users/me/export/download", httpx.Handle(h.downloadExport))
}

// requestExport handles POST /api/users/me/export.
//
// @Summary Request an export of my data
// @Description GDPR Art. 20: builds a ZIP of JSON files with the caller's own data (profile, settings, roles, own messages with the channel names the caller can still see, reactions, upload list with links, audit entries about the account without other people's names). Needs the current password and, while two-factor sign-in is on, a current code. One export per 24 hours; it is built in the background (poll GET /api/users/me/export), can be downloaded once within 24 hours and is then deleted. Needs object storage. Size-bounded; nothing of other members' personal data is included.
// @ID requestDataExport
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body ExportRequest true "Request body."
// @Success 202 {object} ExportStatus "Accepted; the export is being built."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: wrong password or code, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: an export was already requested in the last 24 hours, or too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Header 503 {integer} Retry-After "Seconds until the client may retry (when every export slot is busy)."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: object storage is not available, or every export slot is busy; no export was started and none counts toward the daily limit."
// @Router /api/users/me/export [post]
func (h *Handler) requestExport(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	var req ExportRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if h.Objects == nil {
		return httpx.ErrUnavailable("exports need object storage")
	}
	if ok, err := h.ConfirmIdentity(w, r, u, req.CurrentPassword, req.Code); !ok {
		return err
	}
	id, retry, err := h.claimExport(r.Context(), u.ID)
	if err != nil {
		return err
	}
	if retry > 0 {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	if !h.buildExportInBackground(id, u.ID) {
		busy := httpx.ErrUnavailable("exports are being built for other members right now; try again in a few minutes")
		busy.RetryAfter = exportBusyRetry
		return busy
	}
	h.auditUser(r.Context(), u, "auth.export_request")
	st, err := h.exportState(r.Context(), u.ID)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusAccepted, st)
	return nil
}

// claimExport records a new export unless one was requested in the last 24
// hours (failed ones do not count). A per-account advisory lock makes check
// and insert one step, so parallel requests cannot all pass.
func (h *Handler) claimExport(ctx context.Context, user uuid.UUID) (id uuid.UUID, wait time.Duration, err error) {
	err = pgx.BeginFunc(ctx, h.Sessions.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('export:' || $1::text, 0))`, user); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT GREATEST(requested_at + INTERVAL '24 hours' - NOW(), INTERVAL '1 second')
			FROM data_exports WHERE user_id = $1 AND status <> 'failed' AND requested_at > NOW() - INTERVAL '24 hours'
			ORDER BY requested_at DESC LIMIT 1`, user).Scan(&wait)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO data_exports (user_id) VALUES ($1) RETURNING id`, user).Scan(&id)
	})
	return id, wait, err
}

// buildExportInBackground starts building the export. It reports false, after
// marking the export failed (which does not count toward the daily limit),
// when every build slot is taken.
func (h *Handler) buildExportInBackground(id, user uuid.UUID) bool {
	select {
	case h.exportSlots <- struct{}{}:
	default:
		h.failExport(context.Background(), id)
		return false
	}
	h.rec.wg.Add(1)
	go func() {
		defer func() {
			<-h.exportSlots
			h.rec.wg.Done()
		}()
		ctx, cancel := context.WithTimeout(h.jobContext(), exportBuildTimeout)
		defer cancel()
		if err := h.buildExport(ctx, id, user); err != nil {
			slog.Warn("data export failed", "err", err)
			h.failExport(context.Background(), id)
		}
	}()
	return true
}

func (h *Handler) failExport(ctx context.Context, id uuid.UUID) {
	_, _ = h.Sessions.DB.Exec(ctx, `UPDATE data_exports SET status = 'failed' WHERE id = $1`, id)
}

// exportState reads the caller's newest export.
func (h *Handler) exportState(ctx context.Context, user uuid.UUID) (ExportStatus, error) {
	var st ExportStatus
	var status string
	var requested, expires time.Time
	var hasKey, downloaded bool
	err := h.Sessions.DB.QueryRow(ctx, `SELECT status, requested_at, COALESCE(expires_at, 'epoch'), size_bytes, s3_key IS NOT NULL, downloaded_at IS NOT NULL
		FROM data_exports WHERE user_id = $1 ORDER BY requested_at DESC LIMIT 1`, user).Scan(&status, &requested, &expires, &st.SizeBytes, &hasKey, &downloaded)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExportStatus{State: "none"}, nil
	}
	if err != nil {
		return st, err
	}
	st.RequestedAt = &requested
	st.State = exportStateName(status, hasKey, downloaded, expires)
	if st.State == "ready" {
		st.ExpiresAt = &expires
	}
	if next := requested.Add(exportTTL); status != "failed" && next.After(time.Now()) {
		st.NextRequestAt = &next
	}
	return st, nil
}

func exportStateName(status string, hasKey, downloaded bool, expires time.Time) string {
	switch {
	case status != "ready":
		return status
	case downloaded:
		return "downloaded"
	case !hasKey || !expires.After(time.Now()):
		return "expired"
	}
	return "ready"
}

// exportStatus handles GET /api/users/me/export.
//
// @Summary Status of my data export
// @Description The caller's newest export: none, pending, ready (with the time it expires), failed, downloaded or expired, and when a new one may be requested.
// @ID getDataExport
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Success 200 {object} ExportStatus "Export status."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/export [get]
func (h *Handler) exportStatus(w http.ResponseWriter, r *http.Request) error {
	st, err := h.exportState(r.Context(), UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, st)
	return nil
}

// downloadExport handles GET /api/users/me/export/download.
//
// @Summary Download my data export
// @Description The finished ZIP, once: the file is spent and deleted only after it was sent completely, so an interrupted download can be repeated. Refuses requests that browsers mark as coming from another site (Sec-Fetch-Site). 404 when there is no ready export, it expired or it was downloaded already.
// @ID downloadDataExport
// @Tags Users
// @Produce application/zip
// @Security cookieAuth
// @Success 200 {file} binary "The ZIP archive."
// @Header 200 {string} Cache-Control "no-store"
// @Header 200 {string} Content-Disposition "attachment; filename=mnema-talk-export.zip"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the request came from another site (Sec-Fetch-Site is neither same-origin nor none)."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no export is ready, it expired or was downloaded already."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/export/download [get]
func (h *Handler) downloadExport(w http.ResponseWriter, r *http.Request) error {
	if h.Objects == nil {
		return httpx.ErrUnavailable("exports need object storage")
	}
	// The download takes the file, so it must not be triggerable from another
	// site (an image tag, a link, a form): browsers say where a request comes
	// from, and only the app itself or a typed address may ask.
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return httpx.ErrForbidden("the download can only be started from this site")
	}
	u := UserFrom(r.Context())
	id, key, err := h.claimDownload(r.Context(), u.ID)
	if err != nil {
		return err
	}
	body, err := h.Objects.GetObjectFrom(r.Context(), key, 0)
	if err != nil {
		h.releaseDownload(r.Context(), id)
		return fmt.Errorf("open export: %w", err)
	}
	defer body.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="mnema-talk-export.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, body); err != nil {
		// Not delivered: the export stays available for another try.
		slog.Warn("export download interrupted", "err", err)
		h.releaseDownload(r.Context(), id)
		return nil
	}
	h.finishDownload(r.Context(), id, key)
	h.auditUser(r.Context(), u, "auth.export_download")
	return nil
}

// claimDownload reserves the newest ready export for one download at a time.
// A reservation that was never finished (a crashed request) lapses after ten
// minutes.
func (h *Handler) claimDownload(ctx context.Context, user uuid.UUID) (uuid.UUID, string, error) {
	var id uuid.UUID
	var key string
	err := h.Sessions.DB.QueryRow(ctx, `UPDATE data_exports SET claimed_at = NOW()
		WHERE id = (SELECT id FROM data_exports WHERE user_id = $1 AND status = 'ready' AND downloaded_at IS NULL
		            AND s3_key IS NOT NULL AND expires_at > NOW()
		            AND (claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '10 minutes')
		            ORDER BY requested_at DESC LIMIT 1)
		RETURNING id, s3_key`, user).Scan(&id, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", httpx.ErrNotFound("there is no export to download")
	}
	return id, key, err
}

func (h *Handler) releaseDownload(ctx context.Context, id uuid.UUID) {
	_, _ = h.Sessions.DB.Exec(context.WithoutCancel(ctx), `UPDATE data_exports SET claimed_at = NULL WHERE id = $1`, id)
}

// finishDownload spends the export after the whole file was sent.
func (h *Handler) finishDownload(ctx context.Context, id uuid.UUID, key string) {
	_, _ = h.Sessions.DB.Exec(context.WithoutCancel(ctx), `UPDATE data_exports SET downloaded_at = NOW() WHERE id = $1`, id)
	h.dropExportFile(ctx, id, key)
}

func (h *Handler) dropExportFile(ctx context.Context, id uuid.UUID, key string) {
	if err := h.Objects.Delete(context.WithoutCancel(ctx), key); err != nil {
		slog.Warn("export file not removed", "err", err)
		return
	}
	_, _ = h.Sessions.DB.Exec(context.WithoutCancel(ctx), `UPDATE data_exports SET s3_key = NULL WHERE id = $1`, id)
}

// RunExportSweep removes finished exports that were downloaded or expired,
// marks builds that never finished as failed and forgets old rows, until ctx
// ends.
func (h *Handler) RunExportSweep(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = exportSweepEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		h.sweepExports(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (h *Handler) sweepExports(ctx context.Context) {
	db := h.Sessions.DB
	_, _ = db.Exec(ctx, `UPDATE data_exports SET status = 'failed' WHERE status = 'pending' AND requested_at < NOW() - INTERVAL '30 minutes'`)
	rows, err := db.Query(ctx, `SELECT id, s3_key FROM data_exports WHERE s3_key IS NOT NULL AND (expires_at < NOW() OR downloaded_at IS NOT NULL) LIMIT 200`)
	if err != nil {
		return
	}
	type item struct {
		id  uuid.UUID
		key string
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (item, error) {
		var it item
		return it, row.Scan(&it.id, &it.key)
	})
	if err != nil || h.Objects == nil {
		return
	}
	for _, it := range items {
		h.dropExportFile(ctx, it.id, it.key)
	}
	_, _ = db.Exec(ctx, `DELETE FROM data_exports WHERE requested_at < NOW() - INTERVAL '7 days' AND s3_key IS NULL`)
}
