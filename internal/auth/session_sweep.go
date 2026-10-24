package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// DefaultSessionSweepEvery is how often the inactivity sweep runs. Requests
// are refused as soon as a session is over the limit (see sessionUser); the
// sweep makes it final, closes its sockets and removes its push subscriptions.
const DefaultSessionSweepEvery = 10 * time.Minute

// SweepSessions signs out every session that has been inactive for longer
// than the community's limit and drops the bookkeeping of sessions that ended
// otherwise (expired, or replaced by a password change or "sign out
// everywhere"). It returns the ids of the sessions it signed out.
func (s *Sessions) SweepSessions(ctx context.Context) ([]string, error) {
	var jtis []string
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM user_sessions
			WHERE expires_at < NOW()
			   OR token_version <> (SELECT token_version FROM users WHERE id = user_sessions.user_id)`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `DELETE FROM user_sessions WHERE `+inactiveSQL+` RETURNING jti, expires_at`)
		if err != nil {
			return err
		}
		var expires []time.Time
		for rows.Next() {
			var jti string
			var exp time.Time
			if err := rows.Scan(&jti, &exp); err != nil {
				rows.Close()
				return err
			}
			jtis, expires = append(jtis, jti), append(expires, exp)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		return endSessions(ctx, tx, jtis, expires)
	})
	s.pruneTouched()
	return jtis, err
}

// RunSessionSweep signs out inactive sessions every interval until ctx ends.
func (h *Handler) RunSessionSweep(ctx context.Context, every time.Duration) {
	if h.Sessions == nil || h.Sessions.DB == nil {
		return
	}
	if every <= 0 {
		every = DefaultSessionSweepEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		jtis, err := h.Sessions.SweepSessions(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Warn("session inactivity sweep failed", "err", err)
		}
		h.disconnect(jtis)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
