//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
)

func TestServerAcceptsSessionsSignedWithThePreviousSecret(t *testing.T) {
	previous := strings.Repeat("p", 40)
	a := newAppWithConfig(t, func(c *config.Config) { c.JWTSecretPrevious = previous })
	admin := a.seedAdmin()

	withSession := func(secret string) int {
		token, err := auth.IssueToken(admin.user.ID, 0, secret, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodGet, a.srv.URL+"/api/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: "mnema_session", Value: token})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := withSession(previous); got != http.StatusOK {
		t.Fatalf("a session from before the rotation: %d", got)
	}
	if got := withSession(strings.Repeat("x", 40)); got != http.StatusUnauthorized {
		t.Fatalf("a session signed with an unknown secret: %d", got)
	}
}

func TestServerSealsAuthenticatorSecretsUnderTheDataKey(t *testing.T) {
	a := newAppWithConfig(t, func(c *config.Config) { c.DataKey = strings.Repeat("d", 40) })
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	if n := a.countRows(`SELECT COUNT(*) FROM user_totp WHERE secret_enc LIKE 'mtk1%'::bytea`); n != 1 {
		t.Fatal("the stored secret does not name the data key it is under")
	}
	a.freshStep()
	c := a.anon()
	ch := a.startLogin(c, "alice", memberPassword)
	if res := secondStep(c, ch.Challenge, e.auth.code(0)); res.status != http.StatusOK {
		t.Fatalf("sign-in with the authenticator: %d %s", res.status, res.body)
	}
}
