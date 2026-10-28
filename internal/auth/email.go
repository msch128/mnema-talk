package auth

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// maxEmailLen is the longest valid address (RFC 5321).
const maxEmailLen = 254

var errInvalidEmail = httpx.ErrInvalidInput("enter a valid e-mail address")

// NormalizeEmail accepts a plain ASCII address ("user@host.tld") and returns
// it trimmed. Display names, comments, quoted parts and anything else that
// could be read two ways are refused: the address ends up in mail headers.
func NormalizeEmail(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if len(s) < 6 || len(s) > maxEmailLen {
		return "", errInvalidEmail
	}
	for _, r := range s {
		if r <= ' ' || r >= 0x7f {
			return "", errInvalidEmail
		}
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || a.Name != "" {
		return "", errInvalidEmail
	}
	at := strings.LastIndex(s, "@")
	if at < 1 || at > 64 || !strings.Contains(s[at+1:], ".") || strings.HasSuffix(s, ".") {
		return "", errInvalidEmail
	}
	return s, nil
}

// emailState is what the account stores about the address.
type emailState struct {
	Address  string
	Verified bool
	Locale   string
	Username string
}

func loadEmailState(ctx context.Context, q db.Querier, id uuid.UUID) (emailState, error) {
	var st emailState
	var addr *string
	err := q.QueryRow(ctx, `SELECT email, email_verified_at IS NOT NULL, locale, username FROM users WHERE id = $1`, id).
		Scan(&addr, &st.Verified, &st.Locale, &st.Username)
	if addr != nil {
		st.Address = *addr
	}
	return st, err
}

// storeEmail saves the address. Typing the already verified address again
// (any letter case) keeps it verified; any other address starts unverified,
// and links sent for the old one stop working.
func storeEmail(ctx context.Context, q db.Querier, id uuid.UUID, email string) (verified bool, err error) {
	err = q.QueryRow(ctx, `UPDATE users SET
			email_verified_at = CASE WHEN LOWER(email) = LOWER($2) THEN email_verified_at END,
			email = $2, updated_at = NOW()
		WHERE id = $1 RETURNING email_verified_at IS NOT NULL`, id, email).Scan(&verified)
	if err != nil {
		return false, err
	}
	if !verified {
		// Links sent for the old address, reset links included, are dead now.
		err = dropTokens(ctx, q, id)
	}
	return verified, err
}

// removeEmail forgets the address and every link that depended on it.
func removeEmail(ctx context.Context, q db.Querier, id uuid.UUID) error {
	if _, err := q.Exec(ctx, `UPDATE users SET email = NULL, email_verified_at = NULL, updated_at = NOW() WHERE id = $1`, id); err != nil {
		return err
	}
	return dropTokens(ctx, q, id)
}

var errAddressInUse = httpx.ErrConflict("this e-mail address is already confirmed for another account")

// confirmEmail marks the address of a verification token as confirmed. It
// only applies while the account still has exactly that address, is active and
// has not confirmed it yet; otherwise the link counts as invalid.
func confirmEmail(ctx context.Context, tx pgx.Tx, plain string) (uuid.UUID, error) {
	claim, err := consumeToken(ctx, tx, plain, purposeEmailVerify)
	if err != nil {
		return uuid.Nil, err
	}
	tag, err := tx.Exec(ctx, `UPDATE users SET email_verified_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND LOWER(email) = LOWER($2) AND email_verified_at IS NULL AND `+ActiveSQL("")+` AND NOT is_bot`,
		claim.UserID, claim.Email)
	if db.IsUniqueViolation(err) {
		return uuid.Nil, errAddressInUse
	}
	if err != nil {
		return uuid.Nil, err
	}
	if tag.RowsAffected() == 0 {
		return uuid.Nil, errInvalidLink
	}
	return claim.UserID, nil
}

// userByVerifiedEmail finds the active person account whose confirmed address
// is email (any letter case).
func userByVerifiedEmail(ctx context.Context, q db.Querier, email string) (id uuid.UUID, username, locale, stored string, err error) {
	err = q.QueryRow(ctx, `SELECT id, username, locale, email FROM users
		WHERE LOWER(email) = LOWER($1) AND email_verified_at IS NOT NULL AND NOT is_bot AND `+ActiveSQL(""), email).
		Scan(&id, &username, &locale, &stored)
	return id, username, locale, stored, err
}

func notFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
