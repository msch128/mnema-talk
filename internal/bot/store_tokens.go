package bot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Tokens lists the tokens of a bot, newest first.
func Tokens(ctx context.Context, q db.Querier, botID uuid.UUID) ([]Token, error) {
	rows, err := q.Query(ctx, `
		SELECT id, name, hint, expires_at, last_used_at, revoked_at, created_at,
		       CASE WHEN created_by IS NULL THEN 'unbound' WHEN minter_inferred THEN 'inferred' ELSE '' END
		FROM bot_tokens WHERE bot_id = $1 ORDER BY created_at DESC, id`, botID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	now := time.Now()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Token, error) {
		var t Token
		var revoked *time.Time
		err := row.Scan(&t.ID, &t.Name, &t.Hint, &t.ExpiresAt, &t.LastUsedAt, &revoked, &t.CreatedAt, &t.IssuerBinding)
		t.State = tokenState(revoked, t.ExpiresAt, now)
		return t, err
	})
}

func tokenState(revoked, expires *time.Time, now time.Time) string {
	switch {
	case revoked != nil:
		return "revoked"
	case expires != nil && !expires.After(now):
		return "expired"
	}
	return "active"
}

// IssueToken adds a token to a bot and returns the secret, the only time it
// exists in clear.
func IssueToken(ctx context.Context, p *db.Pool, botID uuid.UUID, name string, expires *time.Time, minter uuid.UUID) (Token, string, error) {
	var t Token
	var secret string
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if err := lockBot(ctx, tx, botID); err != nil {
			return err
		}
		var err error
		t, secret, err = insertToken(ctx, tx, botID, name, expires, minter)
		return err
	})
	return finishIssue(t, secret, err)
}

// RegenerateToken replaces an active token by a new one with the same name and
// expiry, in one transaction: the old one is revoked only if the new one is
// issued. A token that is already expired or revoked cannot be regenerated
// (its expiry would make the new secret useless at once).
func RegenerateToken(ctx context.Context, p *db.Pool, botID, tokenID, minter uuid.UUID) (Token, string, error) {
	var t Token
	var secret string
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if err := lockBot(ctx, tx, botID); err != nil {
			return err
		}
		var name string
		var expires, revoked *time.Time
		err := tx.QueryRow(ctx, `SELECT name, expires_at, revoked_at FROM bot_tokens WHERE id = $1 AND bot_id = $2 FOR UPDATE`, tokenID, botID).Scan(&name, &expires, &revoked)
		if errors.Is(err, pgx.ErrNoRows) {
			return errTokenNotFound
		}
		if err != nil {
			return err
		}
		if tokenState(revoked, expires, time.Now()) != "active" {
			return httpx.ErrInvalidInput("only an active token can be regenerated; create a new one")
		}
		if _, err := tx.Exec(ctx, `UPDATE bot_tokens SET revoked_at = NOW() WHERE id = $1`, tokenID); err != nil {
			return err
		}
		t, secret, err = insertToken(ctx, tx, botID, name, expires, minter)
		return err
	})
	return finishIssue(t, secret, err)
}

// insertToken adds a token to a locked bot, within the cap on active tokens.
func insertToken(ctx context.Context, tx pgx.Tx, botID uuid.UUID, name string, expires *time.Time, minter uuid.UUID) (Token, string, error) {
	secret, hash, hint := NewToken()
	var t Token
	var active int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM bot_tokens WHERE bot_id = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > NOW())`, botID).Scan(&active); err != nil {
		return t, "", err
	}
	if active >= MaxTokens {
		return t, "", httpx.ErrConflict(fmt.Sprintf("a bot can have at most %d active tokens", MaxTokens))
	}
	err := tx.QueryRow(ctx, `
		INSERT INTO bot_tokens (bot_id, name, token_hash, hint, expires_at, created_by) VALUES ($1, $2, $3, $4, $5, NULLIF($6, '00000000-0000-0000-0000-000000000000'::uuid))
		RETURNING id, name, hint, expires_at, last_used_at, created_at`,
		botID, name, hash, hint, expires, minter).Scan(&t.ID, &t.Name, &t.Hint, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
	return t, secret, err
}

func finishIssue(t Token, secret string, err error) (Token, string, error) {
	if err != nil {
		if _, ok := httpx.AsAPIError(err); ok {
			return Token{}, "", err
		}
		return Token{}, "", fmt.Errorf("issue token: %w", err)
	}
	t.State = tokenState(nil, t.ExpiresAt, time.Now())
	return t, secret, nil
}

// RevokeToken ends a token at once. A token already revoked stays so.
func RevokeToken(ctx context.Context, p *db.Pool, botID, tokenID uuid.UUID) error {
	tag, err := p.Exec(ctx, `
		UPDATE bot_tokens SET revoked_at = COALESCE(revoked_at, NOW()) WHERE id = $1 AND bot_id = $2`, tokenID, botID)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errTokenNotFound
	}
	return nil
}

// CheckExpiry validates an expiry an admin asked for.
func CheckExpiry(t, now time.Time) error {
	if !t.After(now) {
		return httpx.ErrInvalidInput("expires_at must be in the future")
	}
	if t.After(now.Add(MaxTokenLifetime)) {
		return httpx.ErrInvalidInput("expires_at is too far in the future")
	}
	return nil
}
