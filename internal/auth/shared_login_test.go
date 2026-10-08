package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func sharedLoginRequest(path, address string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, nil)
	r.RemoteAddr = address
	return r
}

func TestSharedLoginPreservesVerifiedVersionAndUser(t *testing.T) {
	h := newLoginHandler()
	want := &User{ID: uuid.New(), Username: "Herzog", Role: RoleAdmin}
	h.verify = func(_ context.Context, username, password string) (*User, int, error) {
		if username != "Herzog" || password != testPassword {
			t.Fatal("credential check received changed credentials")
		}
		return want, 37, nil
	}
	u, version, retry, err := h.verifyLogin(sharedLoginRequest("/api/native/v1/auth/login", "192.0.2.1:1234"),
		LoginRequest{Username: " Herzog ", Password: testPassword})
	if err != nil || retry != 0 || version != 37 || u != want {
		t.Fatalf("verified identity/version was not preserved: version=%d retry=%v err=%v", version, retry, err)
	}
}

func TestSharedLoginCannotEscapeBrowserAddressLockout(t *testing.T) {
	h := newLoginHandler()
	for i := 0; i < 10; i++ {
		if code := postLogin(h, "192.0.2.7", "Herzog", "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("wrong browser password status=%d", code)
		}
	}
	called := false
	h.verify = func(context.Context, string, string) (*User, int, error) {
		called = true
		return nil, 0, errors.New("must not verify locked address")
	}
	u, version, retry, err := h.verifyLogin(sharedLoginRequest("/api/native/v1/auth/login", "192.0.2.7:5678"),
		LoginRequest{Username: "herzog", Password: testPassword})
	if called || u != nil || version != 0 || retry <= 0 || err != nil {
		t.Fatal("alternate transport escaped shared address/account lockout")
	}
}

func TestSharedLoginFailuresAlsoLockBrowser(t *testing.T) {
	h := newLoginHandler()
	for i := 0; i < 10; i++ {
		_, _, retry, err := h.verifyLogin(sharedLoginRequest("/api/native/v1/auth/login", "192.0.2.9:5678"),
			LoginRequest{Username: "Herzog", Password: "wrong"})
		if !errors.Is(err, ErrInvalidCredentials) || retry != 0 {
			t.Fatalf("unexpected failed verification: retry=%v err=%v", retry, err)
		}
	}
	if code := postLogin(h, "192.0.2.9", "Herzog", testPassword); code != http.StatusTooManyRequests {
		t.Fatalf("browser escaped shared lockout: %d", code)
	}
}

func TestSharedLoginVerificationFailureDoesNotClearFailures(t *testing.T) {
	h := newLoginHandler()
	key := "192.0.2.11|herzog"
	h.loginFailures.RecordFailures(key, 9)
	failure := errors.New("credential store unavailable")
	h.verify = func(context.Context, string, string) (*User, int, error) {
		return nil, 0, failure
	}
	u, version, retry, err := h.verifyLogin(sharedLoginRequest("/api/native/v1/auth/login", "192.0.2.11:5678"),
		LoginRequest{Username: "Herzog", Password: testPassword})
	if u != nil || version != 0 || retry != 0 || !errors.Is(err, failure) {
		t.Fatal("credential failure incorrectly produced a login")
	}
	h.loginFailures.RecordFailure(key)
	if locked, _ := h.loginFailures.IsLockedOut(key); !locked {
		t.Fatal("credential store failure erased previous failed attempts")
	}
}
