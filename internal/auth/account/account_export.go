package account

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/safego"
	"github.com/msch128/mnema-talk/internal/worker"
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
	// exportRedownload is how long a complete download leaves the file in
	// place (never past its expiry), so a copy the client lost after the last
	// byte left the server (a desktop host or proxy that read it and could not
	// save it, a browser that cancelled late) can be taken again.
	exportRedownload = time.Hour
)

// requestExport handles POST /api/users/me/export.
//
// @Summary Request an export of my data
// @Description GDPR Art. 20: builds a ZIP of JSON files with the caller's own data (profile, settings, roles, own messages with the channel names the caller can still see, reactions, upload list with links, audit entries about the account without other people's names). Needs the current password and, while two-factor sign-in is on, a current code. One export per 24 hours; it is built in the background (poll GET /api/users/me/export), can be downloaded within 24 hours and is deleted one hour after the first complete download. Needs object storage. Size-bounded; nothing of other members' personal data is included.
// @ID requestDataExport
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body auth.ExportRequest true "Request body."
// @Success 202 {object} auth.ExportStatus "Accepted; the export is being built."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: wrong password or code, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: an export was already requested in the last 24 hours, or too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Header 503 {integer} Retry-After "Seconds until the client may retry (when every export slot is busy or the server is shutting down)."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: object storage is not available, every export slot is busy, or the server is shutting down; no export was started and none counts toward the daily limit."
// @Router /api/users/me/export [post]
func (h *API) requestExport(w http.ResponseWriter, r *http.Request) error {
	u := auth.UserFrom(r.Context())
	var req auth.ExportRequest
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
	if err := h.buildExportInBackground(id, u.ID); err != nil {
		return err
	}
	h.AuditUser(r.Context(), u, "auth.export_request")
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
func (h *API) claimExport(ctx context.Context, user uuid.UUID) (id uuid.UUID, wait time.Duration, err error) {
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

// buildExportInBackground starts building the export. When every build slot
// is taken, or the server is shutting down, it marks the export failed (which
// does not count toward the daily limit, so the member can ask again) and
// returns the 503 to answer. Should that update fail too (the pool is
// closing), the sweep marks the export failed after 30 minutes.
func (h *API) buildExportInBackground(id, user uuid.UUID) *httpx.APIError {
	select {
	case h.exportSlots <- struct{}{}:
	default:
		h.failExport(context.Background(), id)
		busy := httpx.ErrUnavailable("exports are being built for other members right now; try again in a few minutes")
		busy.RetryAfter = exportBusyRetry
		return busy
	}
	done, ok := h.TrackJob()
	if !ok {
		<-h.exportSlots
		h.failExport(context.Background(), id)
		slog.Warn("server shutting down, data export refused")
		down := httpx.ErrUnavailable("server shutting down")
		down.RetryAfter = exportBusyRetry
		return down
	}
	// A panic fails this export; its slot is released like after any build.
	safego.Go("export_build", func() { h.failExport(context.Background(), id) }, func() {
		defer func() {
			<-h.exportSlots
			done()
		}()
		ctx, cancel := context.WithTimeout(h.JobContext(), exportBuildTimeout)
		defer cancel()
		if err := h.buildExport(ctx, id, user); err != nil {
			slog.Warn("data export failed", "err", err)
			h.failExport(context.Background(), id)
		}
	})
	return nil
}

func (h *API) failExport(ctx context.Context, id uuid.UUID) {
	_, _ = h.Sessions.DB.Exec(ctx, `UPDATE data_exports SET status = 'failed' WHERE id = $1`, id)
}

// exportState reads the caller's newest export.
func (h *API) exportState(ctx context.Context, user uuid.UUID) (auth.ExportStatus, error) {
	var st auth.ExportStatus
	var status string
	var requested, expires time.Time
	var downloaded *time.Time
	var hasKey bool
	err := h.Sessions.DB.QueryRow(ctx, `SELECT status, requested_at, COALESCE(expires_at, 'epoch'), size_bytes, s3_key IS NOT NULL, downloaded_at
		FROM data_exports WHERE user_id = $1 ORDER BY requested_at DESC LIMIT 1`, user).Scan(&status, &requested, &expires, &st.SizeBytes, &hasKey, &downloaded)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ExportStatus{State: "none"}, nil
	}
	if err != nil {
		return st, err
	}
	st.RequestedAt = &requested
	if downloaded != nil {
		// A downloaded file stays for the redownload window, never past its expiry.
		expires = minTime(expires, downloaded.Add(exportRedownload))
		st.DownloadedAt = downloaded
	}
	st.State = exportStateName(status, hasKey, downloaded != nil, expires)
	if st.State == "ready" {
		st.ExpiresAt = &expires
	}
	if next := requested.Add(exportTTL); status != "failed" && next.After(time.Now()) {
		st.NextRequestAt = &next
	}
	return st, nil
}

// exportStateName names the state; expires is when the file stops being
// downloadable (for a downloaded export the end of its redownload window).
func exportStateName(status string, hasKey, downloaded bool, expires time.Time) string {
	switch {
	case status != "ready":
		return status
	case hasKey && expires.After(time.Now()):
		return "ready"
	case downloaded:
		return "downloaded"
	}
	return "expired"
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// exportStatus handles GET /api/users/me/export.
//
// @Summary Status of my data export
// @Description The caller's newest export: none, pending, ready (with the time it stops being downloadable), failed, downloaded or expired, and when a new one may be requested. After a complete download the export stays ready for one more hour (never past its expiry) with downloaded_at set, then reads downloaded.
// @ID getDataExport
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Success 200 {object} auth.ExportStatus "Export status."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/export [get]
func (h *API) exportStatus(w http.ResponseWriter, r *http.Request) error {
	st, err := h.exportState(r.Context(), auth.UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, st)
	return nil
}

// dropExportFile removes the stored file, then forgets its key. A failure
// keeps the key, so the next sweep tries again.
func (h *API) dropExportFile(ctx context.Context, id uuid.UUID, key string) error {
	if err := h.Objects.Delete(context.WithoutCancel(ctx), key); err != nil {
		slog.Warn("export file not removed", "err", err)
		return err
	}
	_, err := h.Sessions.DB.Exec(context.WithoutCancel(ctx), `UPDATE data_exports SET s3_key = NULL WHERE id = $1`, id)
	return err
}

// RunExportSweep removes finished exports that were downloaded or expired,
// marks builds that never finished as failed and forgets old rows, until ctx
// ends.
func (h *API) RunExportSweep(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = exportSweepEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		worker.Tick(worker.JobExportSweep, func() error {
			// An online backup freezes the storage: wait for it to end.
			err := h.Sessions.DB.OutsideBackup(ctx, func() error { return h.sweepExports(ctx) })
			if err != nil && ctx.Err() == nil {
				slog.Warn("export sweep failed", "err", err)
			}
			return worker.IgnoreDone(ctx, err)
		})
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (h *API) sweepExports(ctx context.Context) error {
	db := h.Sessions.DB
	_, markErr := db.Exec(ctx, `UPDATE data_exports SET status = 'failed' WHERE status = 'pending' AND requested_at < NOW() - INTERVAL '30 minutes'`)
	rows, err := db.Query(ctx, `SELECT id, s3_key FROM data_exports WHERE s3_key IS NOT NULL
		AND (expires_at < NOW() OR downloaded_at < NOW() - $1 * INTERVAL '1 second') LIMIT 200`, int64(exportRedownload.Seconds()))
	if err != nil {
		return errors.Join(markErr, err)
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
		return errors.Join(markErr, err)
	}
	failed := []error{markErr}
	for _, it := range items {
		if err := h.dropExportFile(ctx, it.id, it.key); err != nil {
			failed = append(failed, err)
		}
	}
	_, err = db.Exec(ctx, `DELETE FROM data_exports WHERE requested_at < NOW() - INTERVAL '7 days' AND s3_key IS NULL`)
	return errors.Join(append(failed, err)...)
}
