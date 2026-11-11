package bot

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
)

// bindRetry is how long a legacy token that could not be bound is left alone
// before the next attempt (each attempt costs a few queries).
const bindRetry = 5 * time.Minute

// legacyTokenStore is what binding a legacy token reads and writes. It is an
// interface so that a test can fail one step by its name.
type legacyTokenStore interface {
	// botCreator is the account that created the token's bot (nil when it is
	// not recorded, pgx.ErrNoRows when the token is gone).
	botCreator(ctx context.Context, tokenID uuid.UUID) (*uuid.UUID, error)
	// owner is the Owner's account (pgx.ErrNoRows without an active one).
	owner(ctx context.Context) (uuid.UUID, error)
	// bind records who as the issuer of a token that has none and reports
	// whether it did.
	bind(ctx context.Context, tokenID, who uuid.UUID) (bool, error)
}

// pgLegacyTokens is the legacyTokenStore of the database.
type pgLegacyTokens struct{ db *db.Pool }

func (s pgLegacyTokens) botCreator(ctx context.Context, tokenID uuid.UUID) (*uuid.UUID, error) {
	var creator *uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT b.created_by FROM bot_tokens t JOIN bots b ON b.id = t.bot_id WHERE t.id = $1`, tokenID).Scan(&creator)
	return creator, err
}

func (s pgLegacyTokens) owner(ctx context.Context) (uuid.UUID, error) {
	var owner uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT id FROM users WHERE role = 'admin' AND NOT is_bot AND `+auth.ActiveSQL("")+` ORDER BY created_at LIMIT 1`).Scan(&owner)
	return owner, err
}

func (s pgLegacyTokens) bind(ctx context.Context, tokenID, who uuid.UUID) (bool, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE bot_tokens SET created_by = $2, minter_inferred = TRUE WHERE id = $1 AND created_by IS NULL`, tokenID, who)
	return err == nil && tag.RowsAffected() == 1, err
}

// legacy is the store bindLegacy works on: the database unless a test put
// another one in.
func (a *Authenticator) legacy() legacyTokenStore {
	if a.legacyStore != nil {
		return a.legacyStore
	}
	return pgLegacyTokens{a.DB}
}

// bindLegacy binds a token that has no recorded issuer (issued before 1.0) to
// a person, on its first use. The real issuer is unknown, so it picks the
// likeliest one who still qualifies right now: the account that created the
// bot, else the Owner. Choosing someone who qualifies now means the token
// keeps working today; from then on it is re-checked like every other token,
// so it ends when that person is demoted or disabled. A token for which
// nobody qualifies stays unbound and keeps working: this never revokes
// anything (a revocation would break bots whose owners did nothing wrong), the
// admin console lists such tokens as unbound. Database trouble is not a
// reason to refuse the request either.
func (a *Authenticator) bindLegacy(ctx context.Context, tokenID, botUser uuid.UUID) {
	now := a.now()
	a.mu.Lock()
	if tried, ok := a.bindTried[tokenID]; ok && now.Sub(tried) < bindRetry {
		a.mu.Unlock()
		return
	}
	if a.bindTried == nil || len(a.bindTried) > 4096 {
		a.bindTried = map[uuid.UUID]time.Time{}
	}
	a.bindTried[tokenID] = now
	a.mu.Unlock()

	candidates, err := a.bindCandidates(ctx, tokenID)
	if err != nil {
		slog.Warn("legacy bot token: cannot list its likely issuers yet", "token_id", tokenID, "err", err)
		return
	}
	for _, who := range candidates {
		ok, err := a.minterQualifies(ctx, who, botUser)
		if err != nil {
			slog.Warn("legacy bot token: cannot decide its issuer yet", "token_id", tokenID, "err", err)
			return
		}
		if !ok {
			continue
		}
		bound, err := a.legacy().bind(ctx, tokenID, who)
		if err != nil {
			slog.Warn("legacy bot token: binding failed", "token_id", tokenID, "err", err)
			return
		}
		if bound {
			slog.Info("legacy bot token bound to its likeliest issuer", "audit", "bot_token_bound", "token_id", tokenID, "bound_to", who)
		}
		return
	}
	slog.Warn("legacy bot token stays unbound: nobody who could have issued it qualifies; regenerate it", "audit", "bot_token_unbound", "token_id", tokenID)
}

// bindCandidates lists who may have issued a legacy token, likeliest first.
// A database error is returned, never skipped: dropping the creator would
// bind the token to the Owner for good.
func (a *Authenticator) bindCandidates(ctx context.Context, tokenID uuid.UUID) ([]uuid.UUID, error) {
	var out []uuid.UUID
	creator, err := a.legacy().botCreator(ctx, tokenID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if creator != nil {
		out = append(out, *creator)
	}
	owner, err := a.legacy().owner(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if len(out) == 0 || out[0] != owner {
		out = append(out, owner)
	}
	return out, nil
}
