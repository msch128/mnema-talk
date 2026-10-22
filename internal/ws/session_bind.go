package ws

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// maxRevalidateFailures is how many revalidations in a row may fail on a
// database error before a browser connection is closed (fail closed): the
// session can no longer be proven alive.
const maxRevalidateFailures = 3

// SessionClaimer is implemented by the session authenticator (*auth.Sessions).
// It names the verified cookie's session id and expiry, so a connection can end
// when that one session is signed out or its token runs out.
type SessionClaimer interface {
	SessionClaims(r *http.Request) (id string, expires time.Time)
}

// bindSession ties a browser connection to the session it was opened with. An
// authenticator without claims (in-memory test doubles) binds nothing.
func (h *Hub) bindSession(c *Client, r *http.Request) {
	if sc, ok := h.Sessions.(SessionClaimer); ok {
		c.sessionID, c.sessionExp = sc.SessionClaims(r)
	}
}

// DisconnectSession closes every live connection that was opened with the
// session id jti (a single sign-out). Other sessions of the user stay.
func (h *Hub) DisconnectSession(jti string) {
	if jti == "" {
		return
	}
	h.mu.Lock()
	var clients []*Client
	for c := range h.clients {
		if c.sessionID == jti {
			c.shutdown()
			clients = append(clients, c)
		}
	}
	for c := range h.pending {
		if c.sessionID == jti {
			delete(h.pending, c)
			c.shutdown()
			clients = append(clients, c)
		}
	}
	h.mu.Unlock()
	for _, c := range clients {
		c.close()
	}
}

// sessionState reads what revalidation needs in one query: the user's token
// version and whether the session id was signed out.
func (h *Hub) sessionState(ctx context.Context, userID uuid.UUID, jti string) (tv int, revoked bool, err error) {
	err = h.DB.QueryRow(ctx, `SELECT token_version,
		($2 <> '' AND EXISTS (SELECT 1 FROM revoked_sessions WHERE jti = $2))
		FROM users WHERE id = $1`, userID, jti).Scan(&tv, &revoked)
	return tv, revoked, err
}
