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

func (h *Handler) mountUserAdmin(r chi.Router) {
	r.Get("/users", httpx.Handle(h.listUsers))
	r.Patch("/users/{id}", httpx.Handle(h.updateUser))
	r.Post("/users/{id}/disable", httpx.Handle(h.disableUser))
	r.Post("/users/{id}/enable", httpx.Handle(h.enableUser))
	r.Post("/users/{id}/sessions", httpx.Handle(h.revokeUserSessions))
	r.Post("/users/{id}/sessions/revoke", httpx.Handle(h.revokeUserSessions))
	r.Post("/users/{id}/password", httpx.Handle(h.setUserPassword))
	r.Post("/users/{id}/password-reset", httpx.Handle(h.resetUserPassword))
	r.Post("/users/{id}/kick", httpx.Handle(h.kickUser))
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

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req struct {
		IsDisabled *bool `json:"is_disabled"`
		Disabled   *bool `json:"disabled"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	dis := req.IsDisabled
	if dis == nil {
		dis = req.Disabled
	}
	if dis != nil {
		if err := SetDisabled(r.Context(), h.Sessions.DB, id, *dis); err != nil {
			return err
		}
		if *dis && h.Live != nil {
			h.Live.KickFromVoice(id)
			h.Live.DisconnectUser(id)
		}
		h.Events.Broadcast("user_update", map[string]any{"id": id, "disabled": *dis})
	}
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
		NewPassword string `json:"new_password"`
		Password    string `json:"password"`
	}
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}
	}
	newPw := req.NewPassword
	if newPw == "" {
		newPw = req.Password
	}
	if newPw != "" {
		if err := ValidatePassword(newPw); err != nil {
			return err
		}
		if _, err := targetUser(r.Context(), h.Sessions.DB, id); err != nil {
			return err
		}
		hash, err := HashPassword(newPw)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		tag, err := h.Sessions.DB.Exec(r.Context(), `UPDATE users SET password_hash = $1, token_version = token_version + 1,
			updated_at = NOW() WHERE id = $2`, hash, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound("user not found")
		}
		if h.Live != nil {
			h.Live.DisconnectUser(id)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	return h.resetUserPassword(w, r)
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
		return httpx.ErrInvalidInput("user is not in a Tafelrunde")
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
