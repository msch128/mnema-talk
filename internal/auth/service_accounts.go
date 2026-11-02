package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/textcut"
)

// Register creates a regular user, consuming one use of a valid invite code.
func Register(ctx context.Context, p *db.Pool, username, displayName, password, inviteCode string) (*User, error) {
	username, displayName, inviteCode, err := cleanRegistration(username, displayName, password, inviteCode)
	if err != nil {
		return nil, err
	}

	// A legacy custom code below the current floor is guessable and no longer
	// works; it is refused like any invalid code.
	// A vanity link is public by design, so the floor does not apply to it.
	if _, vanity := vanitySlug(inviteCode); !vanity && IsWeakInviteCode(inviteCode) {
		return nil, ErrInvalidInvite
	}

	// Cheap pre-check so unauthenticated guesses never cost a bcrypt hash;
	// the transaction below re-checks under a row lock.
	if err := checkCodeKnown(ctx, p, inviteCode); err != nil {
		return nil, err
	}

	hash, err := hashPasswordCtx(ctx, password)
	if errors.Is(err, ErrServerBusy) || (err != nil && ctx.Err() != nil) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	// Hashing cannot be cancelled: a client that left meanwhile gets no account.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var user *User
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		inviteID, err := claimCode(ctx, tx, inviteCode)
		if err != nil {
			return err
		}
		user, err = insertInvitedUser(ctx, tx, username, displayName, hash, inviteID)
		return err
	})
	if err != nil {
		return nil, err
	}
	// Raid protection counts the join and may restrict the newcomer.
	automod.Of(p).OnJoin(ctx, user.ID)
	return user, nil
}

// cleanRegistration validates the sign-up fields and returns the username
// (normalised), display name (defaulted from the username) and invite code
// (trimmed) to use.
func cleanRegistration(username, displayName, password, inviteCode string) (string, string, string, error) {
	username = NormalizeUsername(username)
	if err := ValidateUsername(username); err != nil {
		return "", "", "", err
	}
	if err := ValidatePassword(password); err != nil {
		return "", "", "", err
	}
	displayName, err := httpx.CleanText("display_name", displayName, MaxDisplayNameLen, false)
	if err != nil {
		return "", "", "", err
	}
	if displayName == "" {
		displayName = defaultDisplayName(username)
	}
	inviteCode = strings.TrimSpace(inviteCode)
	if inviteCode == "" || len(inviteCode) > maxInviteCodeChars {
		return "", "", "", ErrInvalidInvite
	}
	return username, displayName, inviteCode, nil
}

// claimInvite locks the invite, checks that it is still usable and returns its
// ID. The use itself is counted by insertInvitedUser.
func claimInvite(ctx context.Context, tx pgx.Tx, code string) (uuid.UUID, error) {
	var inviteID uuid.UUID
	var maxUses *int
	var usesCount int
	var expiresAt *time.Time
	err := tx.QueryRow(ctx,
		`SELECT id, max_uses, uses_count, expires_at FROM invites WHERE code = $1 FOR UPDATE`,
		code).Scan(&inviteID, &maxUses, &usesCount, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrInvalidInvite
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("invite lookup: %w", err)
	}
	if expiresAt != nil && time.Now().After(*expiresAt) {
		return uuid.Nil, ErrInviteExpired
	}
	if maxUses != nil && usesCount >= *maxUses {
		return uuid.Nil, ErrInviteExhausted
	}
	return inviteID, nil
}

// insertInvitedUser creates the account and counts one use of its invite.
func insertInvitedUser(ctx context.Context, tx pgx.Tx, username, displayName, hash string, inviteID *uuid.UUID) (*User, error) {
	var taken bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE LOWER(username) = LOWER($1))`, username).Scan(&taken); err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrUsernameTaken
	}

	user, err := scanUser(tx.QueryRow(ctx, `
		INSERT INTO users (username, display_name, password_hash, role, invite_id)
		VALUES ($1, $2, $3, 'user', $4)
		RETURNING `+userColumns, username, displayName, hash, inviteID))
	if err != nil {
		// Also covers a concurrent sign-up racing past the check above,
		// whichever unique index on the username catches it.
		if db.IsUniqueViolation(err) {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}

	// The vanity link has no invite row: nothing to count.
	if inviteID == nil {
		return user, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE invites SET uses_count = uses_count + 1 WHERE id = $1`, *inviteID); err != nil {
		return nil, err
	}
	return user, nil
}

// ChangePassword verifies the current password, stores the new hash and bumps the
// token version, which signs out every other session. It returns the new version.
// The write only lands while the password is still the one that was verified: a
// reset or another change in between answers ErrWrongPassword.
func ChangePassword(ctx context.Context, p *db.Pool, userID uuid.UUID, current, next string) (int, error) {
	if err := ValidatePassword(next); err != nil {
		return 0, err
	}
	var hash, role string
	if err := p.QueryRow(ctx, `SELECT password_hash, role FROM users WHERE id = $1`, userID).Scan(&hash, &role); err != nil {
		return 0, fmt.Errorf("load password hash: %w", err)
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
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// The hash is compared under the row lock, so two changes that both
		// verified the same old password cannot both win.
		tag, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 AND password_hash = $2 FOR UPDATE`, userID, hash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrWrongPassword
		}
		if err := tx.QueryRow(ctx, `
			UPDATE users SET password_hash = $1, token_version = token_version + 1, updated_at = NOW()
			WHERE id = $2 RETURNING token_version`, newHash, userID).Scan(&tv); err != nil {
			return err
		}
		return dropTokens(ctx, tx, userID, purposePasswordReset)
	})
	return tv, err
}

// RevokeSessions invalidates every session of userID ("log out everywhere")
// and ends its pending password-reset links, in one statement.
func RevokeSessions(ctx context.Context, p db.Querier, userID uuid.UUID) error {
	_, err := p.Exec(ctx, `WITH bumped AS (
			UPDATE users SET token_version = token_version + 1, updated_at = NOW() WHERE id = $1
		)
		UPDATE account_tokens SET used_at = NOW()
		WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL`, userID, purposePasswordReset)
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

// UpdateDisplayName changes only the display name; the bio and everything else
// stay as stored. It cleans and limits the name like UpdateProfile.
func UpdateDisplayName(ctx context.Context, p *db.Pool, userID uuid.UUID, displayName string) (*User, error) {
	displayName, err := httpx.CleanText("display_name", displayName, MaxDisplayNameLen, false)
	if err != nil {
		return nil, err
	}
	u, err := scanUser(p.QueryRow(ctx, `
		UPDATE users
		SET display_name = CASE WHEN $1 = '' THEN LEFT(username, 24) ELSE $1 END, updated_at = NOW()
		WHERE id = $2
		RETURNING `+userColumns, displayName, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("user not found")
	}
	return u, err
}

// defaultDisplayName is the username, cut to the display name limit.
func defaultDisplayName(username string) string {
	return textcut.Runes(username, MaxDisplayNameLen)
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

// MaxStatusExpiry is how far ahead a status line may be set to clear itself.
const MaxStatusExpiry = 366 * 24 * time.Hour

// checkStatusExpiry accepts nil (never) or a time between now and
// MaxStatusExpiry ahead; an empty status carries no expiry.
func checkStatusExpiry(text string, expiresAt *time.Time) (*time.Time, error) {
	if expiresAt == nil || text == "" {
		return nil, nil
	}
	now := time.Now()
	if !expiresAt.After(now) {
		return nil, httpx.ErrInvalidInput("expires_at must be in the future")
	}
	if expiresAt.After(now.Add(MaxStatusExpiry)) {
		return nil, httpx.ErrInvalidInput("expires_at is too far ahead")
	}
	t := expiresAt.UTC().Truncate(time.Second)
	return &t, nil
}

// SetStatusText stores userID's status line; an empty text clears it. A
// non-nil expiresAt makes the line clear itself at that time.
func SetStatusText(ctx context.Context, p db.Querier, userID uuid.UUID, text string, expiresAt *time.Time) (*User, error) {
	text, err := httpx.CleanText("status_text", text, MaxStatusTextLen, false)
	if err != nil {
		return nil, err
	}
	expiresAt, err = checkStatusExpiry(text, expiresAt)
	if err != nil {
		return nil, err
	}
	u, err := scanUser(p.QueryRow(ctx, `
		UPDATE users SET status_text = $1, status_expires_at = $3, updated_at = NOW() WHERE id = $2
		RETURNING `+userColumns, text, userID, expiresAt))
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
