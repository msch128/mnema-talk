//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/mailer/mailtest"
)

func TestMailsFollowTheMembersLanguage(t *testing.T) {
	a, ms := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	if res := alice.put("/api/users/me/locale", map[string]string{"locale": "de"}); res.status != http.StatusOK {
		t.Fatalf("locale: %d", res.status)
	}
	a.setEmail(alice, "alice@example.org")
	verify := ms.WaitFor(t, 1)[0]
	if !strings.Contains(verify.Subject, "bestätigen") {
		t.Fatalf("German confirmation expected: %q", verify.Subject)
	}
	anon := a.anon()
	anon.post("/api/auth/email/verify", map[string]string{"token": verify.Token()})
	requestReset(anon, "alice@example.org")
	if reset := ms.WaitFor(t, 2)[1]; !strings.Contains(reset.Subject, "Passwort") {
		t.Fatalf("German reset expected: %q", reset.Subject)
	}
}

func TestEmailChangesNeedTheRightPasswordAndRespectLimits(t *testing.T) {
	a, ms := mailApp(t)
	admin := a.seedAdmin()
	alice := a.confirmedMember(admin, ms, "alice", "alice@example.org")

	if res := alice.post("/api/users/me/email/remove", map[string]string{"current_password": "wrong-password"}); res.status != http.StatusForbidden {
		t.Fatalf("remove needs the password: %d", res.status)
	}
	if v := a.account(alice); v.Email == "" {
		t.Fatal("the address must stay")
	}
	if res := alice.post("/api/users/me/email/resend", nil); res.status != http.StatusBadRequest {
		t.Fatalf("a confirmed address has nothing to resend: %d", res.status)
	}
	a.setEmail(alice, "new@example.org")
	if res := alice.post("/api/users/me/email/resend", nil); res.status != http.StatusNoContent {
		t.Fatalf("first resend: %d", res.status)
	}
	if res := alice.post("/api/users/me/email/resend", nil); res.status != http.StatusTooManyRequests {
		t.Fatalf("a second resend within a minute: %d", res.status)
	}
	if res := alice.put("/api/users/me/email", map[string]string{"email": "x@example.org", "current_password": "wrong-password"}); res.status != http.StatusForbidden {
		t.Fatalf("set needs the password: %d", res.status)
	}
}

func TestAnAddressSavesEvenWhenTheMailServerIsDown(t *testing.T) {
	cfg := mailtest.Start(t).Config()
	cfg.Port = 1
	a := newAppWithConfig(t, func(c *config.Config) { c.SMTP = cfg })
	alice := a.register(a.seedAdmin(), "alice")
	res := a.setEmail(alice, "alice@example.org")
	if res.status != http.StatusServiceUnavailable {
		t.Fatalf("the failure is reported: %d %s", res.status, res.body)
	}
	if strings.Contains(string(res.body), "connect") || strings.Contains(string(res.body), "127.0.0.1") {
		t.Fatalf("no delivery detail for members: %s", res.body)
	}
	if v := a.account(alice); v.Email != "alice@example.org" || v.EmailVerified {
		t.Fatalf("saved, unconfirmed: %+v", v)
	}
}

func TestAccountRoutesNeedASession(t *testing.T) {
	a, _ := mailApp(t)
	anon := a.anon()
	for _, path := range []string{"/api/users/me/account"} {
		if res := anon.get(path); res.status != http.StatusUnauthorized {
			t.Errorf("%s: %d", path, res.status)
		}
	}
	for _, path := range []string{"/api/users/me/email/remove", "/api/users/me/email/resend"} {
		if res := anon.post(path, map[string]string{"current_password": "x"}); res.status != http.StatusUnauthorized {
			t.Errorf("%s: %d", path, res.status)
		}
	}
	if res := anon.put("/api/users/me/email", map[string]string{"email": "a@example.org", "current_password": "x"}); res.status != http.StatusUnauthorized {
		t.Errorf("set: %d", res.status)
	}
}
