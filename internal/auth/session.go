package auth

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

type contextKey int

const userKey contextKey = iota

var errNoSession = httpx.ErrUnauthorized("authentication required")

// Sessions issues and validates the HttpOnly session cookie. The token never
// touches JavaScript, so an XSS bug cannot exfiltrate it.
type Sessions struct {
	DB     *db.Pool
	Secret string
	TTL    time.Duration
	Secure bool

	// touched throttles the activity writes (see touch); its zero value works.
	touched sync.Map
}

// CookieName uses the __Host- prefix on HTTPS, which forces Secure, Path=/ and
// no Domain attribute, so a sibling subdomain cannot overwrite the session.
func (s *Sessions) CookieName() string {
	if s.Secure {
		return "__Host-mnema_session"
	}
	return "mnema_session"
}

// Start issues a session for the user and sets the cookie. The session list
// learns of it with the session's first authenticated request (see touch).
func (s *Sessions) Start(w http.ResponseWriter, u *User, tokenVersion int) error {
	token, err := IssueToken(u.ID, tokenVersion, s.Secret, s.TTL)
	if err != nil {
		return err
	}
	http.SetCookie(w, s.cookie(token, int(s.TTL.Seconds())))
	return nil
}

// Revoke records the request's session id as signed out until its token would
// have expired, and prunes entries that already have. A missing, invalid,
// expired or legacy (no jti) token is ignored.
func (s *Sessions) Revoke(r *http.Request) error {
	c, err := r.Cookie(s.CookieName())
	if err != nil || c.Value == "" {
		return nil
	}
	claims, err := ParseToken(c.Value, s.Secret)
	if err != nil || claims.ID == "" || claims.ExpiresAt == nil {
		return nil
	}
	if _, err := s.DB.Exec(r.Context(), `DELETE FROM revoked_sessions WHERE expires_at < NOW()`); err != nil {
		return err
	}
	if _, err = s.DB.Exec(r.Context(), `INSERT INTO revoked_sessions (jti, expires_at) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		claims.ID, claims.ExpiresAt.Time); err != nil {
		return err
	}
	_, err = s.DB.Exec(r.Context(), `DELETE FROM user_sessions WHERE jti = $1`, claims.ID)
	return err
}

// End clears the session cookie.
func (s *Sessions) End(w http.ResponseWriter) {
	http.SetCookie(w, s.cookie("", -1))
}

func (s *Sessions) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     s.CookieName(),
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

// Authenticate resolves the request's session to the current user. Expired,
// revoked (token_version mismatch) or deleted sessions fail with 401.
func (s *Sessions) Authenticate(r *http.Request) (*User, error) {
	u, _, err := s.AuthenticateRequest(r)
	return u, err
}

// RequireUser rejects unauthenticated requests and stores the user in the context.
func (s *Sessions) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.Authenticate(r)
		if err != nil {
			if _, ok := httpx.AsAPIError(err); !ok {
				err = httpx.ErrServer(err)
			}
			httpx.WriteError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
	})
}

// UserKey returns the authenticated user's ID as a rate-limit key.
func UserKey(r *http.Request) string {
	if u := UserFrom(r.Context()); u != nil {
		return u.ID.String()
	}
	return ""
}

func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// UserFrom returns the authenticated user, or nil outside RequireUser.
func UserFrom(ctx context.Context) *User {
	u, _ := ctx.Value(userKey).(*User)
	return u
}
