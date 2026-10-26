package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/msch128/mnema-talk/internal/db"
)

// touchInterval is the shortest gap between two writes of one session's
// last activity: inactivity is measured in days, so a few minutes of slack
// keep the write off the hot path.
const touchInterval = 5 * time.Minute

// recordSession stores or refreshes the user_sessions row of a verified
// session. A session issued before the table existed has no row yet; it gets
// one here with its inactivity clock starting now. A session that was signed
// out in the meantime is never put back.
func recordSession(ctx context.Context, q db.Querier, c *Claims, device string) error {
	issued := time.Now()
	if c.IssuedAt != nil {
		issued = c.IssuedAt.Time
	}
	_, err := q.Exec(ctx, `
		INSERT INTO user_sessions (user_id, jti, device, token_version, created_at, last_active_at, expires_at)
		SELECT $1, $2, $3, $4, $5, NOW(), $6
		WHERE NOT EXISTS (SELECT 1 FROM revoked_sessions WHERE jti = $2)
		ON CONFLICT (jti) DO UPDATE SET last_active_at = NOW(),
			device = CASE WHEN EXCLUDED.device <> '' THEN EXCLUDED.device ELSE user_sessions.device END`,
		c.UserID, c.ID, device, c.TokenVersion, issued, c.ExpiresAt.Time)
	return err
}

// touch notes that the request's session was used. At most one write per
// session and touchInterval; a failure is logged and never fails the request.
func (s *Sessions) touch(r *http.Request, c *Claims) {
	if c.ID == "" || c.ExpiresAt == nil {
		return
	}
	now := time.Now()
	if last, ok := s.touched.Load(c.ID); ok && now.Sub(last.(time.Time)) < touchInterval {
		return
	}
	s.touched.Store(c.ID, now)
	if err := recordSession(r.Context(), s.DB, c, DeviceLabel(r.UserAgent())); err != nil {
		slog.Warn("record session activity failed", "err", err)
	}
}

// pruneTouched forgets throttle entries that have run out.
func (s *Sessions) pruneTouched() {
	cutoff := time.Now().Add(-touchInterval)
	s.touched.Range(func(k, v any) bool {
		if v.(time.Time).Before(cutoff) {
			s.touched.Delete(k)
		}
		return true
	})
}

// endSessions signs the sessions out for good: their ids join revoked_sessions
// until their tokens would have expired anyway, and the push subscriptions of
// those sessions go with them. The caller removes the user_sessions rows.
func endSessions(ctx context.Context, q db.Querier, jtis []string, expires []time.Time) error {
	if len(jtis) == 0 {
		return nil
	}
	if _, err := q.Exec(ctx, `INSERT INTO revoked_sessions (jti, expires_at)
		SELECT * FROM unnest($1::text[], $2::timestamptz[]) ON CONFLICT DO NOTHING`, jtis, expires); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	if _, err := q.Exec(ctx, `DELETE FROM push_subscriptions WHERE session_id = ANY($1)`, jtis); err != nil {
		return fmt.Errorf("remove push subscriptions of ended sessions: %w", err)
	}
	return nil
}
