//go:build integration

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func nativeHTTPFixture(t *testing.T) (*db.Pool, *User, *NativeHandler) {
	t.Helper()
	pool, user, service, device := nativeFixture(t)
	_ = device
	accounts := NewHandler(&Sessions{DB: pool, Secret: strings.Repeat("s", 32), TTL: nativeMaxFamilyTTL, Secure: true}, nil)
	handler, err := NewNativeHandler(accounts, service)
	if err != nil {
		t.Fatal(err)
	}
	return pool, user, handler
}

// Test-only explicit wire response. Never include this value in failure output.
type nativeClientGrant struct {
	User         User      `json:"user"`
	Access       string    `json:"access_token"`
	Refresh      string    `json:"refresh_token"`
	AccessExpiry time.Time `json:"access_expires_at"`
	FamilyExpiry time.Time `json:"family_expires_at"`
}

func nativeLoginBody(username, password string, instance uuid.UUID) string {
	body, err := json.Marshal(map[string]string{"username": username, "password": password, "client_instance_id": instance.String()})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func nativeRefreshBody(token string) string {
	body, err := json.Marshal(map[string]string{"refresh_token": token})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func nativeHTTPLogin(t *testing.T, h *NativeHandler, user *User) nativeClientGrant {
	t.Helper()
	w := nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", nativeLoginBody(user.Username, testPassword, uuid.New())), http.StatusOK)
	var grant nativeClientGrant
	if err := json.Unmarshal(w.Body.Bytes(), &grant); err != nil {
		t.Fatalf("unmarshal grant: %v", err)
	}
	if grant.User.ID != user.ID {
		t.Fatalf("grant user mismatch: got %v, want %v", grant.User.ID, user.ID)
	}
	if len(grant.Access) != 43 || len(grant.Refresh) != 43 || grant.Access == grant.Refresh {
		t.Fatal("invalid token lengths or identical tokens")
	}
	if !grant.AccessExpiry.Before(grant.FamilyExpiry) {
		t.Fatal("access expiry must precede family expiry")
	}
	return grant
}

func nativeAuthedRequest(method, path, body, token string) *http.Request {
	r := nativeRequest(method, path, body)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestNativeHTTPLoginRefreshReuseAndContext(t *testing.T) {
	pool, user, h := nativeHTTPFixture(t)
	grant := nativeHTTPLogin(t, h, user)
	nativeCounts(t, pool, 1, 1, 1)
	// Native logout has no effect on a browser session or another device family.
	browser := httptest.NewRecorder()
	if err := h.accounts.Sessions.Start(browser, user, 0); err != nil {
		t.Fatal(err)
	}
	other := nativeHTTPLogin(t, h, user)
	w := nativeStatus(t, h.Handler(), nativeAuthedRequest(http.MethodGet, "/auth/me", "", grant.Access), http.StatusOK)
	var got User
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal me: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("me user ID mismatch: got %v, want %v", got.ID, user.ID)
	}
	if bytes.Contains(w.Body.Bytes(), []byte(grant.Access)) || bytes.Contains(w.Body.Bytes(), []byte(grant.Refresh)) {
		t.Fatal("me leaked grant tokens")
	}
	called := false
	h.RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := NativePrincipalFrom(r.Context())
		if !ok || UserFrom(r.Context()) == nil {
			t.Fatal("native context missing")
		}
		if principal.User().ID != user.ID || UserFrom(r.Context()).ID != user.ID {
			t.Fatal("native context user mismatch")
		}
		called = true
	})).ServeHTTP(httptest.NewRecorder(), nativeAuthedRequest(http.MethodGet, "/resource", "", grant.Access))
	if !called {
		t.Fatal("native context middleware did not call handler")
	}
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/refresh", nativeRefreshBody(grant.Refresh)), http.StatusTooManyRequests)
	if _, err := pool.Exec(context.Background(), `UPDATE native_session_families SET refresh_after=created_at`); err != nil {
		t.Fatal(err)
	}
	w = nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/refresh", nativeRefreshBody(grant.Refresh)), http.StatusOK)
	var next nativeClientGrant
	if err := json.Unmarshal(w.Body.Bytes(), &next); err != nil {
		t.Fatalf("unmarshal next grant: %v", err)
	}
	if next.Access == grant.Access || next.Refresh == grant.Refresh {
		t.Fatal("rotation did not produce fresh tokens")
	}
	if next.FamilyExpiry != grant.FamilyExpiry {
		t.Fatal("rotation changed family expiry")
	}
	// Reuse must revoke the family even though the new refresh is throttled.
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/refresh", nativeRefreshBody(grant.Refresh)), http.StatusUnauthorized)
	nativeStatus(t, h.Handler(), nativeAuthedRequest(http.MethodGet, "/auth/me", "", next.Access), http.StatusUnauthorized)
	nativeStatus(t, h.Handler(), nativeAuthedRequest(http.MethodPost, "/auth/logout", "", other.Access), http.StatusNoContent)
	nativeStatus(t, h.Handler(), nativeAuthedRequest(http.MethodGet, "/auth/me", "", other.Access), http.StatusUnauthorized)
	browserRequest := nativeRequest(http.MethodGet, "/api/auth/me", "")
	browserRequest.AddCookie(browser.Result().Cookies()[0])
	gotUser, err := h.accounts.Sessions.Authenticate(browserRequest)
	if err != nil {
		t.Fatalf("authenticate browser request: %v", err)
	}
	if gotUser.ID != user.ID {
		t.Fatal("native family logout invalidated browser cookie")
	}
}

func TestNativeHTTPAccountInvalidationAndFreshRole(t *testing.T) {
	for _, change := range []string{"disabled", "version", "password", "deleted"} {
		t.Run(change, func(t *testing.T) {
			pool, user, h := nativeHTTPFixture(t)
			grant := nativeHTTPLogin(t, h, user)
			if _, err := pool.Exec(context.Background(), `UPDATE users SET role='admin' WHERE id=$1`, user.ID); err != nil {
				t.Fatal(err)
			}
			w := nativeStatus(t, h.Handler(), nativeAuthedRequest(http.MethodGet, "/auth/me", "", grant.Access), http.StatusOK)
			var got User
			if json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Role != RoleAdmin {
				t.Fatal("native access cached stale role")
			}
			// Restore member role so existing disable policy applies.
			if _, err := pool.Exec(context.Background(), `UPDATE users SET role='user' WHERE id=$1`, user.ID); err != nil {
				t.Fatal(err)
			}
			var err error
			switch change {
			case "disabled":
				err = SetDisabled(context.Background(), pool, user.ID, true)
			case "version":
				err = RevokeSessions(context.Background(), pool, user.ID)
			case "password":
				_, err = ChangePassword(context.Background(), pool, user.ID, testPassword, "another-test-password")
			case "deleted":
				_, err = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			nativeStatus(t, h.Handler(), nativeAuthedRequest(http.MethodGet, "/auth/me", "", grant.Access), http.StatusUnauthorized)
			nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/refresh", nativeRefreshBody(grant.Refresh)), http.StatusUnauthorized)
			if change == "disabled" {
				nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", nativeLoginBody(user.Username, testPassword, uuid.New())), http.StatusForbidden)
			}
		})
	}
}

func TestNativeHTTPSharedLockoutsAndIndependentRefreshBudget(t *testing.T) {
	_, user, h := nativeHTTPFixture(t)
	for i := 0; i < 10; i++ {
		nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", nativeLoginBody(user.Username, "wrong-test-password", uuid.New())), http.StatusUnauthorized)
	}
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", nativeLoginBody(user.Username, testPassword, uuid.New())), http.StatusTooManyRequests)
	if code := postLogin(h.accounts, "192.0.2.20", user.Username, testPassword); code != http.StatusTooManyRequests {
		t.Fatalf("browser escaped native failures: %d", code)
	}
	_, user, h = nativeHTTPFixture(t)
	for i := 0; i < 10; i++ {
		if code := postLogin(h.accounts, "192.0.2.20", user.Username, "wrong-test-password"); code != http.StatusUnauthorized {
			t.Fatal("unexpected browser lockout")
		}
	}
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", nativeLoginBody(user.Username, testPassword, uuid.New())), http.StatusTooManyRequests)
	_, user, h = nativeHTTPFixture(t)
	h.refreshPerIP = httpx.NewRateLimiter(1, time.Hour)
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/refresh", nativeRefreshBody("invalid")), http.StatusUnauthorized)
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/refresh", nativeRefreshBody("invalid")), http.StatusTooManyRequests)
	_ = nativeHTTPLogin(t, h, user)
}

func TestNativeHTTPNoCookieTokenConfusionOrNonemptyAuthedBody(t *testing.T) {
	_, user, h := nativeHTTPFixture(t)
	grant := nativeHTTPLogin(t, h, user)
	for _, route := range []struct{ method, path string }{{http.MethodGet, "/auth/me"}, {http.MethodPost, "/auth/logout"}} {
		for _, body := range []string{" ", "{}"} {
			nativeStatus(t, h.Handler(), nativeAuthedRequest(route.method, route.path, body, grant.Access), http.StatusBadRequest)
		}
	}
	nativeStatus(t, h.Handler(), nativeAuthedRequest(http.MethodGet, "/auth/me", "", grant.Refresh), http.StatusUnauthorized)
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/refresh", nativeRefreshBody(grant.Access)), http.StatusUnauthorized)
	jwt, err := IssueToken(user.ID, 0, h.accounts.Sessions.Secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	nativeStatus(t, h.Handler(), nativeAuthedRequest(http.MethodGet, "/auth/me", "", jwt), http.StatusUnauthorized)
	r := nativeRequest(http.MethodGet, "/auth/me", "")
	r.AddCookie(&http.Cookie{Name: h.accounts.Sessions.CookieName(), Value: jwt})
	nativeStatus(t, h.Handler(), r, http.StatusForbidden)
	r = nativeAuthedRequest(http.MethodGet, "/api/auth/me", "", grant.Access)
	if _, err := h.accounts.Sessions.Authenticate(r); err == nil {
		t.Fatal("browser accepted native bearer")
	}
}

func TestNativeHTTPCapAndFaultsExposeNoSecrets(t *testing.T) {
	_, user, h := nativeHTTPFixture(t)
	for i := 0; i < nativeMaxFamilies; i++ {
		_ = nativeHTTPLogin(t, h, user)
	}
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", nativeLoginBody(user.Username, testPassword, uuid.New())), http.StatusConflict)
	_, user, h = nativeHTTPFixture(t)
	const detail = "synthetic-native-credential-private-detail"
	h.accounts.verify = func(context.Context, string, string) (*User, int, error) { return nil, 0, errors.New(detail) }
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	w := nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", nativeLoginBody(user.Username, testPassword, uuid.New())), http.StatusInternalServerError)
	if strings.Contains(log.String(), detail) || strings.Contains(w.Body.String(), detail) {
		t.Fatal("native credential error detail exposed")
	}
	h.accounts.verify = nil
	h.sessions.begin = func(context.Context) (pgx.Tx, error) { return nil, io.ErrUnexpectedEOF }
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", nativeLoginBody(user.Username, testPassword, uuid.New())), http.StatusInternalServerError)
	_, user, h = nativeHTTPFixture(t)
	grant := nativeHTTPLogin(t, h, user)
	h.sessions.begin = func(context.Context) (pgx.Tx, error) { return nil, io.ErrUnexpectedEOF }
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/refresh", nativeRefreshBody(grant.Refresh)), http.StatusInternalServerError)
	nativeStatus(t, h.Handler(), nativeAuthedRequest(http.MethodPost, "/auth/logout", "", grant.Access), http.StatusInternalServerError)
}
