//go:build integration

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func requestWithToken(s *Sessions, token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: s.CookieName(), Value: token})
	return r
}

func TestLogoutRevokesOnlyThatSession(t *testing.T) {
	p, admin, _ := authFixture(t)
	s := &Sessions{DB: p, Secret: strings.Repeat("k", 32), TTL: time.Hour}
	first, err := IssueToken(admin.ID, 0, s.Secret, s.TTL)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := IssueToken(admin.ID, 0, s.Secret, s.TTL)
	if _, err := s.Authenticate(requestWithToken(s, first)); err != nil {
		t.Fatalf("fresh session: %v", err)
	}

	if err := s.Revoke(requestWithToken(s, first)); err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(requestWithToken(s, first)); err != nil {
		t.Fatalf("revoking twice must be harmless: %v", err)
	}
	if _, err := s.Authenticate(requestWithToken(s, first)); err == nil {
		t.Fatal("a copied cookie still works after logout")
	}
	if _, err := s.Authenticate(requestWithToken(s, second)); err != nil {
		t.Fatalf("another session of the same user must stay valid: %v", err)
	}
}

func TestRevokeIgnoresUnusableTokensAndPrunesExpiredEntries(t *testing.T) {
	p, admin, _ := authFixture(t)
	ctx := context.Background()
	s := &Sessions{DB: p, Secret: strings.Repeat("k", 32), TTL: time.Hour}

	if err := s.Revoke(httptest.NewRequest(http.MethodGet, "/", nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(requestWithToken(s, "not-a-token")); err != nil {
		t.Fatal(err)
	}
	// A token from before sessions had an id stays valid until it expires and
	// cannot be revoked individually.
	legacy, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: admin.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: admin.ID.String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString([]byte(s.Secret))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(requestWithToken(s, legacy)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(requestWithToken(s, legacy)); err != nil {
		t.Fatalf("legacy session without id must stay valid: %v", err)
	}
	var n int
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM revoked_sessions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("nothing should be recorded yet: %d %v", n, err)
	}

	if _, err := p.Exec(ctx, `INSERT INTO revoked_sessions (jti, expires_at) VALUES ('old', NOW() - INTERVAL '1 hour')`); err != nil {
		t.Fatal(err)
	}
	tok, _ := IssueToken(admin.ID, 0, s.Secret, s.TTL)
	if err := s.Revoke(requestWithToken(s, tok)); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM revoked_sessions WHERE jti = 'old'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("expired revocation not pruned: %d %v", n, err)
	}
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM revoked_sessions`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("live revocation missing: %d %v", n, err)
	}
}

func TestBotAccountsCannotSignInOrHoldSessions(t *testing.T) {
	p, _, _ := authFixture(t)
	ctx := context.Background()
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	var bot uuid.UUID
	if err := p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role, is_bot)
		VALUES ('wh-test', 'Bot', $1, 'user', TRUE) RETURNING id`, hash).Scan(&bot); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Login(ctx, p, "wh-test", testPassword); err != ErrInvalidCredentials {
		t.Fatalf("bot login: %v", err)
	}
	if _, _, err := sessionUser(ctx, p, bot, ""); err == nil {
		t.Fatal("bot session loaded")
	}
	if err := CheckUserPassword(ctx, p, bot, testPassword); err != ErrWrongPassword {
		t.Fatalf("bot reauth: %v", err)
	}
}

func TestOwnerPasswordNeedsMoreCharactersThanMembers(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	const twelve = "twelve-chars"
	if _, err := ChangePassword(ctx, p, admin.ID, testPassword, twelve); err == nil {
		t.Fatal("owner accepted a 12 character password")
	}
	if _, err := ChangePassword(ctx, p, admin.ID, testPassword, "fourteen-chars"); err != nil {
		t.Fatalf("owner 14 characters: %v", err)
	}
	if _, err := ChangePassword(ctx, p, member.ID, testPassword, twelve); err != nil {
		t.Fatalf("member 12 characters: %v", err)
	}
}
