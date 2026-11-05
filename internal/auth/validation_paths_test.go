package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestTokenWithoutUserIDIsRejected(t *testing.T) {
	token, err := IssueToken(uuid.Nil, 0, secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseToken(token, secret); err == nil || err.Error() != "token has no user id" {
		t.Fatalf("missing identity = %v", err)
	}
}

type inviteEntropyFailure struct{}

func (inviteEntropyFailure) Read([]byte) (int, error) { return 0, errors.New("test entropy failure") }

func TestInviteEntropyFailureIsPropagated(t *testing.T) {
	// GenerateInviteCode uses rand.Int's injectable Reader, unlike rand.Read
	// (which terminates the process on entropy failure in modern Go).
	original := rand.Reader
	rand.Reader = inviteEntropyFailure{}
	t.Cleanup(func() { rand.Reader = original })
	if code, err := GenerateInviteCode(); code != "" || err == nil || err.Error() != "test entropy failure" {
		t.Fatalf("generate = %q, %v", code, err)
	}
	if inv, err := CreateInvite(context.Background(), nil, uuid.Nil, "", nil, nil); inv != nil || err == nil || err.Error() != "test entropy failure" {
		t.Fatalf("create = %+v, %v", inv, err)
	}
}

func TestServiceValidationRejectsBeforeDatabaseAccess(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ username, displayName, password, invite string }{
		{"ab", "", testPassword, "code123"},
		{"validuser", "", "short", "code123"},
		{"validuser", strings.Repeat("x", MaxDisplayNameLen+1), testPassword, "code123"},
		{"validuser", "", testPassword, ""},
		{"validuser", "", testPassword, strings.Repeat("x", maxInviteCodeChars+1)},
	} {
		if u, err := Register(ctx, nil, tc.username, tc.displayName, tc.password, tc.invite); u != nil || err == nil {
			t.Fatalf("invalid registration = %+v, %v", u, err)
		}
	}
	if _, err := ChangePassword(ctx, nil, uuid.Nil, testPassword, "short"); err == nil {
		t.Fatal("short new password accepted")
	}
	if _, err := UpdateProfile(ctx, nil, uuid.Nil, strings.Repeat("x", MaxDisplayNameLen+1), ""); err == nil {
		t.Fatal("long display name accepted")
	}
	if _, err := UpdateProfile(ctx, nil, uuid.Nil, "valid", strings.Repeat("x", MaxBioLen+1)); err == nil {
		t.Fatal("long bio accepted")
	}
	if _, err := SetPresence(ctx, nil, uuid.Nil, "offline"); err == nil {
		t.Fatal("offline presence accepted")
	}
	if _, err := SetStatusText(ctx, nil, uuid.Nil, strings.Repeat("x", MaxStatusTextLen+1), nil); err == nil {
		t.Fatal("long status accepted")
	}
}

func TestDefaultDisplayNameTruncatesLongUsername(t *testing.T) {
	username := strings.Repeat("a", 32)
	if got := defaultDisplayName(username); got != username[:MaxDisplayNameLen] {
		t.Fatalf("display name = %q", got)
	}
}
