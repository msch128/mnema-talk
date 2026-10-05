// Package auth implements accounts, password handling, invite-only registration
// and cookie-based sessions.
package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"golang.org/x/crypto/bcrypt"
)

const (
	MinPasswordLength = 10
	// MaxPasswordBytes is bcrypt's input limit; longer inputs would be silently truncated.
	MaxPasswordBytes   = 72
	MaxDisplayNameLen  = 24
	MaxStatusTextLen   = 32
	MaxBioLen          = 250
	RoleAdmin          = "admin"
	RoleUser           = "user"
	bcryptCost         = 12
	maxInviteCodeChars = 64
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

// reservedUsernames are mention keywords (@all, @here) no account may take.
var reservedUsernames = map[string]bool{"all": true, "here": true}

// Presences a user can choose. Offline is not one of them: it only follows
// from having no open connection.
const (
	PresenceOnline = "online"
	PresenceAway   = "away"
	PresenceDND    = "dnd"
	PresenceFocus  = "focus"
)

// ValidPresence reports whether p is a presence a user may choose.
func ValidPresence(p string) bool {
	return p == PresenceOnline || p == PresenceAway || p == PresenceDND || p == PresenceFocus
}

// User is the public view of an account.
type User struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Bio         string    `json:"bio"`
	Role        string    `json:"role"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	StatusText  string    `json:"status_text"`
	// Presence is the chosen presence; others see the live one from the hub.
	Presence string `json:"presence,omitempty"`
	// Locale is the chosen UI language ("de", "en"); empty until chosen.
	Locale    string    `json:"locale"`
	CreatedAt time.Time `json:"created_at"`
}

// SupportedLocales are the UI languages the web app ships.
var SupportedLocales = map[string]bool{"de": true, "en": true}

// SetLocale stores the user's UI language.
func SetLocale(ctx context.Context, p *db.Pool, userID uuid.UUID, locale string) (*User, error) {
	if !SupportedLocales[locale] {
		return nil, httpx.ErrInvalidInput("locale must be de or en")
	}
	u, err := scanUser(p.QueryRow(ctx, `
		UPDATE users SET locale = $1, updated_at = NOW() WHERE id = $2
		RETURNING `+userColumns, locale, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("user not found")
	}
	return u, err
}

func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// Public is u as other members see it: the chosen presence stays private,
// they get the live one from the hub.
func (u User) Public() User {
	u.Presence = ""
	return u
}

// AvatarURL turns a stored avatar media ID into its API URL.
func AvatarURL(mediaID *string) string {
	if mediaID == nil || *mediaID == "" {
		return ""
	}
	return "/api/media/" + *mediaID
}

var (
	ErrInvalidCredentials = httpx.NewAPIError(401, httpx.CodeInvalidCredentials, "invalid username or password")
	ErrWrongPassword      = httpx.ErrForbidden("current password is incorrect")
	// Only returned for the correct password, so it reveals nothing to guessers.
	ErrAccountDisabled = httpx.NewAPIError(403, httpx.CodeAccountDisabled, "this account is disabled")
	ErrInvalidInvite   = httpx.ErrInvalidInput("invalid invite code")
	ErrInviteExpired   = httpx.ErrInvalidInput("invite code has expired")
	ErrInviteExhausted = httpx.ErrInvalidInput("invite code usage limit reached")
	ErrUsernameTaken   = httpx.ErrConflict("username is already taken")
)

// dummyHash keeps Login's timing identical for unknown usernames, so response
// times do not reveal which accounts exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("mnema-talk-timing-equaliser"), bcryptCost)

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	return string(b), err
}

func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ValidatePassword enforces the password policy.
func ValidatePassword(password string) error {
	if len([]rune(password)) < MinPasswordLength {
		return httpx.ErrInvalidInput(fmt.Sprintf("password must be at least %d characters", MinPasswordLength))
	}
	if len(password) > MaxPasswordBytes {
		return httpx.ErrInvalidInput(fmt.Sprintf("password must be at most %d bytes", MaxPasswordBytes))
	}
	return nil
}

// ValidateUsername enforces the username policy.
func ValidateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return httpx.ErrInvalidInput("username must be 3-32 characters: letters, digits, '_', '.', '-'")
	}
	if reservedUsernames[strings.ToLower(username)] {
		return httpx.ErrInvalidInput("this username is reserved")
	}
	return nil
}

const userColumns = `id, username, display_name, bio, role, avatar_s3_key, status_text, presence, locale, created_at`

func scanUser(row pgx.Row, extra ...any) (*User, error) {
	var u User
	var avatar *string
	dest := append([]any{&u.ID, &u.Username, &u.DisplayName, &u.Bio, &u.Role, &avatar, &u.StatusText, &u.Presence, &u.Locale, &u.CreatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	u.AvatarURL = AvatarURL(avatar)
	return &u, nil
}

// GetUser loads a user by ID; it returns a 404 APIError if absent.
func GetUser(ctx context.Context, p *db.Pool, id uuid.UUID) (*User, error) {
	u, err := scanUser(p.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("user not found")
	}
	return u, err
}

// sessionUser loads the user behind a session and its current token version.
func sessionUser(ctx context.Context, p *db.Pool, id uuid.UUID) (*User, int, error) {
	var tv int
	u, err := scanUser(p.QueryRow(ctx, `SELECT `+userColumns+`, token_version FROM users WHERE id = $1 AND disabled_at IS NULL`, id), &tv)
	return u, tv, err
}

// Login verifies credentials and returns the user plus its token version.
func Login(ctx context.Context, p *db.Pool, username, password string) (*User, int, error) {
	var hash string
	var tv int
	var disabled bool
	u, err := scanUser(p.QueryRow(ctx,
		`SELECT `+userColumns+`, password_hash, token_version, disabled_at IS NOT NULL FROM users WHERE LOWER(username) = LOWER($1)`,
		strings.TrimSpace(username)), &hash, &tv, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, 0, ErrInvalidCredentials
	}
	if err != nil {
		return nil, 0, fmt.Errorf("login query: %w", err)
	}
	if !CheckPassword(password, hash) {
		return nil, 0, ErrInvalidCredentials
	}
	if disabled {
		return nil, 0, ErrAccountDisabled
	}
	return u, tv, nil
}

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
			INSERT INTO users (username, display_name, password_hash, role)
			VALUES ($1, $2, $3, 'user')
			RETURNING `+userColumns, username, displayName, hash))
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
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
	var hash string
	if err := p.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash); err != nil {
		return 0, fmt.Errorf("load password hash: %w", err)
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
func RevokeSessions(ctx context.Context, p *db.Pool, userID uuid.UUID) error {
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
func SetStatusText(ctx context.Context, p *db.Pool, userID uuid.UUID, text string) (*User, error) {
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
