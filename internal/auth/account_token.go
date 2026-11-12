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
	PurposeEmailVerify   = "email_verify"
	PurposePasswordReset = "password_reset"

	// EmailVerifyTTL is how long a confirmation link works.
	EmailVerifyTTL = 24 * time.Hour
	// PasswordResetTTL is how long a reset link works.
	PasswordResetTTL = 30 * time.Minute

	TokenBytes = 32
	// tokenChars is the length of a token in its URL-safe text form.
	tokenChars = 43
	// TokensPerHour bounds the links issued for one account and purpose per
	// hour, in the database, so it holds across restarts and for any number of
	// clients (IssueAccountToken counts and inserts under one lock).
	TokensPerHour = 5
)

// ErrTooManyLinks is IssueAccountToken's answer when the hourly budget of the
// account and purpose is spent.
var ErrTooManyLinks = errors.New("hourly budget of account links spent")

// ErrInvalidLink is the one answer for every unusable link (unknown, used,
// expired, bound to an address that changed): nothing tells them apart.
var ErrInvalidLink = httpx.ErrInvalidInput("this link is invalid or has expired")

// HashToken is the stored form of an account link token.
func HashToken(plain string) []byte {
	sum := sha256.Sum256([]byte(plain))
	return sum[:]
}

// WellFormedToken reports whether s can be a token; anything else is refused
// before the database is asked.
func WellFormedToken(s string) bool {
	if len(s) != tokenChars {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(raw) == TokenBytes
}

// IssueAccountToken stores a fresh token for the user and returns its plain text,
// which only ever goes into the mail. Earlier unused tokens of the same
// purpose stop working, so only the newest link is live. email is the address
// a verification token is bound to. Once TokensPerHour links of the purpose
// were issued to the account in the last hour it stores nothing and returns
// ErrTooManyLinks.
//
// The count, the retire and the insert run in one transaction that first
// takes a transaction-scoped advisory lock keyed on the account and the
// purpose. Concurrent requests for one account and purpose therefore run one
// after the other, each seeing the rows the previous one committed, and
// nothing else waits on the lock (no row of users is locked). It is held for
// those three short statements and released at commit; the caller sends the
// mail after that, so no connection is held while mail goes out.
func IssueAccountToken(ctx context.Context, p *db.Pool, userID uuid.UUID, purpose, email string, ttl time.Duration) (string, error) {
	raw := make([]byte, TokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	plain := base64.RawURLEncoding.EncodeToString(raw)
	// Superseded tokens stay as used rows: they still count toward the hourly
	// budget. Only rows long past their expiry are deleted, outside the lock.
	if _, err := p.Exec(ctx, `DELETE FROM account_tokens WHERE expires_at < NOW() - INTERVAL '1 day'`); err != nil {
		return "", fmt.Errorf("prune tokens: %w", err)
	}
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		return replaceToken(ctx, tx, userID, purpose, email, HashToken(plain), ttl)
	})
	if err != nil {
		return "", err
	}
	return plain, nil
}

// replaceToken is IssueAccountToken's transaction: lock, count, retire, insert.
func replaceToken(ctx context.Context, tx pgx.Tx, userID uuid.UUID, purpose, email string, hash []byte, ttl time.Duration) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('account_token:' || $1::text || ':' || $2::text, 0))`, userID, purpose); err != nil {
		return fmt.Errorf("lock tokens: %w", err)
	}
	n, err := recentTokens(ctx, tx, userID, purpose)
	if err != nil {
		return fmt.Errorf("count tokens: %w", err)
	}
	if n >= TokensPerHour {
		return ErrTooManyLinks
	}
	if _, err := tx.Exec(ctx, `UPDATE account_tokens SET used_at = NOW()
		WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL`, userID, purpose); err != nil {
		return fmt.Errorf("retire tokens: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO account_tokens (user_id, purpose, token_hash, email, token_version, expires_at)
		VALUES ($1, $2, $3, $4, (SELECT token_version FROM users WHERE id = $1), NOW() + $5 * INTERVAL '1 second')`, userID, purpose, hash, email, int64(ttl.Seconds())); err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	return nil
}

// recentTokens counts the tokens issued for the user and purpose in the last
// hour, used or not (a used token was a mail sent all the same).
func recentTokens(ctx context.Context, q db.Querier, userID uuid.UUID, purpose string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM account_tokens
		WHERE user_id = $1 AND purpose = $2 AND created_at > NOW() - INTERVAL '1 hour'`, userID, purpose).Scan(&n)
	return n, err
}

// IssuedTooOften reports whether the hourly budget for mails of this purpose
// is spent.
func IssuedTooOften(ctx context.Context, q db.Querier, userID uuid.UUID, purpose string) (bool, error) {
	n, err := recentTokens(ctx, q, userID, purpose)
	return n >= TokensPerHour, err
}

// claimedToken is what consuming a token yields.
type claimedToken struct {
	UserID uuid.UUID
	Email  string
}

// ConsumeToken marks the token used and returns whom it was issued to. The
// UPDATE only matches an unused, unexpired token of the purpose, so a replay,
// a late use or a token of the other purpose find nothing.
func ConsumeToken(ctx context.Context, q db.Querier, plain, purpose string) (claimedToken, error) {
	var c claimedToken
	if !WellFormedToken(plain) {
		return c, ErrInvalidLink
	}
	err := q.QueryRow(ctx, `UPDATE account_tokens SET used_at = NOW()
		WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > NOW()
		AND (purpose <> 'password_reset' OR token_version = (SELECT token_version FROM users WHERE id = account_tokens.user_id))
		RETURNING user_id, email`, HashToken(plain), purpose).Scan(&c.UserID, &c.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrInvalidLink
	}
	return c, err
}

// DropTokens ends every live link of the user for the given purposes (all
// purposes when none is named), e.g. after the address or the password
// changed. The rows stay, marked used, so they still count toward the hourly
// budget and changing the address cannot be used to get around it.
func DropTokens(ctx context.Context, q db.Querier, userID uuid.UUID, purposes ...string) error {
	if len(purposes) == 0 {
		purposes = []string{PurposeEmailVerify, PurposePasswordReset}
	}
	_, err := q.Exec(ctx, `UPDATE account_tokens SET used_at = NOW()
		WHERE user_id = $1 AND purpose = ANY($2) AND used_at IS NULL`, userID, purposes)
	return err
}
