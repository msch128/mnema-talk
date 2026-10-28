package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// Wrong recovery-shaped codes on the signed-in proofs count against the
// recovery limiter, like at sign-in, and never lock the authenticator step.
func TestSignedInProofCountsFailuresLikeSignIn(t *testing.T) {
	h := NewHandler(nil, nil)
	u := &User{ID: uuid.New()}
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "192.0.2.50:1234"
	const recovery, authenticator = "abcd-efgh-jkmn", "123456"

	for i := 0; i < 30; i++ {
		if err := h.failedCode(r, u, recovery); err != errBadCode {
			t.Fatalf("failedCode = %v", err)
		}
	}
	if h.lockedOut(httptest.NewRecorder(), r, u.ID, authenticator) {
		t.Fatal("recovery guesses locked the authenticator step")
	}
	w := httptest.NewRecorder()
	if !h.lockedOut(w, r, u.ID, recovery) || w.Code != http.StatusTooManyRequests {
		t.Fatalf("recovery lockout missing: %d", w.Code)
	}

	for i := 0; i < 30; i++ {
		_ = h.failedCode(r, u, authenticator)
	}
	if !h.lockedOut(httptest.NewRecorder(), r, u.ID, authenticator) {
		t.Fatal("authenticator lockout missing")
	}
	h.clearCodeFailures(r, u.ID, authenticator)
	if h.lockedOut(httptest.NewRecorder(), r, u.ID, authenticator) {
		t.Fatal("a correct code did not clear the authenticator failures")
	}
}

func TestCheckTargetRefusesAMissingActor(t *testing.T) {
	if err := checkTarget(nil, nil, &User{ID: uuid.New()}); err == nil {
		t.Fatal("a missing actor passed")
	}
}
