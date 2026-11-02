package onboarding

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/raid"
)

// getRaid handles GET /api/admin/raid.
//
// @Summary Read the raid protection settings
// @Description Owner and Administrators only. The sign-up throttle (at most join_limit new members within join_window_minutes), the minimum membership ages before a new member may write, react and speak (0: off), how many members joined within the window right now, and the state of the lockdown.
// @ID getRaidProtection
// @Tags Onboarding
// @Produce json
// @Security cookieAuth
// @Success 200 {object} raid.Settings "Settings."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and members with the Administrator permission may read it."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/raid [get]
func (h *Handler) getRaid(w http.ResponseWriter, r *http.Request) error {
	s, err := raid.Get(r.Context(), h.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s)
	return nil
}

// putRaid handles PUT /api/admin/raid.
//
// @Summary Set the raid protection settings
// @Description Owner and Administrators only; audited. Replaces the sign-up throttle and the minimum membership ages. While the throttle is full, or a lockdown is on, new sign-ups answer UNAVAILABLE ("sign-ups are paused"); existing members are unaffected. A new member who has not reached an age yet cannot write, react or speak in Talks (direct messages are exempt); the answer is FORBIDDEN with the seconds left, and permissions carry `new_member`. Administrators, the Owner, bots and holders of Bypass new member limits never wait. Broadcasts permissions_changed.
// @ID setRaidProtection
// @Tags Onboarding
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body raid.Input true "New settings."
// @Success 200 {object} raid.Settings "Settings."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and members with the Administrator permission may change it, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/raid [put]
func (h *Handler) putRaid(w http.ResponseWriter, r *http.Request) error {
	var in raid.Input
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	s, err := raid.Put(r.Context(), h.DB, in)
	if err != nil {
		return err
	}
	// The age gates live in the permission snapshot.
	h.Perms.Changed(nil)
	httpx.WriteJSON(w, http.StatusOK, s)
	return nil
}

// putLockdown handles PUT /api/admin/raid/lockdown.
//
// @Summary Switch the lockdown on or off
// @Description Owner and Administrators only; audited. While the lockdown is on, nobody new can sign up (also with a valid invite); with `pause_invites` no invite can be created either. `hours` ends it by itself after that long (1 to 720); without it, it stays until switched off. Existing members and their sessions are unaffected.
// @ID setLockdown
// @Tags Onboarding
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body raid.LockdownInput true "Lockdown."
// @Success 200 {object} raid.Settings "Settings."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and members with the Administrator permission may change it, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/raid/lockdown [put]
func (h *Handler) putLockdown(w http.ResponseWriter, r *http.Request) error {
	var in raid.LockdownInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	s, err := raid.SetLockdown(r.Context(), h.DB, in)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s)
	return nil
}
