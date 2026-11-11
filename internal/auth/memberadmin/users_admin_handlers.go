package memberadmin

import (
	"net/http"
	"strings"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/auth/authn"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// listUsers handles GET /api/admin/users.
//
// @Summary List all accounts
// @Description Admins first, then by username. Requires the Manage members or Manage roles permission (403 otherwise). Includes each account's role_ids.
// @ID listUsersAdmin
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {array} auth.AdminUser "Accounts."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage members or Manage roles permission."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users [get]
func (h *API) listUsers(w http.ResponseWriter, r *http.Request) error {
	users, err := auth.ListUsersForAdmin(r.Context(), h.Sessions.DB)
	if err != nil {
		return err
	}
	if err := h.withRoles(r, users); err != nil {
		return err
	}
	if u := auth.UserFrom(r.Context()); u != nil && u.IsAdmin() {
		locked := h.LockedAccounts()
		for i := range users {
			users[i].LoginLocked = locked[strings.ToLower(users[i].Username)]
		}
	}
	httpx.WriteJSON(w, http.StatusOK, users)
	return nil
}

// disableUser handles POST /api/admin/users/{id}/disable.
//
// @Summary Disable an account
// @Description Signs the user out, drops their live connections and removes them from voice. Requires the Manage members permission (403 otherwise); members whose highest role is not below the caller's, and the Owner, cannot be targeted. The Owner cannot be targeted (400).
// @ID disableUser
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage members permission, or the member's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/disable [post]
func (h *API) disableUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.Moderated(r, id, func(q db.Querier) error { return auth.SetDisabled(r.Context(), q, id, true) }); err != nil {
		return err
	}
	if h.Live != nil {
		h.Live.KickFromVoice(id)
		h.Live.DisconnectUser(id)
	}
	if h.OnAccountEnded != nil {
		h.OnAccountEnded(id)
	}
	h.Events.Broadcast(events.Untyped(events.UserUpdate, map[string]any{"id": id, "disabled": true}))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// enableUser handles POST /api/admin/users/{id}/enable.
//
// @Summary Re-enable an account
// @Description Requires the Manage members permission (403 otherwise); members whose highest role is not below the caller's, and the Owner, cannot be targeted.
// @ID enableUser
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage members permission, or the member's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/enable [post]
func (h *API) enableUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.Moderated(r, id, func(q db.Querier) error { return auth.SetDisabled(r.Context(), q, id, false) }); err != nil {
		return err
	}
	h.Events.Broadcast(events.Untyped(events.UserUpdate, map[string]any{"id": id, "disabled": false}))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// revokeUserSessions handles POST /api/admin/users/{id}/sessions/revoke.
//
// @Summary Revoke all sessions of a user
// @Description Requires the Manage members permission (403 otherwise); members whose highest role is not below the caller's, and the Owner, cannot be targeted.
// @ID revokeUserSessions
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage members permission, or the member's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/sessions/revoke [post]
func (h *API) revokeUserSessions(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.Moderated(r, id, func(q db.Querier) error { return auth.RevokeSessions(r.Context(), q, id) }); err != nil {
		return err
	}
	if h.Live != nil {
		h.Live.DisconnectUser(id)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// resetUserPassword handles POST /api/admin/users/{id}/password-reset.
//
// @Summary Generate a temporary password
// @Description Ends all sessions; the password is returned once. Owner only (403 otherwise). The Owner must confirm with their own current_password and, while their two-factor sign-in is on, a current code (403 when missing or wrong); wrong answers count toward the usual lockouts.
// @ID resetUserPassword
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Param request body auth.OwnerProof false "Owner confirmation."
// @Success 200 {object} auth.TemporaryPassword "Temporary password."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this, the Owner's password or code is missing or wrong, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/password-reset [post]
func (h *API) resetUserPassword(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req auth.OwnerProof
	if err := httpx.DecodeOptionalJSON(r, &req); err != nil {
		return err
	}
	if ok, err := h.ConfirmOwner(w, r, req); !ok {
		return err
	}
	password, err := auth.ResetPassword(r.Context(), h.Sessions.DB, id)
	if err != nil {
		return err
	}
	if h.Live != nil {
		h.Live.DisconnectUser(id)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, auth.TemporaryPassword{Password: password})
	return nil
}

// setUserPassword handles POST /api/admin/users/{id}/password.
//
// @Summary Set a user's password
// @Description Ends all of the user's sessions. Owner only (403 otherwise). The Owner must confirm with their own current_password and, while their two-factor sign-in is on, a current code (403 when missing or wrong); wrong answers count toward the usual lockouts.
// @ID setUserPassword
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Param request body auth.SetPasswordRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this, the Owner's password or code is missing or wrong, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/password [post]
func (h *API) setUserPassword(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req auth.SetPasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if ok, err := h.ConfirmOwner(w, r, req.OwnerProof); !ok {
		return err
	}
	if err := auth.SetPassword(r.Context(), h.Sessions.DB, id, req.Password); err != nil {
		return err
	}
	if h.Live != nil {
		h.Live.DisconnectUser(id)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// setUserStatus lets an admin edit or clear any member's status line.
//
// @Summary Edit or clear a member's status line
// @Description Broadcasts user_update. Requires the Manage members permission (403 otherwise); members whose highest role is not below the caller's, and the Owner, cannot be targeted.
// @ID setUserStatus
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Param request body auth.SetStatusRequest true "Request body."
// @Success 200 {object} auth.User "Updated user (public view unless it is the caller)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage members permission, or the member's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/status [put]
func (h *API) setUserStatus(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	return h.WriteStatus(w, r, id, true)
}

// resetUserTwoFactor handles POST /api/admin/users/{id}/two-factor/reset.
//
// @Summary Reset a member's two-factor sign-in
// @Description For a member who lost the authenticator and the recovery codes: turns two-factor sign-in off, deletes the secret and recovery codes and ends all sessions of the account. Owner only (403 otherwise); the Owner account cannot be targeted (400), its second factor is reset from the host with `mnema-talk reset-2fa <username>`.
// @ID resetUserTwoFactor
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Param request body auth.OwnerProof false "Owner confirmation."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this, the Owner's password or code is missing or wrong, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/two-factor/reset [post]
func (h *API) resetUserTwoFactor(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req auth.OwnerProof
	if err := httpx.DecodeOptionalJSON(r, &req); err != nil {
		return err
	}
	if ok, err := h.ConfirmOwner(w, r, req); !ok {
		return err
	}
	if _, err := auth.TargetUser(r.Context(), h.Sessions.DB, id); err != nil {
		return err
	}
	if err := authn.ResetTwoFactor(r.Context(), h.Sessions.DB, id); err != nil {
		return err
	}
	if h.Live != nil {
		h.Live.DisconnectUser(id)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
