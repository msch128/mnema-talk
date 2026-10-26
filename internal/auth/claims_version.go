package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// AuthenticateRequest is Authenticate plus the token version of the verified
// session cookie. It equals the user's token_version at the time of the check;
// long-lived connections (WebSockets) keep it to notice later revocations
// (password change, "log out everywhere", disabled account).
func (s *Sessions) AuthenticateRequest(r *http.Request) (*User, int, error) {
	c, err := r.Cookie(s.CookieName())
	if err != nil || c.Value == "" {
		return nil, 0, errNoSession
	}
	claims, err := ParseToken(c.Value, s.Secret)
	if err != nil {
		return nil, 0, errNoSession
	}
	u, tv, err := sessionUser(r.Context(), s.DB, claims.UserID, claims.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, errNoSession
	}
	if err != nil {
		return nil, 0, err
	}
	if tv != claims.TokenVersion {
		return nil, 0, errNoSession
	}
	s.touch(r, claims)
	return u, claims.TokenVersion, nil
}

// SessionClaims returns the session id and expiry of the request's verified
// cookie, for long-lived connections that must end with the session: when it
// is signed out (its id lands in revoked_sessions) or when the token expires.
// A request without a valid cookie yields an empty id and a zero time. Tokens
// issued before session ids existed have an empty id.
func (s *Sessions) SessionClaims(r *http.Request) (string, time.Time) {
	c, err := r.Cookie(s.CookieName())
	if err != nil || c.Value == "" {
		return "", time.Time{}
	}
	claims, err := ParseToken(c.Value, s.Secret)
	if err != nil || claims.ExpiresAt == nil {
		return "", time.Time{}
	}
	return claims.ID, claims.ExpiresAt.Time
}
