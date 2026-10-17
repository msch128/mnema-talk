package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// LiveControl acts on a user's live connections (implemented by the WebSocket hub).
type LiveControl interface {
	// KickFromVoice ends the user's voice presence; false if they were not in one.
	KickFromVoice(userID uuid.UUID) bool
	// VoiceChannels lists the Talks the user is in (or lingers in).
	VoiceChannels(userID uuid.UUID) []uuid.UUID
	// KickFromChannel ends the user's presence in one Talk.
	KickFromChannel(userID, channelID uuid.UUID)
	// DisconnectUser closes all of the user's live connections.
	DisconnectUser(userID uuid.UUID)
	// SetPresence applies a newly chosen presence to the user's live status.
	SetPresence(userID uuid.UUID, presence string)
}

// AdminUser is a user as the admin panel lists it.
type AdminUser struct {
	User
	// RoleIDs are the account's custom roles, highest first.
	RoleIDs  []uuid.UUID `json:"role_ids,omitempty" binding:"optional" format:"uuid"`
	Disabled bool        `json:"disabled"`
	// Banned marks a disabled account that was banned (see GET /api/admin/bans).
	Banned bool `json:"banned"`
	// TimeoutUntil is set while the account is timed out.
	TimeoutUntil *time.Time `json:"timeout_until,omitempty" binding:"optional" format:"date-time"`
	LastSeenAt   *time.Time `json:"last_seen_at" format:"date-time" extensions:"x-nullable"`
	// LoginLocked is set while wrong passwords lock the account (or a client
	// address) out in memory. Only the Owner sees it set.
	LoginLocked bool `json:"login_locked" binding:"optional"`
}

// ListUsersForAdmin returns every account, admins first, then by name.
func ListUsersForAdmin(ctx context.Context, p *db.Pool) ([]AdminUser, error) {
	rows, err := p.Query(ctx, `SELECT `+userColumns+`, disabled_at IS NOT NULL, last_seen_at, banned_at IS NOT NULL,
		       CASE WHEN timeout_until > NOW() THEN timeout_until END
		FROM users WHERE NOT is_bot ORDER BY role = 'admin' DESC, LOWER(username)`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := make([]AdminUser, 0)
	for rows.Next() {
		var au AdminUser
		u, err := scanUser(rows, &au.Disabled, &au.LastSeenAt, &au.Banned, &au.TimeoutUntil)
		if err != nil {
			return nil, err
		}
		au.User = *u
		users = append(users, au)
	}
	return users, rows.Err()
}

var errOwnerAction = httpx.ErrInvalidInput("this action is not available for the Owner account")

// targetUser loads the user an admin action applies to. Admin accounts are
// never targets: the single administrator cannot lock themselves out.
func targetUser(ctx context.Context, p db.Querier, id uuid.UUID) (*User, error) {
	u, err := GetUser(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if u.IsAdmin() {
		return nil, errOwnerAction
	}
	return u, nil
}

// SetDisabled disables (and logs out) or re-enables a member.
func SetDisabled(ctx context.Context, p db.Querier, id uuid.UUID, disabled bool) error {
	if _, err := targetUser(ctx, p, id); err != nil {
		return err
	}
	if disabled {
		_, err := p.Exec(ctx, `UPDATE users SET disabled_at = COALESCE(disabled_at, NOW()),
			token_version = token_version + 1, updated_at = NOW() WHERE id = $1`, id)
		return err
	}
	_, err := p.Exec(ctx, `UPDATE users SET disabled_at = NULL, updated_at = NOW() WHERE id = $1`, id)
	return err
}

// ResetPassword sets a random temporary password, ends all sessions and
// returns the password once; it is never stored in plain text.
func ResetPassword(ctx context.Context, p *db.Pool, id uuid.UUID) (string, error) {
	if _, err := targetUser(ctx, p, id); err != nil {
		return "", err
	}
	b := make([]byte, 15)
	_, _ = rand.Read(b) // never fails since Go 1.24: it aborts the process instead
	password := base64.RawURLEncoding.EncodeToString(b)
	hash, err := HashPassword(password)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	tag, err := p.Exec(ctx, `UPDATE users SET password_hash = $1, token_version = token_version + 1,
		updated_at = NOW() WHERE id = $2`, hash, id)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", httpx.ErrNotFound("user not found")
	}
	return password, nil
}

// SetPassword sets a password chosen by the admin and ends all sessions.
func SetPassword(ctx context.Context, p *db.Pool, id uuid.UUID, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	if _, err := targetUser(ctx, p, id); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = p.Exec(ctx, `UPDATE users SET password_hash = $1, token_version = token_version + 1,
		updated_at = NOW() WHERE id = $2`, hash, id)
	return err
}

func (h *Handler) mountUserAdmin(r chi.Router) {
	r.With(RequireAnyPerm(h.Perms, perm.ManageMembers|perm.ManageRoles)).Get("/users", httpx.Handle(h.listUsers))
	members := r.With(RequirePerm(h.Perms, perm.ManageMembers))
	members.Post("/users/{id}/disable", httpx.Handle(h.disableUser))
	members.Post("/users/{id}/enable", httpx.Handle(h.enableUser))
	members.Post("/users/{id}/sessions/revoke", httpx.Handle(h.revokeUserSessions))
	members.Put("/users/{id}/status", httpx.Handle(h.setUserStatus))
	// Taking over an account is not a role's business: the Owner only.
	r.With(RequireOwner).Post("/users/{id}/password", httpx.Handle(h.setUserPassword))
	r.With(RequireOwner).Post("/users/{id}/password-reset", httpx.Handle(h.resetUserPassword))
	r.With(RequireOwner).Post("/users/{id}/login-lockout/clear", httpx.Handle(h.clearUserLockout))
	r.With(RequirePerm(h.Perms, perm.DisconnectMembers)).Post("/users/{id}/kick", httpx.Handle(h.kickUser))
	h.mountModeration(r)
}

// moderated runs fn on the member a moderation action aims at, after checking
// that the caller may act on them. With a permission service the check and fn
// run in one transaction under the permission lock, on the state as it is
// inside it: a concurrent role change cannot move the target out of reach (or
// the caller down) between the check and the write.
func (h *Handler) moderated(r *http.Request, id uuid.UUID, fn func(q db.Querier) error) error {
	ctx := r.Context()
	actor := UserFrom(ctx)
	if h.Perms == nil {
		target, err := GetUser(ctx, h.Sessions.DB, id)
		if err != nil {
			return err
		}
		if err := checkTarget(nil, actor, target); err != nil {
			return err
		}
		return fn(h.Sessions.DB)
	}
	return h.Perms.Locked(ctx, func(tx pgx.Tx, snap *perm.Snapshot) error {
		target, err := GetUser(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := checkTarget(snap, actor, target); err != nil {
			return err
		}
		return fn(tx)
	})
}

// checkTarget: never the Owner, only members below the caller's highest role.
func checkTarget(snap *perm.Snapshot, actor, target *User) error {
	if target.IsAdmin() {
		return errOwnerAction
	}
	if snap == nil || actor == nil || actor.IsAdmin() {
		return nil
	}
	return snap.CanModerate(actor.Subject(), target.Subject())
}

// moderate loads the member a moderation action aims at and checks that the
// caller may act on them: never the Owner, only members below the caller.
func (h *Handler) moderate(r *http.Request, id uuid.UUID) (*User, error) {
	target, err := GetUser(r.Context(), h.Sessions.DB, id)
	if err != nil {
		return nil, err
	}
	if err := h.authorizeTarget(r, target); err != nil {
		return nil, err
	}
	return target, nil
}

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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
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
	h.Events.Broadcast("user_update", map[string]any{"id": id, "disabled": true})
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
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
	h.Events.Broadcast("user_update", map[string]any{"id": id, "disabled": false})
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
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
	// The caller needs the permission in the Talk the member is in; a Talk
	// they may not use answers like no Talk at all, so it stays hidden.
	kicked := false
	if h.Live != nil {
		actor := UserFrom(r.Context()).Subject()
		for _, ch := range h.Live.VoiceChannels(id) {
			if err := h.Perms.RequireChannel(r.Context(), actor, ch, perm.ViewChannel|perm.DisconnectMembers); err != nil {
				if _, refused := httpx.AsAPIError(err); !refused {
					return err
				}
				continue
			}
			h.Live.KickFromChannel(id, ch)
			kicked = true
		}
	}
	if !kicked {
		return httpx.ErrInvalidInput("user is not in a Talk")
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
// @Param request body SetStatusRequest true "Request body."
// @Success 200 {object} User "Updated user (public view unless it is the caller)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
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
