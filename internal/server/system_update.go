package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/update"
)

// UpdateStatus is what the server knows about newer releases.
type UpdateStatus struct {
	// CheckEnabled is false with UPDATE_CHECK_ENABLED=false (no requests to GitHub).
	CheckEnabled   bool   `json:"check_enabled"`
	CurrentVersion string `json:"current_version"`
	// LatestVersion is empty until a check succeeded.
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	// ReleaseURL is the release page on github.com.
	ReleaseURL string `json:"release_url"`
	// ReleaseNotes is Markdown; clients render it as untrusted text.
	ReleaseNotes string     `json:"release_notes"`
	PublishedAt  *time.Time `json:"published_at" format:"date-time" extensions:"x-nullable"`
	CheckedAt    *time.Time `json:"checked_at" format:"date-time" extensions:"x-nullable"`
	// CheckError says why the last check failed (empty when it worked).
	CheckError string `json:"check_error"`
	// RetryAt is set while GitHub's rate limit pauses the checks.
	RetryAt *time.Time `json:"retry_at" format:"date-time" extensions:"x-nullable"`
}

func (h *systemHandler) updateStatus() UpdateStatus {
	st := UpdateStatus{CheckEnabled: h.updates != nil, CurrentVersion: h.version}
	if h.updates == nil {
		return st
	}
	snap := h.updates.Snapshot()
	st.CheckError = snap.Error
	if !snap.CheckedAt.IsZero() {
		t := snap.CheckedAt.UTC()
		st.CheckedAt = &t
	}
	if !snap.RetryAt.IsZero() {
		t := snap.RetryAt.UTC()
		st.RetryAt = &t
	}
	if rel := snap.Latest; rel != nil {
		st.LatestVersion = rel.Version
		st.ReleaseURL = rel.URL
		st.ReleaseNotes = rel.Notes
		st.UpdateAvailable = update.IsNewer(rel.Version, h.version)
		if !rel.PublishedAt.IsZero() {
			t := rel.PublishedAt
			st.PublishedAt = &t
		}
	}
	return st
}

// getUpdate handles GET /api/admin/system/update.
//
// @Summary Update status
// @Description What the last release check found (cheap: no request to GitHub). Requires role admin (403 otherwise).
// @ID getSystemUpdate
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} UpdateStatus "Update status."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/system/update [get]
func (h *systemHandler) getUpdate(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, h.updateStatus())
	return nil
}

// checkNow handles POST /api/admin/system/check.
//
// @Summary Check for a new release now
// @Description Asks GitHub for the latest release right away (at most once a minute; while GitHub rate-limits the server, 429 until that ends). 409 when UPDATE_CHECK_ENABLED=false. A failed check still answers 200 with check_error set. Requires role admin (403 otherwise).
// @ID checkSystemUpdate
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} UpdateStatus "Update status after the check."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the update check is disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/system/check [post]
func (h *systemHandler) checkNow(w http.ResponseWriter, r *http.Request) error {
	if h.updates == nil {
		return httpx.ErrConflict("the update check is disabled (UPDATE_CHECK_ENABLED=false)")
	}
	err := h.updates.CheckNow(r.Context())
	switch {
	case errors.Is(err, update.ErrTooSoon):
		httpx.WriteRateLimited(w, update.ManualMinGap)
		return nil
	case errors.Is(err, update.ErrBackoff):
		retry := time.Minute
		if at := h.updates.Snapshot().RetryAt; !at.IsZero() {
			retry = time.Until(at)
		}
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	// Other failures are part of the status (check_error).
	httpx.WriteJSON(w, http.StatusOK, h.updateStatus())
	return nil
}
