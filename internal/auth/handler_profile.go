package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// setLocale handles PUT /api/users/me/locale.
//
// @Summary Set UI language
// @Description Stores the language (`en` or `de`) on the account, so every device of the member shows the same one.
// @ID setLocale
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body SetLocaleRequest true "Request body."
// @Success 200 {object} User "Updated user."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/locale [put]
func (h *Handler) setLocale(w http.ResponseWriter, r *http.Request) error {
	var req SetLocaleRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u, err := SetLocale(r.Context(), h.Sessions.DB, UserFrom(r.Context()).ID, req.Locale)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

// me handles GET /api/auth/me.
//
// @Summary Current user
// @Description The signed-in account: profile, chosen presence and language, and whether the community rules still have to be accepted (`rules_pending`). The web app calls it on every start to learn whether its session is still valid.
// @ID getCurrentUser
// @Tags Auth
// @Produce json
// @Security cookieAuth
// @Success 200 {object} User "The caller, including chosen presence."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/me [get]
func (h *Handler) me(w http.ResponseWriter, r *http.Request) error {
	u := h.withRulesPending(r.Context(), UserFrom(r.Context()))
	if err := LoadBanner(r.Context(), h.Sessions.DB, &u); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

// withRulesPending returns a copy of the caller with RulesPending filled in
// from the permission state, which is also what enforces the rules.
func (h *Handler) withRulesPending(ctx context.Context, u *User) User {
	out := *u
	if h.Perms == nil {
		return out
	}
	// An unreadable permission state must not break sign-in; the enforcement
	// itself fails closed.
	if snap, err := h.Perms.Snapshot(ctx); err == nil {
		out.RulesPending = snap.RulesPending(u.ID)
	}
	return out
}

// changePassword handles PUT /api/auth/password.
//
// @Summary Change own password
// @Description Signs out all other sessions and keeps the current one (a fresh cookie is set); open WebSockets are closed and reconnect with it. Five wrong current passwords trigger a lockout (429).
// @ID changePassword
// @Tags Auth
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body ChangePasswordRequest true "Request body."
// @Success 204 "Password changed."
// @Header 204 {string} Set-Cookie "Session cookie (HttpOnly, SameSite=Lax, Path=/; Secure on HTTPS)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: current password is incorrect, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/password [put]
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	var req ChangePasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	key := u.ID.String()
	if locked, retry := h.passwordFails.IsLockedOut(key); locked {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	tv, err := ChangePassword(r.Context(), h.Sessions.DB, u.ID, req.CurrentPassword, req.NewPassword)
	if err == ErrWrongPassword {
		h.passwordFails.RecordFailure(key)
		return err
	}
	if err != nil {
		return err
	}
	h.passwordFails.ResetFailures(key)
	h.auditUser(r.Context(), u, "auth.password_change")
	// All other sessions are revoked by the version bump; keep this one alive.
	if err := h.Sessions.Start(w, u, tv); err != nil {
		return err
	}
	h.Sessions.markDevice(w, strings.ToLower(u.Username), u.ID, tv)
	// Close every open WebSocket, other devices' included; this client
	// reconnects with the fresh cookie set above.
	if h.Live != nil {
		h.Live.DisconnectUser(u.ID)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// getUser handles GET /api/users/{userID}.
//
// @Summary Get a user profile
// @Description Includes voice_seconds, message_count and the banner_url of the profile banner. Other users are returned without the private presence choice.
// @ID getUser
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Param userID path string true "User ID." Format(uuid)
// @Success 200 {object} User "The user."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/{userID} [get]
func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "userID")
	if err != nil {
		return err
	}
	u, err := GetUser(r.Context(), h.Sessions.DB, id)
	if err != nil {
		return err
	}
	if err := LoadStats(r.Context(), h.Sessions.DB, u); err != nil {
		return err
	}
	if err := LoadBanner(r.Context(), h.Sessions.DB, u); err != nil {
		return err
	}
	if u.ID != UserFrom(r.Context()).ID {
		pub := u.Public()
		u = &pub
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

// updateProfile handles PUT /api/users/me/profile.
//
// @Summary Update display name and bio
// @Description Broadcasts user_update.
// @ID updateProfile
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body UpdateProfileRequest true "Request body."
// @Success 200 {object} User "Updated user."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/profile [put]
func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) error {
	var req UpdateProfileRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u, err := UpdateProfile(r.Context(), h.Sessions.DB, UserFrom(r.Context()).ID, req.DisplayName, req.Bio)
	if err != nil {
		return err
	}
	h.Events.Broadcast(events.UserUpdate, u.Public())
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

// setPresence handles PUT /api/users/me/presence.
//
// @Summary Choose presence
// @Description Applies to the user's live WebSocket status.
// @ID setPresence
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body SetPresenceRequest true "Request body."
// @Success 200 {object} User "Updated user."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/presence [put]
func (h *Handler) setPresence(w http.ResponseWriter, r *http.Request) error {
	var req SetPresenceRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u, err := SetPresence(r.Context(), h.Sessions.DB, UserFrom(r.Context()).ID, req.Presence)
	if err != nil {
		return err
	}
	if h.Live != nil {
		h.Live.SetPresence(u.ID, u.Presence)
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

// setStatus changes the caller's own status line. Nobody else can, except an
// admin (see setUserStatus).
//
// @Summary Set own status line
// @Description Broadcasts user_update.
// @ID setStatus
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body SetStatusRequest true "Request body."
// @Success 200 {object} User "Updated user."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/status [put]
func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) error {
	return h.writeStatus(w, r, UserFrom(r.Context()).ID, false)
}

// writeStatus sets userID's status line; moderating says it is another
// member's, which needs the usual authority over them.
func (h *Handler) writeStatus(w http.ResponseWriter, r *http.Request, userID uuid.UUID, moderating bool) error {
	var req SetStatusRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	var u *User
	var err error
	if moderating {
		err = h.moderated(r, userID, func(q db.Querier) (e error) {
			u, e = SetStatusText(r.Context(), q, userID, req.StatusText)
			return e
		})
	} else {
		u, err = SetStatusText(r.Context(), h.Sessions.DB, userID, req.StatusText)
	}
	if err != nil {
		return err
	}
	pub := u.Public()
	h.Events.Broadcast(events.UserUpdate, pub)
	if userID != UserFrom(r.Context()).ID {
		u = &pub
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

// UserEnvelope wraps the signed-in user in login and register responses.
type UserEnvelope struct {
	User User `json:"user"`
}

// ChangePasswordRequest is the body of PUT /api/auth/password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
	NewPassword     string `json:"new_password" format:"password" minLength:"10"`
}

// UpdateProfileRequest is the body of PUT /api/users/me/profile.
type UpdateProfileRequest struct {
	// DisplayName: empty resets it to the username.
	DisplayName string `json:"display_name" maxLength:"24" binding:"optional"`
	Bio         string `json:"bio" maxLength:"250" binding:"optional"`
}

// SetLocaleRequest is the body of PUT /api/users/me/locale.
type SetLocaleRequest struct {
	Locale string `json:"locale" enums:"de,en"`
}

// SetPresenceRequest is the body of PUT /api/users/me/presence.
type SetPresenceRequest struct {
	// Presence is the chosen presence; "offline" is never chosen, it follows
	// from having no open WebSocket.
	Presence string `json:"presence" enums:"online,away,dnd,focus"`
}

// SetStatusRequest is the body of the status-line endpoints.
type SetStatusRequest struct {
	// StatusText: empty clears the status.
	StatusText string `json:"status_text" maxLength:"32"`
}
