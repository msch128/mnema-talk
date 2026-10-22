package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Single-use links sent by e-mail. The token is 256 random bits; only its
// SHA-256 is stored, so a copy of the database cannot be turned into a
// working link. Consuming a token is one atomic UPDATE, so two requests with
// the same link cannot both succeed.
const (
	purposeEmailVerify   = "email_verify"
	purposePasswordReset = "password_reset"

	// EmailVerifyTTL is how long a confirmation link works.
	EmailVerifyTTL = 24 * time.Hour
	// PasswordResetTTL is how long a reset link works.
	PasswordResetTTL = 30 * time.Minute

	tokenBytes = 32
	// tokenChars is the length of a token in its URL-safe text form.
	tokenChars = 43
	// tokensPerHour bounds the links issued for one account and purpose per
	// hour, in the database, so it holds across restarts and for any number of
	// clients.
	tokensPerHour = 5
)

// errInvalidLink is the one answer for every unusable link (unknown, used,
// expired, bound to an address that changed): nothing tells them apart.
var errInvalidLink = httpx.ErrInvalidInput("this link is invalid or has expired")

func hashToken(plain string) []byte {
	sum := sha256.Sum256([]byte(plain))
	return sum[:]
}

// wellFormedToken reports whether s can be a token; anything else is refused
// before the database is asked.
func wellFormedToken(s string) bool {
	if len(s) != tokenChars {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(raw) == tokenBytes
}

// issueToken stores a fresh token for the user and returns its plain text,
// which only ever goes into the mail. Earlier unused tokens of the same
// purpose stop working, so only the newest link is live. email is the address
// a verification token is bound to.
func issueToken(ctx context.Context, q db.Querier, userID uuid.UUID, purpose, email string, ttl time.Duration) (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	plain := base64.RawURLEncoding.EncodeToString(raw)
	// Superseded tokens stay as used rows: they still count toward the hourly
	// budget. Only rows long past their expiry are deleted.
	if _, err := q.Exec(ctx, `UPDATE account_tokens SET used_at = NOW()
		WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL`, userID, purpose); err != nil {
		return "", fmt.Errorf("retire tokens: %w", err)
	}
	if _, err := q.Exec(ctx, `DELETE FROM account_tokens WHERE expires_at < NOW() - INTERVAL '1 day'`); err != nil {
		return "", fmt.Errorf("prune tokens: %w", err)
	}
	_, err := q.Exec(ctx, `INSERT INTO account_tokens (user_id, purpose, token_hash, email, expires_at)
		VALUES ($1, $2, $3, $4, NOW() + $5 * INTERVAL '1 second')`, userID, purpose, hashToken(plain), email, int64(ttl.Seconds()))
	if err != nil {
		return "", fmt.Errorf("store token: %w", err)
	}
	return plain, nil
}

// recentTokens counts the tokens issued for the user and purpose in the last
// hour, used or not (a used token was a mail sent all the same).
func recentTokens(ctx context.Context, q db.Querier, userID uuid.UUID, purpose string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM account_tokens
		WHERE user_id = $1 AND purpose = $2 AND created_at > NOW() - INTERVAL '1 hour'`, userID, purpose).Scan(&n)
	return n, err
}

// issuedTooOften reports whether the hourly budget for mails of this purpose
// is spent.
func issuedTooOften(ctx context.Context, q db.Querier, userID uuid.UUID, purpose string) (bool, error) {
	n, err := recentTokens(ctx, q, userID, purpose)
	return n >= tokensPerHour, err
}

// claimedToken is what consuming a token yields.
type claimedToken struct {
	UserID uuid.UUID
	Email  string
}

// consumeToken marks the token used and returns whom it was issued to. The
// UPDATE only matches an unused, unexpired token of the purpose, so a replay,
// a late use or a token of the other purpose find nothing.
func consumeToken(ctx context.Context, q db.Querier, plain, purpose string) (claimedToken, error) {
	var c claimedToken
	if !wellFormedToken(plain) {
		return c, errInvalidLink
	}
	err := q.QueryRow(ctx, `UPDATE account_tokens SET used_at = NOW()
		WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > NOW()
		RETURNING user_id, email`, hashToken(plain), purpose).Scan(&c.UserID, &c.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, errInvalidLink
	}
	return c, err
}

// dropTokens ends every live link of the user for the given purposes (all
// purposes when none is named), e.g. after the address or the password
// changed. The rows stay, marked used, so they still count toward the hourly
// budget and changing the address cannot be used to get around it.
func dropTokens(ctx context.Context, q db.Querier, userID uuid.UUID, purposes ...string) error {
	if len(purposes) == 0 {
		purposes = []string{purposeEmailVerify, purposePasswordReset}
	}
	_, err := q.Exec(ctx, `UPDATE account_tokens SET used_at = NOW()
		WHERE user_id = $1 AND purpose = ANY($2) AND used_at IS NULL`, userID, purposes)
	return err
}
