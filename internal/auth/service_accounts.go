package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Register creates a regular user, consuming one use of a valid invite code.
func Register(ctx context.Context, p *db.Pool, username, displayName, password, inviteCode string) (*User, error) {
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	displayName, err := httpx.CleanText("display_name", displayName, MaxDisplayNameLen, false)
	if err != nil {
		return nil, err
	}
	if displayName == "" {
		displayName = defaultDisplayName(username)
	}
	inviteCode = strings.TrimSpace(inviteCode)
	if inviteCode == "" || len(inviteCode) > maxInviteCodeChars {
		return nil, ErrInvalidInvite
	}

	// Cheap pre-check so unauthenticated guesses never cost a bcrypt hash;
	// the transaction below re-checks under a row lock.
	var known bool
	if err := p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM invites WHERE code = $1)`, inviteCode).Scan(&known); err != nil {
		return nil, fmt.Errorf("invite lookup: %w", err)
	}
	if !known {
		return nil, ErrInvalidInvite
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	var user *User
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		var inviteID uuid.UUID
		var maxUses *int
		var usesCount int
		var expiresAt *time.Time
		err := tx.QueryRow(ctx,
			`SELECT id, max_uses, uses_count, expires_at FROM invites WHERE code = $1 FOR UPDATE`,
			inviteCode).Scan(&inviteID, &maxUses, &usesCount, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidInvite
		}
		if err != nil {
			return fmt.Errorf("invite lookup: %w", err)
		}
		if expiresAt != nil && time.Now().After(*expiresAt) {
			return ErrInviteExpired
		}
		if maxUses != nil && usesCount >= *maxUses {
			return ErrInviteExhausted
		}

		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE LOWER(username) = LOWER($1))`, username).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return ErrUsernameTaken
		}

		user, err = scanUser(tx.QueryRow(ctx, `
			INSERT INTO users (username, display_name, password_hash, role, invite_id)
			VALUES ($1, $2, $3, 'user', $4)
			RETURNING `+userColumns, username, displayName, hash, inviteID))
		if err != nil {
			// Also covers a concurrent sign-up racing past the check above,
			// whichever unique index on the username catches it.
			if isUniqueViolation(err) {
				return ErrUsernameTaken
			}
			return fmt.Errorf("insert user: %w", err)
		}

		_, err = tx.Exec(ctx, `UPDATE invites SET uses_count = uses_count + 1 WHERE id = $1`, inviteID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

// ChangePassword verifies the current password, stores the new hash and bumps the
// token version, which signs out every other session. It returns the new version.
func ChangePassword(ctx context.Context, p *db.Pool, userID uuid.UUID, current, next string) (int, error) {
	if err := ValidatePassword(next); err != nil {
		return 0, err
	}
	var hash, role string
	if err := p.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash); err != nil {
		return 0, fmt.Errorf("load password hash: %w", err)
	}
	if err := p.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, userID).Scan(&role); err != nil {
		return 0, fmt.Errorf("load role: %w", err)
	}
	if err := ValidatePasswordFor(role, next); err != nil {
		return 0, err
	}
	if !CheckPassword(current, hash) {
		return 0, ErrWrongPassword
	}
	newHash, err := HashPassword(next)
	if err != nil {
		return 0, fmt.Errorf("hash password: %w", err)
	}
	var tv int
	err = p.QueryRow(ctx, `
		UPDATE users SET password_hash = $1, token_version = token_version + 1, updated_at = NOW()
		WHERE id = $2 RETURNING token_version`, newHash, userID).Scan(&tv)
	return tv, err
}

// RevokeSessions invalidates every session of userID ("log out everywhere").
func RevokeSessions(ctx context.Context, p db.Querier, userID uuid.UUID) error {
	_, err := p.Exec(ctx, `UPDATE users SET token_version = token_version + 1, updated_at = NOW() WHERE id = $1`, userID)
	return err
}

// UpdateProfile changes display name and bio.
func UpdateProfile(ctx context.Context, p *db.Pool, userID uuid.UUID, displayName, bio string) (*User, error) {
	displayName, err := httpx.CleanText("display_name", displayName, MaxDisplayNameLen, false)
	if err != nil {
		return nil, err
	}
	bio, err = httpx.CleanText("bio", bio, MaxBioLen, false)
	if err != nil {
		return nil, err
	}
	return scanUser(p.QueryRow(ctx, `
		UPDATE users
		SET display_name = CASE WHEN $1 = '' THEN LEFT(username, 24) ELSE $1 END, bio = $2, updated_at = NOW()
		WHERE id = $3
		RETURNING `+userColumns, displayName, bio, userID))
}

// defaultDisplayName is the username, cut to the display name limit.
func defaultDisplayName(username string) string {
	r := []rune(username)
	if len(r) > MaxDisplayNameLen {
		r = r[:MaxDisplayNameLen]
	}
	return string(r)
}

// SetPresence stores the presence userID chose.
func SetPresence(ctx context.Context, p *db.Pool, userID uuid.UUID, presence string) (*User, error) {
	if !ValidPresence(presence) {
		return nil, httpx.ErrInvalidInput("presence must be online, away, dnd or focus")
	}
	u, err := scanUser(p.QueryRow(ctx, `
		UPDATE users SET presence = $1, updated_at = NOW() WHERE id = $2
		RETURNING `+userColumns, presence, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("user not found")
	}
	return u, err
}

// SetStatusText stores userID's status line; an empty text clears it.
func SetStatusText(ctx context.Context, p db.Querier, userID uuid.UUID, text string) (*User, error) {
	text, err := httpx.CleanText("status_text", text, MaxStatusTextLen, false)
	if err != nil {
		return nil, err
	}
	u, err := scanUser(p.QueryRow(ctx, `
		UPDATE users SET status_text = $1, updated_at = NOW() WHERE id = $2
		RETURNING `+userColumns, text, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("user not found")
	}
	return u, err
}

// LoadStats fills u's activity totals.
func LoadStats(ctx context.Context, p *db.Pool, u *User) error {
	return p.QueryRow(ctx, `
		SELECT voice_seconds, (SELECT COUNT(*) FROM messages WHERE user_id = $1)
		FROM users WHERE id = $1`, u.ID).Scan(&u.VoiceSeconds, &u.MessageCount)
}
