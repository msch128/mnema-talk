package auth

import (
	"errors"
	"net/http"

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
	u, tv, err := sessionUser(r.Context(), s.DB, claims.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, errNoSession
	}
	if err != nil {
		return nil, 0, err
	}
	if tv != claims.TokenVersion {
		return nil, 0, errNoSession
	}
	return u, claims.TokenVersion, nil
}
