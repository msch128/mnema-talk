//go:build integration

package server

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/mailer/mailtest"
)

const (
	memberPassword = "member-password-123"
	ownerPassword  = "admin-password-123"
)

// mailApp is an app whose server can send mail to a fake SMTP server.
func mailApp(t *testing.T) (*app, *mailtest.Server) {
	t.Helper()
	ms := mailtest.Start(t)
	a := newAppWithConfig(t, func(c *config.Config) { c.SMTP = ms.Config() })
	return a, ms
}

type accountView struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	MailAvailable bool   `json:"mail_available"`

	TwoFactorEnabled       bool `json:"two_factor_enabled"`
	RecoveryCodesRemaining int  `json:"recovery_codes_remaining"`
}

func (a *app) account(c *client) accountView {
	a.t.Helper()
	res := c.get("/api/users/me/account")
	if res.status != http.StatusOK {
		a.t.Fatalf("account: %d %s", res.status, res.body)
	}
	var v accountView
	res.decode(a.t, &v)
	return v
}

func (a *app) setEmail(c *client, email string) response {
	return c.put("/api/users/me/email", map[string]string{"email": email, "current_password": memberPassword})
}

// confirmedMember registers a member, gives them an address and confirms it
// through the mailed link.
func (a *app) confirmedMember(admin *client, ms *mailtest.Server, name, email string) *client {
	a.t.Helper()
	m := a.register(admin, name)
	before := len(ms.Messages())
	if res := a.setEmail(m, email); res.status != http.StatusOK {
		a.t.Fatalf("set email: %d %s", res.status, res.body)
	}
	mails := ms.WaitFor(a.t, before+1)
	if res := m.post("/api/auth/email/verify", map[string]string{"token": mails[before].Token()}); res.status != http.StatusNoContent {
		a.t.Fatalf("verify: %d %s", res.status, res.body)
	}
	return m
}

func TestEmailIsOptionalAndNeverShownToOthers(t *testing.T) {
	a, ms := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")

	if v := a.account(alice); v.Email != "" || v.EmailVerified || !v.MailAvailable {
		t.Fatalf("a new account has no address: %+v", v)
	}
	if res := alice.put("/api/users/me/email", map[string]string{"email": "alice@example.org", "current_password": "wrong-password"}); res.status != http.StatusForbidden {
		t.Fatalf("the password is needed: %d", res.status)
	}
	if res := a.setEmail(alice, "alice@example.org"); res.status != http.StatusOK {
		t.Fatalf("set: %d %s", res.status, res.body)
	}
	if v := a.account(alice); v.Email != "alice@example.org" || v.EmailVerified {
		t.Fatalf("saved but unconfirmed: %+v", v)
	}
	ms.WaitFor(t, 1)

	// Neither a fellow member nor the Owner's user list carries it.
	for name, res := range map[string]response{
		"profile":   bob.get("/api/users/" + alice.user.ID.String()),
		"own":       alice.get("/api/auth/me"),
		"members":   bob.get("/api/members"),
		"admin":     admin.get("/api/admin/users"),
		"owner-get": admin.get("/api/users/" + alice.user.ID.String()),
	} {
		if bytes.Contains(res.body, []byte("alice@example.org")) {
			t.Errorf("%s leaks the address: %s", name, res.body)
		}
	}
	if v := a.account(bob); v.Email != "" {
		t.Fatalf("another account's view: %+v", v)
	}

	rm := alice.post("/api/users/me/email/remove", map[string]string{"current_password": memberPassword})
	if rm.status != http.StatusOK {
		t.Fatalf("remove: %d %s", rm.status, rm.body)
	}
	if v := a.account(alice); v.Email != "" {
		t.Fatalf("removed: %+v", v)
	}
}

func TestEmailRejectsAddressesThatCouldInjectHeaders(t *testing.T) {
	a, _ := mailApp(t)
	m := a.register(a.seedAdmin(), "alice")
	for _, bad := range []string{
		"", "plain", "a@b", "Name <a@example.org>", "a@example.org\r\nBcc: x@example.org",
		"a b@example.org", "a@example.org,b@example.org", "ä@example.org", "a@example.org.", strings.Repeat("a", 250) + "@example.org",
	} {
		if res := a.setEmail(m, bad); res.status != http.StatusBadRequest {
			t.Errorf("%q: want 400, got %d", bad, res.status)
		}
	}
}

func TestEmailConfirmationLinkWorksOnceAndOnlyForItsAddress(t *testing.T) {
	a, ms := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")

	a.setEmail(alice, "alice@example.org")
	first := ms.WaitFor(t, 1)[0]
	if !strings.HasPrefix(first.Link(), a.srv.URL+"/verify-email#token=") {
		t.Fatalf("the token belongs in the URL fragment: %q", first.Link())
	}

	// The same address again gets a fresh link; the older one is dead.
	if res := alice.post("/api/users/me/email/resend", nil); res.status != http.StatusNoContent {
		t.Fatalf("resend: %d %s", res.status, res.body)
	}
	second := ms.WaitFor(t, 2)[1]
	if res := alice.post("/api/auth/email/verify", map[string]string{"token": first.Token()}); res.status != http.StatusBadRequest {
		t.Fatalf("a superseded link must not work: %d", res.status)
	}
	// Changing the address kills links for the old one.
	a.setEmail(alice, "other@example.org")
	if res := a.anon().post("/api/auth/email/verify", map[string]string{"token": second.Token()}); res.status != http.StatusBadRequest {
		t.Fatalf("a link for a replaced address must not work: %d", res.status)
	}
	third := ms.WaitFor(t, 3)[2]

	anon := a.anon()
	if res := anon.post("/api/auth/email/verify", map[string]string{"token": third.Token()}); res.status != http.StatusNoContent {
		t.Fatalf("verify without a session: %d %s", res.status, res.body)
	}
	if v := a.account(alice); !v.EmailVerified || v.Email != "other@example.org" {
		t.Fatalf("confirmed: %+v", v)
	}
	if res := anon.post("/api/auth/email/verify", map[string]string{"token": third.Token()}); res.status != http.StatusBadRequest {
		t.Fatalf("a link must work once: %d", res.status)
	}
	// Typing the confirmed address again (other letter case) keeps it confirmed.
	a.setEmail(alice, "Other@Example.org")
	if v := a.account(alice); !v.EmailVerified {
		t.Fatalf("same address, still confirmed: %+v", v)
	}
	if len(ms.Messages()) != 3 {
		t.Fatalf("no new mail for a confirmed address: %d", len(ms.Messages()))
	}
}

func TestEmailLinkExpiresAndRejectsGarbage(t *testing.T) {
	a, ms := mailApp(t)
	alice := a.register(a.seedAdmin(), "alice")
	a.setEmail(alice, "alice@example.org")
	tok := ms.WaitFor(t, 1)[0].Token()

	anon := a.anon()
	for _, bad := range []string{"", "short", strings.Repeat("A", 43), strings.Repeat("A", 44), tok + "x", strings.Repeat("!", 43)} {
		if res := anon.post("/api/auth/email/verify", map[string]string{"token": bad}); res.status != http.StatusBadRequest {
			t.Errorf("token %q: %d", bad, res.status)
		}
	}
	if _, err := a.db.Exec(context.Background(), `UPDATE account_tokens SET expires_at = NOW() - INTERVAL '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if res := anon.post("/api/auth/email/verify", map[string]string{"token": tok}); res.status != http.StatusBadRequest {
		t.Fatalf("expired: %d", res.status)
	}
}

func TestOnlyOneAccountCanConfirmAnAddress(t *testing.T) {
	a, ms := mailApp(t)
	admin := a.seedAdmin()
	a.confirmedMember(admin, ms, "alice", "shared@example.org")
	bob := a.register(admin, "bob")
	// Typing an address somebody confirmed gives bob no hint about it.
	if res := a.setEmail(bob, "shared@example.org"); res.status != http.StatusOK {
		t.Fatalf("set: %d %s", res.status, res.body)
	}
	tok := ms.WaitFor(t, 2)[1].Token()
	if res := bob.post("/api/auth/email/verify", map[string]string{"token": tok}); res.status != http.StatusConflict {
		t.Fatalf("second confirmation: %d %s", res.status, res.body)
	}
	if v := a.account(bob); v.EmailVerified {
		t.Fatal("bob must stay unconfirmed")
	}
}

func TestEmailConfirmationMailsAreRateLimitedPerAccount(t *testing.T) {
	a, ms := mailApp(t)
	alice := a.register(a.seedAdmin(), "alice")
	a.setEmail(alice, "alice@example.org")
	for i := 0; i < 20; i++ {
		// Changing the address does not reset the hourly budget.
		a.setEmail(alice, "alice"+string(rune('a'+i))+"@example.org")
	}
	a.account(alice)
	if n := len(ms.Messages()); n > 5 {
		t.Fatalf("at most five mails per hour for an account, got %d", n)
	}
}

func TestWithoutSMTPNothingNeedsMail(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")

	if res := a.setEmail(alice, "alice@example.org"); res.status != http.StatusOK {
		t.Fatalf("an address can be saved without mail: %d %s", res.status, res.body)
	}
	if v := a.account(alice); v.MailAvailable || v.EmailVerified || v.Email != "alice@example.org" {
		t.Fatalf("unverified and unused: %+v", v)
	}
	if res := alice.post("/api/users/me/email/resend", nil); res.status != http.StatusBadRequest {
		t.Fatalf("resend: %d", res.status)
	}
	anon := a.anon()
	if res := anon.post("/api/auth/password-reset/request", map[string]string{"email": "alice@example.org"}); res.status != http.StatusNotFound {
		t.Fatalf("reset needs mail: %d", res.status)
	}
	var opts auth.AuthOptions
	res := anon.get("/api/auth/options")
	res.decode(t, &opts)
	if res.status != http.StatusOK || opts.PasswordReset {
		t.Fatalf("options: %d %+v", res.status, opts)
	}
	if st := admin.get("/api/admin/system"); !bytes.Contains(st.body, []byte(`"mail":{"configured":false`)) {
		t.Fatalf("system status: %s", st.body)
	}
}
