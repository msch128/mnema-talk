//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/mailer/mailtest"
)

func requestReset(c *client, email string) response {
	return c.post("/api/auth/password-reset/request", map[string]string{"email": email})
}

func confirmReset(c *client, token, password string) response {
	return c.post("/api/auth/password-reset/confirm", map[string]string{"token": token, "new_password": password})
}

// settle waits until the background mail work of the app is done.
func (a *app) settle() { a.router.PreviewDeps().Accounts.DrainMail() }

func TestPasswordResetByMailSetsANewPasswordOnce(t *testing.T) {
	a, ms := mailApp(t)
	admin := a.seedAdmin()
	alice := a.confirmedMember(admin, ms, "alice", "alice@example.org")
	other := a.login("alice", memberPassword) // a second signed-in device

	anon := a.anon()
	res := requestReset(anon, "Alice@Example.org")
	if res.status != http.StatusAccepted || len(res.body) != 0 {
		t.Fatalf("request: %d %q", res.status, res.body)
	}
	mails := ms.WaitFor(t, 2)
	reset := mails[1]
	if reset.To != "alice@example.org" || !strings.HasPrefix(reset.Link(), a.srv.URL+"/reset-password#token=") {
		t.Fatalf("reset mail: %+v", reset)
	}

	// A weak password is refused and the link stays usable.
	if res := confirmReset(anon, reset.Token(), "short"); res.status != http.StatusBadRequest {
		t.Fatalf("weak password: %d", res.status)
	}
	if res := confirmReset(anon, reset.Token(), "a-brand-new-password"); res.status != http.StatusNoContent {
		t.Fatalf("confirm: %d %s", res.status, res.body)
	}

	// The link works once; the old password and every old session are gone.
	if res := confirmReset(anon, reset.Token(), "yet-another-password"); res.status != http.StatusBadRequest {
		t.Fatalf("reused link: %d", res.status)
	}
	if res := a.anon().post("/api/auth/login", map[string]string{"username": "alice", "password": memberPassword}); res.status != http.StatusUnauthorized {
		t.Fatalf("old password: %d", res.status)
	}
	for name, c := range map[string]*client{"first device": alice, "second device": other} {
		if res := c.get("/api/auth/me"); res.status != http.StatusUnauthorized {
			t.Errorf("%s still signed in: %d", name, res.status)
		}
	}
	if res := a.anon().post("/api/auth/login", map[string]string{"username": "alice", "password": "a-brand-new-password"}); res.status != http.StatusOK {
		t.Fatalf("new password: %d %s", res.status, res.body)
	}
}

func TestPasswordResetAnswersAlikeForEveryAddress(t *testing.T) {
	a, ms := mailApp(t)
	admin := a.seedAdmin()
	a.confirmedMember(admin, ms, "alice", "alice@example.org")
	unconfirmed := a.register(admin, "carol")
	a.setEmail(unconfirmed, "carol@example.org")
	dan := a.confirmedMember(admin, ms, "dan", "dan@example.org")
	if res := admin.post("/api/admin/users/"+dan.user.ID.String()+"/disable", nil); res.status != http.StatusNoContent {
		t.Fatalf("disable: %d", res.status)
	}
	base := len(ms.Messages())

	var first response
	for i, email := range []string{"alice@example.org", "carol@example.org", "dan@example.org", "nobody@example.org"} {
		res := requestReset(a.anon(), email)
		if i == 0 {
			first = res
			continue
		}
		if res.status != first.status || string(res.body) != string(first.body) {
			t.Fatalf("%s answers differently: %d %q vs %d %q", email, res.status, res.body, first.status, first.body)
		}
		for _, h := range []string{"Content-Type", "Content-Length", "Cache-Control", "Set-Cookie"} {
			if res.header.Get(h) != first.header.Get(h) {
				t.Errorf("%s: header %s differs", email, h)
			}
		}
	}
	a.settle()
	got := ms.Messages()[base:]
	if len(got) != 1 || got[0].To != "alice@example.org" {
		t.Fatalf("only the confirmed, active account may get a mail: %+v", got)
	}
	// A malformed address is the caller's error, not an account fact.
	if res := requestReset(a.anon(), "nonsense"); res.status != http.StatusBadRequest {
		t.Fatalf("malformed: %d", res.status)
	}
}

func TestPasswordResetLinksAreSingleLiveAndShortLived(t *testing.T) {
	a, ms := mailApp(t)
	a.confirmedMember(a.seedAdmin(), ms, "alice", "alice@example.org")
	anon := a.anon()

	requestReset(anon, "alice@example.org")
	first := ms.WaitFor(t, 2)[1]
	requestReset(anon, "alice@example.org")
	second := ms.WaitFor(t, 3)[2]
	if res := confirmReset(anon, first.Token(), "a-brand-new-password"); res.status != http.StatusBadRequest {
		t.Fatalf("an older link must die when a newer one is mailed: %d", res.status)
	}
	if _, err := a.db.Exec(context.Background(), `UPDATE account_tokens SET expires_at = NOW() - INTERVAL '1 second' WHERE purpose = 'password_reset'`); err != nil {
		t.Fatal(err)
	}
	if res := confirmReset(anon, second.Token(), "a-brand-new-password"); res.status != http.StatusBadRequest {
		t.Fatalf("an expired link: %d", res.status)
	}
	var lifetime time.Duration
	err := a.db.QueryRow(context.Background(), `SELECT MAX(expires_at - created_at) FROM account_tokens WHERE purpose = 'password_reset'`).Scan(&lifetime)
	if err != nil || lifetime > 30*time.Minute+time.Second {
		t.Fatalf("lifetime %v err %v", lifetime, err)
	}
}

func TestPasswordResetTokenIsStoredHashedAndKeepsItsPurpose(t *testing.T) {
	a, ms := mailApp(t)
	alice := a.register(a.seedAdmin(), "alice")
	a.setEmail(alice, "alice@example.org")
	verify := ms.WaitFor(t, 1)[0]

	var stored int
	if err := a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM account_tokens WHERE token_hash = convert_to($1, 'UTF8')`, verify.Token()).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("the token must not be stored in clear: %d %v", stored, err)
	}
	// A confirmation link is no reset link.
	if res := confirmReset(a.anon(), verify.Token(), "a-brand-new-password"); res.status != http.StatusBadRequest {
		t.Fatalf("a link for another purpose: %d", res.status)
	}
	if res := a.anon().post("/api/auth/login", map[string]string{"username": "alice", "password": memberPassword}); res.status != http.StatusOK {
		t.Fatal("the password must be untouched")
	}
}

func TestPasswordResetOfDisabledAccountFails(t *testing.T) {
	a, ms := mailApp(t)
	admin := a.seedAdmin()
	alice := a.confirmedMember(admin, ms, "alice", "alice@example.org")
	requestReset(a.anon(), "alice@example.org")
	tok := ms.WaitFor(t, 2)[1].Token()
	admin.post("/api/admin/users/"+alice.user.ID.String()+"/disable", nil)
	if res := confirmReset(a.anon(), tok, "a-brand-new-password"); res.status != http.StatusBadRequest {
		t.Fatalf("a disabled account cannot be reset by link: %d", res.status)
	}
}

func TestChangingOrRemovingTheAddressKillsOpenResetLinks(t *testing.T) {
	a, ms := mailApp(t)
	alice := a.confirmedMember(a.seedAdmin(), ms, "alice", "alice@example.org")
	anon := a.anon()
	requestReset(anon, "alice@example.org")
	link := ms.WaitFor(t, 2)[1].Token()

	a.setEmail(alice, "elsewhere@example.org")
	if res := confirmReset(anon, link, "a-brand-new-password"); res.status != http.StatusBadRequest {
		t.Fatalf("a link mailed to the old address must die with it: %d", res.status)
	}
	// The new address is unconfirmed, so no reset is possible for it yet.
	before := len(ms.Messages())
	requestReset(anon, "elsewhere@example.org")
	a.settle()
	if len(ms.Messages()) != before {
		t.Fatal("an unconfirmed address must not receive reset links")
	}
}

func TestOwnerResetNeedsTheLongerPassword(t *testing.T) {
	a, ms := mailApp(t)
	admin := a.seedAdmin()
	admin.put("/api/users/me/email", map[string]string{"email": "owner@example.org", "current_password": ownerPassword})
	tok := ms.WaitFor(t, 1)[0].Token()
	admin.post("/api/auth/email/verify", map[string]string{"token": tok})

	requestReset(a.anon(), "owner@example.org")
	reset := ms.WaitFor(t, 2)[1].Token()
	if res := confirmReset(a.anon(), reset, "twelve-chars"); res.status != http.StatusBadRequest {
		t.Fatalf("the Owner needs 14 characters: %d", res.status)
	}
	if res := confirmReset(a.anon(), reset, "fourteen-chars!!"); res.status != http.StatusNoContent {
		t.Fatalf("long enough: %d %s", res.status, res.body)
	}
}

func TestPasswordResetRequestsAreRateLimited(t *testing.T) {
	a, ms := mailApp(t)
	a.confirmedMember(a.seedAdmin(), ms, "alice", "alice@example.org")
	anon := a.anon()
	for i := 0; i < 8; i++ {
		if res := requestReset(anon, "alice@example.org"); res.status != http.StatusAccepted {
			t.Fatalf("request %d: %d", i, res.status)
		}
	}
	a.settle()
	// The address gets a few mails an hour; further requests still answer 202.
	if n := len(ms.Messages()) - 1; n > 5 {
		t.Fatalf("at most five reset mails per hour for an address: %d", n)
	}
	var limited bool
	for i := 0; i < 5; i++ {
		if requestReset(anon, "x"+string(rune('a'+i))+"@example.org").status == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("the client address must hit its limit (10 per hour)")
	}
}

func TestOwnerCanSendATestMail(t *testing.T) {
	a, ms := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")

	if res := alice.post("/api/admin/system/mail/test", map[string]string{"to": "alice@example.org"}); res.status != http.StatusForbidden {
		t.Fatalf("members cannot: %d", res.status)
	}
	if res := admin.post("/api/admin/system/mail/test", nil); res.status != http.StatusBadRequest {
		t.Fatalf("without an address on the Owner account or a recipient: %d", res.status)
	}
	if res := admin.post("/api/admin/system/mail/test", map[string]string{"to": "ops@example.org"}); res.status != http.StatusNoContent {
		t.Fatalf("explicit recipient: %d %s", res.status, res.body)
	}
	admin.put("/api/users/me/email", map[string]string{"email": "owner@example.org", "current_password": ownerPassword})
	if res := admin.post("/api/admin/system/mail/test", nil); res.status != http.StatusNoContent {
		t.Fatalf("default recipient: %d %s", res.status, res.body)
	}
	got := ms.WaitFor(t, 3)
	if got[0].To != "ops@example.org" || got[2].To != "owner@example.org" {
		t.Fatalf("recipients: %+v", got)
	}
	assertAuditHasNoAddress(t, a, "system.mail_test")

	var status struct {
		Mail MailStatus `json:"mail"`
	}
	admin.get("/api/admin/system").decode(t, &status)
	if !status.Mail.Configured || status.Mail.TLS != "none" || !strings.Contains(status.Mail.From, "noreply@example.org") {
		t.Fatalf("status: %+v", status.Mail)
	}
	for i := 0; i < 5; i++ {
		admin.post("/api/admin/system/mail/test", map[string]string{"to": "ops@example.org"})
	}
	if res := admin.post("/api/admin/system/mail/test", map[string]string{"to": "ops@example.org"}); res.status != http.StatusTooManyRequests {
		t.Fatalf("rate limit: %d", res.status)
	}
}

func assertAuditHasNoAddress(t *testing.T, a *app, action string) {
	t.Helper()
	var n int
	err := a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = $1`, action).Scan(&n)
	if err != nil || n == 0 {
		t.Fatalf("audit entry %s: %d %v", action, n, err)
	}
	err = a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE details::text ILIKE '%@example.org%' OR target_name ILIKE '%@%'`).Scan(&n)
	if err != nil || n != 0 {
		t.Fatalf("the audit log must not hold addresses: %d %v", n, err)
	}
}

func TestTestMailReportsTheFailingStage(t *testing.T) {
	cfg := mailtest.Start(t).Config()
	cfg.Port = 1 // nothing listens there
	a := newAppWithConfig(t, func(c *config.Config) { c.SMTP = cfg })
	admin := a.seedAdmin()
	res := admin.post("/api/admin/system/mail/test", map[string]string{"to": "ops@example.org"})
	if res.status != http.StatusServiceUnavailable || !strings.Contains(string(res.body), "connect") {
		t.Fatalf("%d %s", res.status, res.body)
	}
	if strings.Contains(string(res.body), "127.0.0.1") {
		t.Fatalf("no addresses in errors: %s", res.body)
	}
}
