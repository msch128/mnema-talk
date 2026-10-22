//go:build integration

package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func nativeSecondBody(challenge, code string, instance uuid.UUID) string {
	b, _ := json.Marshal(map[string]string{"challenge": challenge, "code": code, "client_instance_id": instance.String()})
	return string(b)
}

// A native sign-in must not skip the second factor: the password alone yields
// a challenge, not a grant.
func TestNativeLoginAsksForTheSecondFactor(t *testing.T) {
	pool, user, h := nativeHTTPFixture(t)
	ctx := t.Context()
	s := h.accounts.Sessions
	secret, err := s.beginEnrolment(ctx, pool, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := s.confirmEnrolment(ctx, tx, user.ID, totpCode(secret, totpStep(time.Now())))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE user_totp SET last_step = 0`); err != nil {
		t.Fatal(err)
	}

	instance := uuid.New()
	w := nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", nativeLoginBody(user.Username, testPassword, instance)), http.StatusAccepted)
	body := w.Body.String()
	if strings.Contains(body, "access_token") || strings.Contains(body, "refresh_token") {
		t.Fatalf("no grant before the second step: %s", body)
	}
	var ch TwoFactorChallenge
	if err := json.Unmarshal(w.Body.Bytes(), &ch); err != nil || !ch.TwoFactorRequired || ch.Challenge == "" {
		t.Fatalf("challenge: %s", body)
	}

	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login/2fa", nativeSecondBody(ch.Challenge, "000000", instance)), http.StatusUnauthorized)
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login/2fa", `{"challenge":"`+ch.Challenge+`"}`), http.StatusBadRequest)

	code := totpCode(secret, totpStep(time.Now()))
	w = nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login/2fa", nativeSecondBody(ch.Challenge, code, instance)), http.StatusOK)
	var grant nativeClientGrant
	if err := json.Unmarshal(w.Body.Bytes(), &grant); err != nil || grant.User.ID != user.ID || len(grant.Access) != 43 {
		t.Fatalf("grant after the second step: %v %s", err, "[redacted]")
	}
	// The challenge is spent.
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login/2fa", nativeSecondBody(ch.Challenge, code, instance)), http.StatusUnauthorized)
}
