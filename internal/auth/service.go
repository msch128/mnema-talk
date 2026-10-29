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
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"golang.org/x/crypto/bcrypt"
)

const (
	MinPasswordLength = 10
	// MinOwnerPasswordLength applies to administrator accounts (also the seeded one).
	MinOwnerPasswordLength = 14
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

// usernamePattern is the lookup shape: every name an account can hold, legacy
// ones included (login, mentions and URLs accept all of them).
var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{2,32}$`)

// newUsernamePattern is the rule for names chosen from now on: lowercase
// letters, digits, '_' and '.', 2-32 characters. Dot placement is checked in
// ValidateUsername.
var newUsernamePattern = regexp.MustCompile(`^[a-z0-9_.]{2,32}$`)

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
	ID          uuid.UUID `json:"id" format:"uuid"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name" maxLength:"24"`
	Bio         string    `json:"bio" maxLength:"250"`
	// Role is "admin" for the Owner (the one account named by ADMIN_USERNAME,
	// all-powerful and untouchable) and "user" for everybody else; what a user
	// may do beyond the defaults comes from roles (see GET /api/permissions).
	Role string `json:"role" enums:"admin,user"`
	// AvatarURL is the API URL of the avatar (/api/media/{id}); omitted when none.
	AvatarURL string `json:"avatar_url,omitempty" binding:"optional"`
	// BannerURL is the API URL of the profile banner (/api/media/{id}); omitted
	// when none and in every payload but the profile endpoints (GET
	// /api/users/{userID}, GET /api/auth/me and the banner routes).
	BannerURL string `json:"banner_url,omitempty" binding:"optional"`
	// StatusText is a free-text status line; empty when unset.
	StatusText string `json:"status_text" maxLength:"32"`
	// StatusExpiresAt is when the status line clears itself; omitted when it
	// stays until changed.
	StatusExpiresAt *time.Time `json:"status_expires_at,omitempty" binding:"optional" format:"date-time"`
	// Presence is the chosen presence, only present in responses about the
	// caller's own account; others see the live one from the hub.
	Presence string `json:"presence,omitempty" binding:"optional" enums:"online,away,dnd,focus"`
	// Activity totals, filled where they are shown (members, profiles):
	// time spent in voice rooms (finished stays) and messages sent.
	VoiceSeconds int64 `json:"voice_seconds,omitempty" binding:"optional"`
	MessageCount int64 `json:"message_count,omitempty" binding:"optional"`
	// RulesPending is true while the caller has to accept the community rules
	// (POST /api/onboarding/accept) before posting, reacting, uploading or
	// joining a Talk. Only set in responses about the caller's own account.
	RulesPending bool `json:"rules_pending,omitempty" binding:"optional"`
	// Locale is the chosen UI language ("de", "en"); empty until chosen.
	Locale    string    `json:"locale"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
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

// LoadBanner fills u's banner URL; profile endpoints call it, the user
// columns every other payload is built from do not carry the banner.
func LoadBanner(ctx context.Context, p db.Querier, u *User) error {
	var banner *string
	if err := p.QueryRow(ctx, `SELECT banner_s3_key FROM users WHERE id = $1`, u.ID).Scan(&banner); err != nil {
		return err
	}
	u.BannerURL = AvatarURL(banner)
	return nil
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

// passwordCost is the bcrypt cost new hashes use. It is always bcryptCost in
// production; only integration test binaries lower it (see
// testhooks_integration.go), because under -race every cost-12 hash takes
// seconds and the suites log in hundreds of times.
var passwordCost = bcryptCost

// dummyHash keeps Login's timing identical for unknown usernames, so response
// times do not reveal which accounts exist.
var dummyHash = newDummyHash()

func newDummyHash() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("mnema-talk-timing-equaliser"), passwordCost)
	if err != nil {
		return nil
	}
	return h
}

// ValidatePassword enforces the password policy for regular accounts.
func ValidatePassword(password string) error {
	return validatePasswordMin(password, MinPasswordLength)
}

// ValidatePasswordFor enforces the policy for a role: the Owner (admin) account
// is the one worth guessing, so it needs a longer password.
func ValidatePasswordFor(role, password string) error {
	if role == RoleAdmin {
		return validatePasswordMin(password, MinOwnerPasswordLength)
	}
	return ValidatePassword(password)
}

func validatePasswordMin(password string, min int) error {
	if len([]rune(password)) < min {
		return httpx.ErrInvalidInput(fmt.Sprintf("password must be at least %d characters", min))
	}
	if len(password) > MaxPasswordBytes {
		return httpx.ErrInvalidInput(fmt.Sprintf("password must be at most %d bytes", MaxPasswordBytes))
	}
	return nil
}

// NormalizeUsername applies the input normalisation of the sign-up form:
// surrounding space is trimmed, spaces become '_' and letters lower-case.
func NormalizeUsername(username string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(username), " ", "_"))
}

// ValidateUsername enforces the policy for new names (registration). Accounts
// that predate it keep their names; see usernamePattern.
func ValidateUsername(username string) error {
	if !newUsernamePattern.MatchString(username) ||
		strings.HasPrefix(username, ".") || strings.HasSuffix(username, ".") || strings.Contains(username, "..") {
		return httpx.ErrInvalidInput("username must be 2-32 characters: lowercase letters, digits, '_' and '.' (no leading, trailing or repeated '.')")
	}
	return checkReservedUsername(username)
}

func checkReservedUsername(username string) error {
	if reservedUsernames[strings.ToLower(username)] || strings.HasPrefix(strings.ToLower(username), deletedUsernamePrefix) {
		return httpx.ErrInvalidInput("this username is reserved")
	}
	return nil
}

// validateExistingUsername accepts any name an account can already hold. The
// ADMIN_USERNAME of a running install must keep booting.
func validateExistingUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return httpx.ErrInvalidInput("username must be 2-32 characters: letters, digits, '_', '.', '-'")
	}
	return checkReservedUsername(username)
}

// A status line whose time has passed reads as empty before RunStatusExpiry has cleared it.
var userColumns = `id, username, display_name, bio, role, avatar_s3_key, ` + StatusSQLFor("") + `, presence, locale, created_at`

func scanUser(row pgx.Row, extra ...any) (*User, error) {
	var u User
	var avatar *string
	dest := append([]any{&u.ID, &u.Username, &u.DisplayName, &u.Bio, &u.Role, &avatar, &u.StatusText, &u.StatusExpiresAt, &u.Presence, &u.Locale, &u.CreatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	u.AvatarURL = AvatarURL(avatar)
	return &u, nil
}

// GetUser loads a user by ID; it returns a 404 APIError if absent.
func GetUser(ctx context.Context, p db.Querier, id uuid.UUID) (*User, error) {
	u, err := scanUser(p.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("user not found")
	}
	return u, err
}

// GetActiveUser is GetUser for an account that may act (see ActiveSQL); a
// disabled, banned or deleted account, or a bot, is a 404 APIError like a
// missing one.
func GetActiveUser(ctx context.Context, p db.Querier, id uuid.UUID) (*User, error) {
	u, err := scanUser(p.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1 AND `+ActiveSQL("")+` AND NOT is_bot`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("user not found")
	}
	return u, err
}

// inactiveSQL is true for a user_sessions row that has had no request for
// longer than the community's inactivity limit.
const inactiveSQL = `last_active_at < NOW() - make_interval(days => (SELECT session_inactivity_days FROM instance_settings LIMIT 1))`

// sessionUser loads the user behind a session and its current token version.
// A session whose jti was revoked by logout (or a bot account) does not load.
func sessionUser(ctx context.Context, p *db.Pool, id uuid.UUID, jti string) (*User, int, error) {
	var tv int
	u, err := scanUser(p.QueryRow(ctx, `SELECT `+userColumns+`, token_version FROM users
		WHERE id = $1 AND `+ActiveSQL("")+` AND NOT is_bot
		AND NOT EXISTS (SELECT 1 FROM revoked_sessions WHERE jti = $2)
		AND NOT EXISTS (SELECT 1 FROM user_sessions WHERE jti = $2 AND `+inactiveSQL+`)`, id, jti), &tv)
	return u, tv, err
}

// Login verifies credentials and returns the user plus its token version.
func Login(ctx context.Context, p *db.Pool, username, password string) (*User, int, error) {
	var hash string
	var tv int
	var disabled bool
	u, err := scanUser(p.QueryRow(ctx,
		`SELECT `+userColumns+`, password_hash, token_version, `+InactiveSQL("")+` FROM users WHERE LOWER(username) = LOWER($1) AND NOT is_bot`,
		strings.TrimSpace(username)), &hash, &tv, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := burnPasswordCheck(ctx, password); err != nil {
			return nil, 0, err
		}
		return nil, 0, ErrInvalidCredentials
	}
	if err != nil {
		return nil, 0, fmt.Errorf("login query: %w", err)
	}
	ok, err := checkPasswordCtx(ctx, password, hash)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, ErrInvalidCredentials
	}
	if disabled {
		return nil, 0, bannedError(ctx, p, u.ID)
	}
	return u, tv, nil
}
