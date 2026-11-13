package account

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// downloadExport handles GET /api/users/me/export/download.
//
// @Summary Download my data export
// @Description The finished ZIP with its Content-Length. An interrupted download leaves the export as it was; after the first complete download the file can be downloaded again for one hour (never past its expiry) and is then deleted, so a copy the client lost after the server sent it is not gone. Refuses requests that browsers mark as coming from another site (Sec-Fetch-Site) and prefetches (Sec-Purpose or Purpose: prefetch). 404 when there is no ready export, it expired or its hour after the download is over.
// @ID downloadDataExport
// @Tags Users
// @Produce application/zip
// @Security cookieAuth
// @Success 200 {file} binary "The ZIP archive."
// @Header 200 {string} Cache-Control "no-store"
// @Header 200 {string} Content-Disposition "attachment; filename=mnema-talk-export.zip"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Header 200 {integer} Content-Length "Size of the archive in bytes."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the request came from another site (Sec-Fetch-Site is neither same-origin nor none), or it is a prefetch."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no export is ready, it expired, or the hour after its download is over."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/export/download [get]
func (h *API) downloadExport(w http.ResponseWriter, r *http.Request) error {
	if h.Objects == nil {
		return httpx.ErrUnavailable("exports need object storage")
	}
	// The download takes the file, so it must not be triggerable from another
	// site (an image tag, a link, a form): browsers say where a request comes
	// from, and only the app itself or a typed address may ask.
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return httpx.ErrForbidden("the download can only be started from this site")
	}
	// A prefetch is no request of the member's: it must not start the hour
	// after which the file is gone.
	if isPrefetch(r) {
		return httpx.ErrForbidden("the download cannot be prefetched")
	}
	u := auth.UserFrom(r.Context())
	id, key, size, err := h.claimDownload(r.Context(), u.ID)
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
	// The length lets a client (and the desktop hosts' proxies) size the
	// transfer and tell a cut one from a complete one.
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	if _, err := io.Copy(w, body); err != nil {
		// Not delivered: the export stays available for another try.
		slog.Warn("export download interrupted", "err", err)
		h.releaseDownload(r.Context(), id)
		return nil
	}
	h.finishDownload(r.Context(), id)
	h.AuditUser(r.Context(), u, "auth.export_download")
	return nil
}

// isPrefetch reports a request the browser makes ahead of the member
// (link prefetch, speculation rules).
func isPrefetch(r *http.Request) bool {
	for _, name := range []string{"Sec-Purpose", "Purpose", "X-Purpose", "X-Moz"} {
		if strings.Contains(strings.ToLower(r.Header.Get(name)), "prefetch") {
			return true
		}
	}
	return false
}

// claimDownload reserves the newest ready export for one download at a time:
// not yet downloaded, or downloaded completely within the last hour. A
// reservation that was never finished (a crashed request) lapses after ten
// minutes.
func (h *API) claimDownload(ctx context.Context, user uuid.UUID) (id uuid.UUID, key string, size int64, err error) {
	err = h.Sessions.DB.QueryRow(ctx, `UPDATE data_exports SET claimed_at = NOW()
		WHERE id = (SELECT id FROM data_exports WHERE user_id = $1 AND status = 'ready'
		            AND (downloaded_at IS NULL OR downloaded_at > NOW() - $2 * INTERVAL '1 second')
		            AND s3_key IS NOT NULL AND expires_at > NOW()
		            AND (claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '10 minutes')
		            ORDER BY requested_at DESC LIMIT 1)
		RETURNING id, s3_key, size_bytes`, user, int64(exportRedownload.Seconds())).Scan(&id, &key, &size)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", 0, httpx.ErrNotFound("there is no export to download")
	}
	return id, key, size, err
}

func (h *API) releaseDownload(ctx context.Context, id uuid.UUID) {
	_, _ = h.Sessions.DB.Exec(context.WithoutCancel(ctx), `UPDATE data_exports SET claimed_at = NULL WHERE id = $1`, id)
}

// finishDownload records the first complete download and frees the
// reservation. The file stays for exportRedownload; the sweep removes it then.
func (h *API) finishDownload(ctx context.Context, id uuid.UUID) {
	_, _ = h.Sessions.DB.Exec(context.WithoutCancel(ctx), `UPDATE data_exports SET downloaded_at = COALESCE(downloaded_at, NOW()), claimed_at = NULL WHERE id = $1`, id)
}
