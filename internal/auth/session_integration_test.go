//go:build integration

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuthSessionFailurePaths(t *testing.T) {
	p, _, member := authFixture(t)
	s := &Sessions{DB: p, Secret: secret, TTL: time.Hour}
	for _, value := range []string{"", "not-a-token"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: s.CookieName(), Value: value})
		if _, _, err := s.AuthenticateRequest(r); err != ErrNoSession {
			t.Fatalf("bad cookie=%q err=%v", value, err)
		}
	}
	token, err := IssueToken(member.ID, 0, secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: s.CookieName(), Value: token})
	fault := authHookPool(t, p, &authQueryHook{match: "WHERE id = $1 AND NOT (disabled_at IS NOT NULL", cancel: true})
	s.DB = fault
	if _, _, err := s.AuthenticateRequest(r); !errors.Is(err, context.Canceled) {
		t.Fatalf("auth db error = %v", err)
	}
	s.DB = p
	// Use a second traced pool: the first hook intentionally fires only once.
	s.DB = authHookPool(t, p, &authQueryHook{match: "WHERE id = $1 AND NOT (disabled_at IS NOT NULL", cancel: true})
	w := httptest.NewRecorder()
	s.RequireUser(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("failed authentication reached handler") })).ServeHTTP(w, r)
	if w.Code != 500 {
		t.Fatalf("middleware db error status=%d", w.Code)
	}
	if err := CheckUserPassword(context.Background(), p, uuid.New(), testPassword); err != ErrWrongPassword {
		t.Fatalf("absent password check = %v", err)
	}
	if err := SetDisabled(context.Background(), p, member.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := CheckUserPassword(context.Background(), p, member.ID, testPassword); err != ErrWrongPassword {
		t.Fatalf("disabled password check = %v", err)
	}
}
