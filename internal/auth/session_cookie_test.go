package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func assertSessionCookie(t *testing.T, c *http.Cookie, name string, secure bool) {
	t.Helper()
	if c.Name != name || c.Secure != secure {
		t.Fatalf("unexpected cookie identity: name=%q secure=%v", c.Name, c.Secure)
	}
	if !c.HttpOnly || c.Path != "/" {
		t.Fatalf("unexpected cookie scope: httpOnly=%v path=%q", c.HttpOnly, c.Path)
	}
	if c.Domain != "" || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 3600 {
		t.Fatalf("unexpected cookie attributes: domain=%q sameSite=%v maxAge=%d", c.Domain, c.SameSite, c.MaxAge)
	}
}

func TestSessionCookieAndContextContract(t *testing.T) {
	for _, secure := range []bool{false, true} {
		s := &Sessions{Secure: secure, Secret: secret, TTL: time.Hour}
		u := &User{ID: uuid.New(), Role: RoleAdmin}
		w := httptest.NewRecorder()
		if err := s.Start(w, u, 7); err != nil {
			t.Fatal(err)
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("cookies = %v", cookies)
		}
		c := cookies[0]
		assertSessionCookie(t, c, s.CookieName(), secure)
		claims, err := ParseToken(c.Value, secret)
		if err != nil {
			t.Fatalf("parse cookie token: %v", err)
		}
		if claims.UserID != u.ID || claims.TokenVersion != 7 {
			t.Fatalf("cookie claims mismatch: %+v", claims)
		}
		w = httptest.NewRecorder()
		s.End(w)
		if c := w.Result().Cookies()[0]; c.Value != "" || c.MaxAge != -1 {
			t.Fatalf("expired cookie = %+v", c)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if UserKey(r) != "" || UserFrom(r.Context()) != nil {
		t.Fatal("anonymous request has user")
	}
	u := &User{ID: uuid.New(), Role: RoleAdmin}
	r = r.WithContext(WithUser(r.Context(), u))
	if UserKey(r) != u.ID.String() || UserFrom(r.Context()) != u {
		t.Fatal("authenticated context lost user")
	}
}
