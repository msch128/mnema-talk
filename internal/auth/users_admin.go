package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// LiveControl acts on a user's live connections (implemented by the WebSocket hub).
type LiveControl interface {
	// KickFromVoice ends the user's voice presence; false if they were not in one.
	KickFromVoice(userID uuid.UUID) bool
	// DisconnectUser closes all of the user's live connections.
	DisconnectUser(userID uuid.UUID)
	// SetPresence applies a newly chosen presence to the user's live status.
	SetPresence(userID uuid.UUID, presence string)
}

// AdminUser is a user as the admin panel lists it.
type AdminUser struct {
	User
	Disabled   bool       `json:"disabled"`
	LastSeenAt *time.Time `json:"last_seen_at"`
}

// ListUsersForAdmin returns every account, admins first, then by name.
func ListUsersForAdmin(ctx context.Context, p *db.Pool) ([]AdminUser, error) {
	rows, err := p.Query(ctx, `SELECT `+userColumns+`, disabled_at IS NOT NULL, last_seen_at
		FROM users ORDER BY role = 'admin' DESC, LOWER(username)`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := make([]AdminUser, 0)
	for rows.Next() {
		var au AdminUser
		u, err := scanUser(rows, &au.Disabled, &au.LastSeenAt)
		if err != nil {
			return nil, err
		}
		au.User = *u
		users = append(users, au)
	}
	return users, rows.Err()
}

// targetUser loads the user an admin action applies to. Admin accounts are
// never targets: the single administrator cannot lock themselves out.
func targetUser(ctx context.Context, p *db.Pool, id uuid.UUID) (*User, error) {
	u, err := GetUser(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if u.IsAdmin() {
		return nil, httpx.ErrInvalidInput("this action is not available for the administrator account")
	}
	return u, nil
}

// SetDisabled disables (and logs out) or re-enables a member.
func SetDisabled(ctx context.Context, p *db.Pool, id uuid.UUID, disabled bool) error {
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
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
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
	r.Get("/users", httpx.Handle(h.listUsers))
	r.Post("/users/{id}/disable", httpx.Handle(h.disableUser))
	r.Post("/users/{id}/enable", httpx.Handle(h.enableUser))
	r.Post("/users/{id}/sessions/revoke", httpx.Handle(h.revokeUserSessions))
	r.Post("/users/{id}/password", httpx.Handle(h.setUserPassword))
	r.Post("/users/{id}/password-reset", httpx.Handle(h.resetUserPassword))
	r.Post("/users/{id}/kick", httpx.Handle(h.kickUser))
	r.Put("/users/{id}/status", httpx.Handle(h.setUserStatus))
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) error {
	users, err := ListUsersForAdmin(r.Context(), h.Sessions.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, users)
	return nil
}

func (h *Handler) disableUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := SetDisabled(r.Context(), h.Sessions.DB, id, true); err != nil {
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

func (h *Handler) enableUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := SetDisabled(r.Context(), h.Sessions.DB, id, false); err != nil {
		return err
	}
	h.Events.Broadcast("user_update", map[string]any{"id": id, "disabled": false})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) revokeUserSessions(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := targetUser(r.Context(), h.Sessions.DB, id); err != nil {
		return err
	}
	if err := RevokeSessions(r.Context(), h.Sessions.DB, id); err != nil {
		return err
	}
	if h.Live != nil {
		h.Live.DisconnectUser(id)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

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
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"password": password})
	return nil
}

func (h *Handler) setUserPassword(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req struct {
		Password string `json:"password"`
	}
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

func (h *Handler) kickUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := GetUser(r.Context(), h.Sessions.DB, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound("user not found")
		}
		return err
	}
	if h.Live == nil || !h.Live.KickFromVoice(id) {
		return httpx.ErrInvalidInput("user is not in a Talk")
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// setUserStatus lets an admin edit or clear any member's status line.
func (h *Handler) setUserStatus(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	return h.writeStatus(w, r, id)
}
