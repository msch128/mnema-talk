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

func TestAccountLockoutNeverBlocksTheCorrectPassword(t *testing.T) {
	h := newLoginHandler()
	// 100 wrong guesses spread over 20 addresses: below every per-address
	// limit, but enough to trip the account-wide lockout.
	for i := 0; i < 100; i++ {
		postLogin(h, fmt.Sprintf("198.51.100.%d", i%20), "Herzog", "wrong-guess")
	}
	if locked, _ := h.accountFailures.IsLockedOut("herzog"); !locked {
		t.Fatal("account lockout did not trigger")
	}

	if code := postLogin(h, "203.0.113.9", "herzog", "wrong-guess"); code != http.StatusTooManyRequests {
		t.Fatalf("wrong password during account lockout: got %d, want 429", code)
	}
	if code := postLogin(h, "192.0.2.1", "Herzog", testPassword); code != http.StatusOK {
		t.Fatalf("correct password during account lockout: got %d, want 200", code)
	}
	if locked, _ := h.accountFailures.IsLockedOut("herzog"); locked {
		t.Fatal("successful login did not reset the account lockout")
	}
	if code := postLogin(h, "203.0.113.10", "Herzog", "wrong-guess"); code != http.StatusUnauthorized {
		t.Fatalf("after reset a wrong password is a plain 401, got %d", code)
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
