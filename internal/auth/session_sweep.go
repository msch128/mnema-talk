package auth

import (
	"context"
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
		rows, err := tx.Query(ctx, `DELETE FROM user_sessions WHERE `+InactiveSessionSQL+` RETURNING jti, expires_at`)
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
		return EndSessions(ctx, tx, jtis, expires)
	})
	s.pruneTouched()
	if err != nil {
		// Rolled back: no session ended, so none may have its sockets closed.
		return nil, err
	}
	return jtis, nil
}
