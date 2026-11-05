package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

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
	rows, err := p.Query(ctx, `SELECT `+UserColumns+`, disabled_at IS NOT NULL, last_seen_at, banned_at IS NOT NULL,
		       CASE WHEN timeout_until > NOW() THEN timeout_until END
		FROM users WHERE NOT is_bot ORDER BY role = 'admin' DESC, LOWER(username)`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := make([]AdminUser, 0)
	for rows.Next() {
		var au AdminUser
		u, err := ScanUser(rows, &au.Disabled, &au.LastSeenAt, &au.Banned, &au.TimeoutUntil)
		if err != nil {
			return nil, err
		}
		au.User = *u
		users = append(users, au)
	}
	return users, rows.Err()
}

// TargetUser loads the user an admin action applies to. Admin accounts are
// never targets: the single administrator cannot lock themselves out.
func TargetUser(ctx context.Context, p db.Querier, id uuid.UUID) (*User, error) {
	if err := RefuseDeleted(ctx, p, id); err != nil {
		return nil, err
	}
	u, err := GetUser(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if u.IsAdmin() {
		return nil, ErrOwnerAction
	}
	return u, nil
}

// SetDisabled disables (and logs out) or re-enables a member.
func SetDisabled(ctx context.Context, p db.Querier, id uuid.UUID, disabled bool) error {
	if _, err := TargetUser(ctx, p, id); err != nil {
		return err
	}
	if disabled {
		_, err := p.Exec(ctx, `UPDATE users SET disabled_at = COALESCE(disabled_at, NOW()),
			token_version = token_version + 1, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
		return err
	}
	_, err := p.Exec(ctx, `UPDATE users SET disabled_at = NULL, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	return err
}

// ResetPassword sets a random temporary password, ends all sessions and
// returns the password once; it is never stored in plain text.
func ResetPassword(ctx context.Context, p *db.Pool, id uuid.UUID) (string, error) {
	if _, err := TargetUser(ctx, p, id); err != nil {
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
	err = storePassword(ctx, p, id, hash)
	if err != nil {
		return "", err
	}
	return password, nil
}

// storePassword replaces the hash of an active account, ends its sessions and
// drops its pending reset links in one transaction, so the change is never
// reported as failed after it took effect.
func storePassword(ctx context.Context, p *db.Pool, id uuid.UUID, hash string) error {
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET password_hash = $1, token_version = token_version + 1,
			updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL`, hash, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound("user not found")
		}
		return DropTokens(ctx, tx, id, PurposePasswordReset)
	})
}

// SetPassword sets a password chosen by the admin and ends all sessions.
func SetPassword(ctx context.Context, p *db.Pool, id uuid.UUID, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	if _, err := TargetUser(ctx, p, id); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return storePassword(ctx, p, id, hash)
}

// ErrOwnerAction is the refusal of an admin action aimed at the Owner account.
var ErrOwnerAction = httpx.ErrInvalidInput("this action is not available for the Owner account")
