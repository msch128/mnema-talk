package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const testPassword = "correct-horse-battery"

// newLoginHandler returns a handler whose credential check accepts only
// "Herzog" with testPassword, without a database.
func newLoginHandler() *Handler {
	h := NewHandler(&Sessions{Secret: strings.Repeat("s", 32), TTL: time.Hour}, nil)
	h.verify = func(_ context.Context, username, password string) (*User, int, error) {
		if strings.EqualFold(username, "Herzog") && password == testPassword {
			return &User{ID: uuid.New(), Username: "Herzog", Role: RoleAdmin}, 0, nil
		}
		return nil, 0, ErrInvalidCredentials
	}
	return h
}

func postLogin(h *Handler, ip, username, password string) int {
	body, _ := json.Marshal(LoginRequest{Username: username, Password: password})
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(string(body)))
	r.RemoteAddr = ip + ":1234"
	rec := httptest.NewRecorder()
	httpx.Handle(h.login).ServeHTTP(rec, r)
	return rec.Code
}

func TestLoginRejectsImplausibleUsernamesWithoutTracking(t *testing.T) {
	h := newLoginHandler()
	for _, name := range []string{strings.Repeat("a", 1<<20), strings.Repeat("b", 33), "has space", "ü-umlaut", ""} {
		if code := postLogin(h, "203.0.113.5", name, "whatever-password"); code != http.StatusUnauthorized {
			t.Fatalf("username of %d bytes: got %d, want 401", len(name), code)
		}
	}
	if n := h.loginFailures.Len() + h.accountFailures.Len(); n != 0 {
		t.Fatalf("implausible usernames became limiter keys: %d entries", n)
	}
}

// postLoginWith signs in with the given cookies and returns the response.
func postLoginWith(h *Handler, ip, username, password string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	body, _ := json.Marshal(LoginRequest{Username: username, Password: password})
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(string(body)))
	r.RemoteAddr = ip + ":1234"
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	httpx.Handle(h.login).ServeHTTP(rec, r)
	return rec
}

func tripAccountCap(t *testing.T, h *Handler) {
	t.Helper()
	// 100 wrong guesses spread over 20 addresses: below every per-address
	// limit, but enough to trip the account-wide cap.
	for i := 0; i < 100; i++ {
		postLogin(h, fmt.Sprintf("198.51.100.%d", i%20), "Herzog", "wrong-guess")
	}
	if locked, _ := h.accountFailures.IsLockedOut("herzog"); !locked {
		t.Fatal("account lockout did not trigger")
	}
}

func TestAccountCapOnlyEvaluatesPasswordsOfKnownDevices(t *testing.T) {
	h := newLoginHandler()
	// The owner signed in from a browser (cookie) before the attack.
	first := postLoginWith(h, "192.0.2.50", "Herzog", testPassword)
	if first.Code != http.StatusOK {
		t.Fatalf("first login: %d", first.Code)
	}
	var device *http.Cookie
	for _, c := range first.Result().Cookies() {
		if strings.HasPrefix(c.Name, "mnema_dev_") {
			device = c
		}
	}
	if device == nil || !device.HttpOnly || device.Path != "/api/auth" {
		t.Fatalf("known-device cookie missing or weak: %+v", device)
	}

	tripAccountCap(t, h)

	// A stranger with the right password is not even evaluated.
	verified := false
	inner := h.verify
	h.verify = func(ctx context.Context, u, p string) (*User, int, error) { verified = true; return inner(ctx, u, p) }
	if code := postLoginWith(h, "203.0.113.9", "Herzog", testPassword).Code; code != http.StatusTooManyRequests {
		t.Fatalf("unknown device during account cap: got %d, want 429", code)
	}
	if verified {
		t.Fatal("password was evaluated for an unknown device")
	}
	// A forged or foreign cookie does not make a device known.
	forged := &http.Cookie{Name: device.Name, Value: "9999999999.AAAA"}
	if code := postLoginWith(h, "203.0.113.9", "Herzog", testPassword, forged).Code; code != http.StatusTooManyRequests {
		t.Fatalf("forged cookie: got %d, want 429", code)
	}
	// The cookie of the same account is honoured from a new address ...
	if code := postLoginWith(h, "192.0.2.77", "Herzog", testPassword, device).Code; code != http.StatusOK {
		t.Fatalf("known browser during account cap: got %d, want 200", code)
	}
	if locked, _ := h.accountFailures.IsLockedOut("herzog"); locked {
		t.Fatal("successful login did not reset the account cap")
	}
	// ... and so is a recently seen address (native clients have no cookies).
	tripAccountCap(t, h)
	if code := postLoginWith(h, "192.0.2.77", "Herzog", testPassword).Code; code != http.StatusOK {
		t.Fatalf("recently seen address during account cap: got %d, want 200", code)
	}
}

func TestKnownDeviceCookieExpiresAndIsPerAccount(t *testing.T) {
	s := &Sessions{Secret: strings.Repeat("s", 32)}
	w := httptest.NewRecorder()
	s.markDevice(w, "herzog")
	c := w.Result().Cookies()[0]
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	if !s.hasDeviceCookie(r, "herzog") || s.hasDeviceCookie(r, "other") {
		t.Fatal("cookie must be valid for its own account only")
	}
	expired := &http.Cookie{Name: c.Name, Value: "1." + s.deviceMAC("herzog", 1)}
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(expired)
	if s.hasDeviceCookie(r, "herzog") {
		t.Fatal("expired cookie accepted")
	}
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: c.Name, Value: "nodot"})
	if s.hasDeviceCookie(r, "herzog") {
		t.Fatal("malformed cookie accepted")
	}
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: c.Name, Value: "x.y"})
	if s.hasDeviceCookie(r, "herzog") {
		t.Fatal("non-numeric expiry accepted")
	}
}

func TestKnownAddrsAreBounded(t *testing.T) {
	k := &knownAddrs{}
	for i := 0; i < knownIPMax+5; i++ {
		k.add("a", fmt.Sprint(i))
	}
	if len(k.m) > knownIPMax {
		t.Fatalf("known addresses grew to %d", len(k.m))
	}
	for key := range k.m {
		k.m[key] = time.Now().Add(-2 * knownIPTTL)
	}
	k.add("a", "fresh")
	if len(k.m) != 1 || !k.has("a", "fresh") || k.has("a", "0") {
		t.Fatalf("expired entries not pruned: %d", len(k.m))
	}
	var nilK *knownAddrs
	nilK.add("a", "b")
	if nilK.has("a", "b") {
		t.Fatal("nil set knows nothing")
	}
}

func TestAccountCapStillCountsWrongGuessesOfKnownDevices(t *testing.T) {
	h := newLoginHandler()
	if postLoginWith(h, "192.0.2.50", "Herzog", testPassword).Code != http.StatusOK {
		t.Fatal("login")
	}
	tripAccountCap(t, h)
	if code := postLogin(h, "192.0.2.50", "herzog", "wrong-guess"); code != http.StatusTooManyRequests {
		t.Fatalf("wrong password from a known device during the cap: got %d, want 429", code)
	}
}

func TestAccountUnderAttackTightensPerAddressLockout(t *testing.T) {
	h := newLoginHandler()
	for i := 0; i < 100; i++ {
		postLogin(h, fmt.Sprintf("198.51.100.%d", i%20), "Herzog", "wrong-guess")
	}
	// A fresh address gets far fewer than 10 guesses while the account is under attack.
	for i := 0; i < 4; i++ {
		postLogin(h, "203.0.113.50", "Herzog", "wrong-guess")
	}
	if code := postLogin(h, "203.0.113.50", "Herzog", testPassword); code != http.StatusTooManyRequests {
		t.Fatalf("attacking address not locked after 4 guesses under attack: %d", code)
	}
}

func TestPerAddressLockoutAppliesAndResets(t *testing.T) {
	h := newLoginHandler()
	for i := 0; i < 10; i++ {
		postLogin(h, "203.0.113.5", "Herzog", "wrong-guess")
	}
	if code := postLogin(h, "203.0.113.5", "Herzog", testPassword); code != http.StatusTooManyRequests {
		t.Fatalf("attacker address not locked: %d", code)
	}
	if code := postLogin(h, "198.51.100.7", "Herzog", testPassword); code != http.StatusOK {
		t.Fatalf("owner locked out from another address: %d", code)
	}

	// Success clears the address's failures.
	for i := 0; i < 9; i++ {
		postLogin(h, "192.0.2.8", "Herzog", "wrong-guess")
	}
	if code := postLogin(h, "192.0.2.8", "Herzog", testPassword); code != http.StatusOK {
		t.Fatalf("login before the threshold: %d", code)
	}
	if code := postLogin(h, "192.0.2.8", "Herzog", "wrong-guess"); code != http.StatusUnauthorized {
		t.Fatalf("failures not reset by success: %d", code)
	}
}

func TestPerAddressLockoutGroupsIPv6Slash64(t *testing.T) {
	h := newLoginHandler()
	for i := 0; i < 10; i++ {
		postLogin(h, fmt.Sprintf("[2001:db8:1:2::%x]", i+1), "Herzog", "wrong-guess")
	}
	if code := postLogin(h, "[2001:db8:1:2:ffff::1]", "Herzog", testPassword); code != http.StatusTooManyRequests {
		t.Fatalf("rotating within one /64 escaped the lockout: %d", code)
	}
	if code := postLogin(h, "[2001:db8:1:3::1]", "Herzog", testPassword); code != http.StatusOK {
		t.Fatalf("another /64 must not be locked: %d", code)
	}
}
