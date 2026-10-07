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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

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
		if c.Name != s.CookieName() || c.Secure != secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 3600 {
			t.Fatalf("cookie attributes = %+v", c)
		}
		claims, err := ParseToken(c.Value, secret)
		if err != nil || claims.UserID != u.ID || claims.TokenVersion != 7 {
			t.Fatalf("cookie claims = %+v, %v", claims, err)
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

func TestAuthHandlersRejectMalformedJSON(t *testing.T) {
	h := NewHandler(&Sessions{Secret: secret}, nil)
	u := &User{ID: uuid.New()}
	for name, handler := range map[string]httpx.HandlerFunc{
		"login": h.login, "register": h.register, "password": h.changePassword,
		"locale": h.setLocale, "profile": h.updateProfile, "presence": h.setPresence,
		"status": h.setStatus, "invite": h.createInvite, "admin password": h.setUserPassword,
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{"))
			r = r.WithContext(WithUser(r.Context(), u))
			rc := chi.NewRouteContext()
			rc.URLParams.Add("id", uuid.NewString())
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
			w := httptest.NewRecorder()
			httpx.Handle(handler).ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAdminHandlersRejectMalformedIDs(t *testing.T) {
	h := NewHandler(&Sessions{}, nil)
	for name, handler := range map[string]httpx.HandlerFunc{
		"get": h.getUser, "delete invite": h.deleteInvite, "disable": h.disableUser,
		"enable": h.enableUser, "revoke": h.revokeUserSessions, "reset": h.resetUserPassword,
		"password": h.setUserPassword, "kick": h.kickUser, "status": h.setUserStatus,
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			rc := chi.NewRouteContext()
			rc.URLParams.Add("id", "not-a-uuid")
			rc.URLParams.Add("userID", "not-a-uuid")
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
			w := httptest.NewRecorder()
			httpx.Handle(handler).ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d", w.Code)
			}
		})
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
	if _, err := SetStatusText(ctx, nil, uuid.Nil, strings.Repeat("x", MaxStatusTextLen+1)); err == nil {
		t.Fatal("long status accepted")
	}
}

func TestDefaultDisplayNameTruncatesLongUsername(t *testing.T) {
	username := strings.Repeat("a", 32)
	if got := defaultDisplayName(username); got != username[:MaxDisplayNameLen] {
		t.Fatalf("display name = %q", got)
	}
}
