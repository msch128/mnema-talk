package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// listUsers handles GET /api/admin/users.
//
// @Summary List all accounts
// @Description Admins first, then by username. Requires the Manage members or Manage roles permission (403 otherwise). Includes each account's role_ids.
// @ID listUsersAdmin
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {array} AdminUser "Accounts."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage members or Manage roles permission."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users [get]
func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) error {
	users, err := ListUsersForAdmin(r.Context(), h.Sessions.DB)
	if err != nil {
		return err
	}
	if err := h.withRoles(r, users); err != nil {
		return err
	}
	if u := UserFrom(r.Context()); u != nil && u.IsAdmin() {
		locked := h.lockedAccounts()
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
func (h *Handler) disableUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.moderated(r, id, func(q db.Querier) error { return SetDisabled(r.Context(), q, id, true) }); err != nil {
		return err
	}
	if h.Live != nil {
		h.Live.KickFromVoice(id)
		h.Live.DisconnectUser(id)
	}
	if h.OnAccountEnded != nil {
		h.OnAccountEnded(id)
	}
	h.Events.Broadcast(events.UserUpdate, map[string]any{"id": id, "disabled": true})
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
func (h *Handler) enableUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.moderated(r, id, func(q db.Querier) error { return SetDisabled(r.Context(), q, id, false) }); err != nil {
		return err
	}
	h.Events.Broadcast(events.UserUpdate, map[string]any{"id": id, "disabled": false})
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
func (h *Handler) revokeUserSessions(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.moderated(r, id, func(q db.Querier) error { return RevokeSessions(r.Context(), q, id) }); err != nil {
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
// @Description Ends all sessions; the password is returned once. Owner only (403 otherwise).
// @ID resetUserPassword
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Success 200 {object} TemporaryPassword "Temporary password."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/password-reset [post]
func (h *Handler) resetUserPassword(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	password, err := ResetPassword(r.Context(), h.Sessions.DB, id)
	if err != nil {
		return err
	}
	if h.Live != nil {
		h.Live.DisconnectUser(id)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, TemporaryPassword{Password: password})
	return nil
}

// setUserPassword handles POST /api/admin/users/{id}/password.
//
// @Summary Set a user's password
// @Description Ends all of the user's sessions. Owner only (403 otherwise).
// @ID setUserPassword
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Param request body SetPasswordRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/password [post]
func (h *Handler) setUserPassword(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req SetPasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := SetPassword(r.Context(), h.Sessions.DB, id, req.Password); err != nil {
		return err
	}
	if h.Live != nil {
		h.Live.DisconnectUser(id)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// kickUser handles POST /api/admin/users/{id}/kick.
//
// @Summary Remove a user from voice
// @Description 400 when the user is not in a Talk the caller may moderate (View channels and Disconnect members in that Talk; a Talk they may not moderate answers the same). Requires the Disconnect members permission (403 otherwise); members whose highest role is not below the caller's cannot be targeted, and the Owner cannot be removed (400).
// @ID kickUserFromVoice
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Disconnect members permission, or the member's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/kick [post]
func (h *Handler) kickUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := h.moderate(r, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound("user not found")
		}
		return err
	}
	kicked, err := h.kickUserFromVoice(r, id)
	if err != nil {
		return err
	}
	if !kicked {
		return httpx.ErrInvalidInput("user is not in a Talk")
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) kickUserFromVoice(r *http.Request, id uuid.UUID) (bool, error) {
	if h.Live == nil {
		return false, nil
	}
	actor := UserFrom(r.Context()).Subject()
	kicked := false
	for _, ch := range h.Live.VoiceChannels(id) {
		allowed, err := h.canModerateVoiceChannel(r, actor, ch)
		if err != nil {
			return false, err
		}
		if !allowed {
			continue
		}
		h.Live.KickFromChannel(id, ch)
		kicked = true
	}
	return kicked, nil
}

func (h *Handler) canModerateVoiceChannel(r *http.Request, actor perm.Subject, ch uuid.UUID) (bool, error) {
	err := h.Perms.RequireChannel(r.Context(), actor, ch, perm.ViewChannel|perm.DisconnectMembers)
	if err == nil {
		return true, nil
	}
	if _, refused := httpx.AsAPIError(err); !refused {
		return false, err
	}
	return false, nil
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
// @Param request body SetStatusRequest true "Request body."
// @Success 200 {object} User "Updated user (public view unless it is the caller)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage members permission, or the member's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/status [put]
func (h *Handler) setUserStatus(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	return h.writeStatus(w, r, id, true)
}

// SetPasswordRequest is the body of POST /api/admin/users/{id}/password.
type SetPasswordRequest struct {
	Password string `json:"password" format:"password" minLength:"10"`
}

// TemporaryPassword is the response of POST /api/admin/users/{id}/password-reset.
type TemporaryPassword struct {
	// Password is shown once; it is never stored in plain text.
	Password string `json:"password"`
}

// resetUserTwoFactor handles POST /api/admin/users/{id}/two-factor/reset.
//
// @Summary Reset a member's two-factor sign-in
// @Description For a member who lost the authenticator and the recovery codes: turns two-factor sign-in off, deletes the secret and recovery codes and ends all sessions of the account. Owner only (403 otherwise); the Owner account cannot be targeted (400), its second factor is reset from the host with `mnema-talk reset-2fa <username>`.
// @ID resetUserTwoFactor
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/two-factor/reset [post]
func (h *Handler) resetUserTwoFactor(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := targetUser(r.Context(), h.Sessions.DB, id); err != nil {
		return err
	}
	if err := ResetTwoFactor(r.Context(), h.Sessions.DB, id); err != nil {
		return err
	}
	if h.Live != nil {
		h.Live.DisconnectUser(id)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
