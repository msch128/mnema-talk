package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
)

// Revoke ends the live connections and the resumable sessions of a bot: of one
// token when tokenID is set, of all its tokens otherwise. Open connections get
// close code 4004, as for a failed login.
func (g *Gateway) Revoke(botID uuid.UUID, tokenID *uuid.UUID) {
	g.mu.Lock()
	var victims []*session
	for id, s := range g.byBot[botID] {
		if tokenID == nil || s.tokenID == *tokenID {
			victims = append(victims, s)
			delete(g.sessions, id)
			delete(g.byBot[botID], id)
		}
	}
	if len(g.byBot[botID]) == 0 {
		delete(g.byBot, botID)
	}
	g.mu.Unlock()
	for _, s := range victims {
		s.closeConn(CloseAuthFailed, "Authentication failed")
	}
}

// RevokeUser ends every session of the bot account userID, as Revoke does for a
// bot: its account was banned or disabled. Open connections get close code 4004.
func (g *Gateway) RevokeUser(userID uuid.UUID) {
	g.mu.Lock()
	var victims []*session
	for id, s := range g.sessions {
		if s.userID == userID {
			victims = append(victims, s)
			g.removeLocked(id, s)
		}
	}
	g.mu.Unlock()
	for _, s := range victims {
		s.closeConn(CloseAuthFailed, "Authentication failed")
	}
}

// expire drops detached sessions that were not resumed in time.
func (g *Gateway) expire() {
	now, window := time.Now(), g.timings().Resume
	g.mu.Lock()
	defer g.mu.Unlock()
	for id, s := range g.sessions {
		if s.expired(now, window) {
			g.removeLocked(id, s)
		}
	}
}

func (g *Gateway) removeLocked(id string, s *session) {
	delete(g.sessions, id)
	if m := g.byBot[s.botID]; m != nil {
		delete(m, id)
		if len(m) == 0 {
			delete(g.byBot, s.botID)
		}
	}
}

// validTokens returns which of the tokens are still good: not revoked, not
// expired and of a bot that is neither disabled nor banned.
func (g *Gateway) validTokens(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	var live []uuid.UUID
	rows, err := g.DB.Query(ctx, `
		SELECT t.id FROM bot_tokens t JOIN bots b ON b.id = t.bot_id JOIN users u ON u.id = b.user_id
		WHERE t.id = ANY($1) AND t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > NOW()) AND NOT b.disabled
		  AND `+auth.ActiveSQL("u"), ids)
	if err == nil {
		live, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
	valid := make(map[uuid.UUID]bool, len(live))
	for _, id := range live {
		valid[id] = true
	}
	return valid, err
}

// checkTokens closes the sessions whose token was revoked or expired, or whose
// bot was disabled or deleted, however that happened.
func (g *Gateway) checkTokens(ctx context.Context) {
	g.mu.Lock()
	ids := make([]uuid.UUID, 0, len(g.sessions))
	seen := map[uuid.UUID]bool{}
	for _, s := range g.sessions {
		if !seen[s.tokenID] {
			seen[s.tokenID] = true
			ids = append(ids, s.tokenID)
		}
	}
	g.mu.Unlock()
	if len(ids) == 0 || g.DB == nil {
		return
	}
	valid, err := g.validTokens(ctx, ids)
	if err != nil {
		slog.Warn("gateway token check", "err", err)
		return
	}
	for _, id := range ids {
		if !valid[id] {
			g.revokeToken(id)
		}
	}
}

// adopt hands a session over to the token that resumes it, unless the session
// ended meanwhile (a Revoke between the lookup and now), and checks that the
// token is still good: its revocation may have happened before the swap, when
// Revoke could not find the session under this token yet. The session ends
// when either fails.
func (g *Gateway) adopt(ctx context.Context, s *session, tokenID uuid.UUID) bool {
	g.mu.Lock()
	if g.sessions[s.id] != s {
		g.mu.Unlock()
		return false
	}
	s.tokenID = tokenID
	g.mu.Unlock()
	return g.stillGood(ctx, s)
}

// stillGood verifies the session's token against the database and ends the
// session when it is not good (or cannot be checked).
func (g *Gateway) stillGood(ctx context.Context, s *session) bool {
	g.mu.Lock()
	tokenID := s.tokenID
	g.mu.Unlock()
	if g.DB != nil {
		valid, err := g.validTokens(ctx, []uuid.UUID{tokenID})
		if err == nil && valid[tokenID] {
			return true
		}
	}
	g.mu.Lock()
	if g.sessions[s.id] == s {
		g.removeLocked(s.id, s)
	}
	g.mu.Unlock()
	s.closeConn(CloseAuthFailed, "Authentication failed")
	return false
}

func (g *Gateway) revokeToken(tokenID uuid.UUID) {
	g.mu.Lock()
	var victims []*session
	for id, s := range g.sessions {
		if s.tokenID == tokenID {
			victims = append(victims, s)
			g.removeLocked(id, s)
		}
	}
	g.mu.Unlock()
	for _, s := range victims {
		s.closeConn(CloseAuthFailed, "Authentication failed")
	}
}
