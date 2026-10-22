package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSessionClaimsNameTheCookiesSessionAndExpiry(t *testing.T) {
	s := &Sessions{Secret: strings.Repeat("c", 32), TTL: time.Hour}
	token, err := IssueToken(uuid.New(), 3, s.Secret, s.TTL)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ParseToken(token, s.Secret)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: s.CookieName(), Value: token})
	id, exp := s.SessionClaims(req)
	if id == "" || id != claims.ID || !exp.Equal(claims.ExpiresAt.Time) || time.Until(exp) <= 0 {
		t.Fatalf("claims = %q %v, want %q %v", id, exp, claims.ID, claims.ExpiresAt.Time)
	}
}

func TestSessionClaimsAreEmptyWithoutAValidCookie(t *testing.T) {
	s := &Sessions{Secret: strings.Repeat("c", 32), TTL: time.Hour}
	other, err := IssueToken(uuid.New(), 0, strings.Repeat("o", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for name, cookie := range map[string]*http.Cookie{
		"none":         nil,
		"empty":        {Name: s.CookieName(), Value: ""},
		"garbage":      {Name: s.CookieName(), Value: "not-a-token"},
		"wrong secret": {Name: s.CookieName(), Value: other},
		"wrong cookie": {Name: "other", Value: other},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if id, exp := s.SessionClaims(req); id != "" || !exp.IsZero() {
			t.Errorf("%s: claims = %q %v, want none", name, id, exp)
		}
	}
}
