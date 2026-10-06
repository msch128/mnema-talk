package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/update"
)

// selfUpdateCooldown is the least time between two calls to the updater
// sidecar: one update at a time, and a stolen admin session can't turn the
// endpoint into a restart loop.
const selfUpdateCooldown = 5 * time.Minute

// SelfUpdateStatus says whether "update now" can be offered.
type SelfUpdateStatus struct {
	// Configured is true when UPDATER_TOKEN is set (sidecar opted in).
	Configured bool `json:"configured"`
	// Image is MNEMA_IMAGE, the image reference the app container runs.
	Image string `json:"image"`
	// Reach says whether re-pulling Image can reach the latest release.
	Reach       string `json:"reach" enums:"yes,no,unknown"`
	ReachReason string `json:"reach_reason" enums:"follows,unset,local_build,digest_pinned,version_pinned,track_mismatch,custom_tag"`
	// Available: configured, an update exists, the image can reach it and no
	// update ran in the last five minutes.
	Available bool `json:"available"`
	// NextAllowedAt is set during the cooldown after an update request.
	NextAllowedAt *time.Time `json:"next_allowed_at" format:"date-time" extensions:"x-nullable"`
}

// SelfUpdateRequest is the body of POST /api/admin/system/update.
type SelfUpdateRequest struct {
	// Password is the admin's current password (re-authentication).
	Password string `json:"password" format:"password"`
	// TargetVersion must equal the latest release the server knows, so a
	// dialog opened before a newer release can't start the wrong update.
	TargetVersion string `json:"target_version"`
}

// SelfUpdateStarted is the response of POST /api/admin/system/update.
type SelfUpdateStarted struct {
	Status        string `json:"status" enums:"started"`
	FromVersion   string `json:"from_version"`
	TargetVersion string `json:"target_version"`
}

// selfUpdate holds the sidecar client and the cooldown state.
type selfUpdate struct {
	updater *update.Updater // nil: not configured
	confirm func(ctx context.Context, userID uuid.UUID, password string) (time.Duration, error)
	now     func() time.Time

	lastCall time.Time
	inFlight bool
}

func (h *systemHandler) selfUpdateStatus(upd UpdateStatus) SelfUpdateStatus {
	st := SelfUpdateStatus{Configured: h.self.updater != nil, Image: h.cfg.AppImage}
	reach, reason := update.ImageReach(h.cfg.AppImage, upd.LatestVersion)
	st.Reach, st.ReachReason = string(reach), reason
	h.selfMu.Lock()
	if next := h.self.lastCall.Add(selfUpdateCooldown); !h.self.lastCall.IsZero() && h.self.now().Before(next) {
		st.NextAllowedAt = &next
	}
	busy := h.self.inFlight || st.NextAllowedAt != nil
	h.selfMu.Unlock()
	st.Available = st.Configured && upd.UpdateAvailable && reach != update.ReachNo && !busy
	return st
}

// startSelfUpdate handles POST /api/admin/system/update.
//
// @Summary Update the server now
// @Description Asks the optional updater sidecar (compose profile autoupdate, UPDATER_TOKEN) to pull the app image again and restart the app; everyone is disconnected for about 30-60 seconds. Admin only, same-origin only, re-authenticates with the admin's current password (5 wrong passwords lock the action like a password change does), at most once per 5 minutes, only while a newer release is known and target_version names it. Audit-logged. Requires role admin (403 otherwise).
// @ID startSelfUpdate
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body SelfUpdateRequest true "Request body."
// @Success 202 {object} SelfUpdateStarted "The updater accepted the request."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not an admin, wrong current password, or cross-origin request rejected."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: self-update not configured, no update available, target_version outdated, the image tag cannot reach the release, or an update is already running."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: an update was requested less than 5 minutes ago, or too many wrong passwords."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: the updater is not reachable or rejected the request."
// @Router /api/admin/system/update [post]
func (h *systemHandler) startSelfUpdate(w http.ResponseWriter, r *http.Request) error {
	admin := auth.UserFrom(r.Context())
	var req SelfUpdateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if h.self.updater == nil {
		return httpx.ErrConflict("self-update is not configured (UPDATER_TOKEN and the autoupdate profile)")
	}
	upd := h.updateStatus()
	if !upd.UpdateAvailable {
		return httpx.ErrConflict("no update available")
	}
	if req.TargetVersion != upd.LatestVersion {
		return httpx.ErrConflict("the latest release changed; reload the System tab")
	}
	if reach, _ := update.ImageReach(h.cfg.AppImage, upd.LatestVersion); reach == update.ReachNo {
		return httpx.ErrConflict("the configured image tag cannot reach this release; update manually")
	}
	// Cheap checks first: no bcrypt work while the cooldown runs.
	if retry, busy := h.selfUpdateBlocked(); busy {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	log := slog.With("audit", "self_update", "admin_id", admin.ID, "admin", admin.Username,
		"from_version", h.version, "target_version", upd.LatestVersion, "client_ip", httpx.ClientIPKey(r))

	retry, err := h.self.confirm(r.Context(), admin.ID, req.Password)
	if err != nil {
		if retry > 0 {
			log.Warn("self-update refused: password confirmation locked")
			httpx.WriteRateLimited(w, retry)
			return nil
		}
		if errors.Is(err, auth.ErrWrongPassword) {
			log.Warn("self-update refused: wrong password")
		}
		return err
	}

	// Reserve the slot; a concurrent request may have taken it meanwhile.
	h.selfMu.Lock()
	if h.self.inFlight || (!h.self.lastCall.IsZero() && h.self.now().Before(h.self.lastCall.Add(selfUpdateCooldown))) {
		h.selfMu.Unlock()
		httpx.WriteRateLimited(w, selfUpdateCooldown)
		return nil
	}
	h.self.inFlight = true
	h.self.lastCall = h.self.now()
	h.selfMu.Unlock()
	defer func() {
		h.selfMu.Lock()
		h.self.inFlight = false
		h.selfMu.Unlock()
	}()

	log.Info("self-update requested")
	// The admin closing the tab must not cut the request short.
	err = h.self.updater.Trigger(context.WithoutCancel(r.Context()))
	switch {
	case errors.Is(err, update.ErrUpdaterBusy):
		log.Warn("self-update: updater busy")
		return httpx.ErrConflict("an update is already running")
	case err != nil:
		log.Error("self-update failed", "err", err)
		return httpx.ErrUnavailable(updaterMessage(err))
	}
	log.Info("self-update started")
	if h.events != nil {
		h.events.Broadcast("system_update", map[string]string{"version": upd.LatestVersion})
	}
	httpx.WriteJSON(w, http.StatusAccepted, SelfUpdateStarted{Status: "started", FromVersion: h.version, TargetVersion: upd.LatestVersion})
	return nil
}

// selfUpdateBlocked reports whether an update ran (or runs) within the cooldown.
func (h *systemHandler) selfUpdateBlocked() (time.Duration, bool) {
	h.selfMu.Lock()
	defer h.selfMu.Unlock()
	if h.self.inFlight {
		return time.Minute, true
	}
	if h.self.lastCall.IsZero() {
		return 0, false
	}
	if left := h.self.lastCall.Add(selfUpdateCooldown).Sub(h.self.now()); left > 0 {
		return left, true
	}
	return 0, false
}

// updaterMessage keeps sidecar errors to our own fixed wording.
func updaterMessage(err error) string {
	switch {
	case errors.Is(err, update.ErrUpdaterAuth):
		return update.ErrUpdaterAuth.Error()
	case errors.Is(err, update.ErrUpdaterUnreachable):
		return update.ErrUpdaterUnreachable.Error()
	default:
		return "the updater could not start the update; see the updater's log"
	}
}
