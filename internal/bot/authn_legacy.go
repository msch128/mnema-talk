package bot

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// bindRetry is how long a legacy token that could not be bound is left alone
// before the next attempt (each attempt costs a few queries).
const bindRetry = 5 * time.Minute

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

	for _, who := range a.bindCandidates(ctx, tokenID) {
		ok, err := a.minterQualifies(ctx, who, botUser)
		if err != nil {
			slog.Warn("legacy bot token: cannot decide its issuer yet", "token_id", tokenID, "err", err)
			return
		}
		if !ok {
			continue
		}
		tag, err := a.DB.Exec(ctx, `
			UPDATE bot_tokens SET created_by = $2, minter_inferred = TRUE WHERE id = $1 AND created_by IS NULL`, tokenID, who)
		if err != nil {
			slog.Warn("legacy bot token: binding failed", "token_id", tokenID, "err", err)
			return
		}
		if tag.RowsAffected() == 1 {
			slog.Info("legacy bot token bound to its likeliest issuer", "audit", "bot_token_bound", "token_id", tokenID, "bound_to", who)
		}
		return
	}
	slog.Warn("legacy bot token stays unbound: nobody who could have issued it qualifies; regenerate it", "audit", "bot_token_unbound", "token_id", tokenID)
}

// bindCandidates lists who may have issued a legacy token, likeliest first.
func (a *Authenticator) bindCandidates(ctx context.Context, tokenID uuid.UUID) []uuid.UUID {
	var out []uuid.UUID
	var creator *uuid.UUID
	if err := a.DB.QueryRow(ctx, `
		SELECT b.created_by FROM bot_tokens t JOIN bots b ON b.id = t.bot_id WHERE t.id = $1`, tokenID).Scan(&creator); err == nil && creator != nil {
		out = append(out, *creator)
	}
	var owner uuid.UUID
	if err := a.DB.QueryRow(ctx, `
		SELECT id FROM users WHERE role = 'admin' AND NOT is_bot AND `+auth.ActiveSQL("")+` ORDER BY created_at LIMIT 1`).Scan(&owner); err == nil && (len(out) == 0 || out[0] != owner) {
		out = append(out, owner)
	}
	return out
}
