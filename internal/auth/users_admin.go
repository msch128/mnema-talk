package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
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
	// DisconnectSession closes the connections opened with one session id (a
	// single sign-out); the user's other sessions stay.
	DisconnectSession(jti string)
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
	if _, err := rand.Read(b); err != nil {
		return "", err
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
	return password, dropTokens(ctx, p, id, purposePasswordReset)
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
	if _, err = p.Exec(ctx, `UPDATE users SET password_hash = $1, token_version = token_version + 1,
		updated_at = NOW() WHERE id = $2`, hash, id); err != nil {
		return err
	}
	return dropTokens(ctx, p, id, purposePasswordReset)
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
	r.With(RequireOwner).Post("/users/{id}/two-factor/reset", httpx.Handle(h.resetUserTwoFactor))
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
	return h.Perms.Locked(ctx, actor.Subject(), func(tx pgx.Tx, snap *perm.Snapshot) error {
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
