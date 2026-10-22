//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestEnrolmentNeedsThePasswordAndAFirstCode(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")

	if res := alice.post("/api/users/me/2fa/setup", map[string]string{"current_password": "wrong-password"}); res.status != http.StatusForbidden {
		t.Fatalf("setup needs the password: %d", res.status)
	}
	res := alice.post("/api/users/me/2fa/setup", map[string]string{"current_password": memberPassword})
	if res.status != http.StatusOK || res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("setup: %d %v", res.status, res.header)
	}
	if v := a.account(alice); v.TwoFactorEnabled {
		t.Fatal("a pending secret must not switch anything on")
	}
	if got := a.password(a.anon(), "alice", memberPassword); got.status != http.StatusOK {
		t.Fatalf("sign-in is unchanged until a code is confirmed: %d", got.status)
	}
	if res := alice.post("/api/users/me/2fa/enable", map[string]string{"code": "000000"}); res.status != http.StatusForbidden {
		t.Fatalf("a wrong first code: %d", res.status)
	}
	if res := alice.post("/api/users/me/2fa/enable", map[string]string{"code": ""}); res.status != http.StatusForbidden {
		t.Fatalf("an empty first code: %d", res.status)
	}
}

func TestEnrolmentGivesTenSingleUseRecoveryCodes(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)

	seen := map[string]bool{}
	for _, c := range e.recovery {
		if len(c) != 14 || strings.Count(c, "-") != 2 || seen[c] {
			t.Fatalf("recovery code %q", c)
		}
		seen[c] = true
	}
	if len(e.recovery) != 10 {
		t.Fatalf("want ten codes, got %d", len(e.recovery))
	}
	if v := a.account(alice); !v.TwoFactorEnabled || v.RecoveryCodesRemaining != 10 {
		t.Fatalf("account: %+v", v)
	}
	// Only keyed hashes are stored, never the codes or the secret.
	for _, c := range e.recovery {
		plain := strings.ReplaceAll(c, "-", "")
		if n := a.countRows(`SELECT COUNT(*) FROM recovery_codes WHERE code_hash = convert_to($1, 'UTF8')`, plain); n != 0 {
			t.Fatal("recovery code stored in the clear")
		}
	}
	if n := a.countRows(`SELECT COUNT(*) FROM user_totp WHERE position($1::bytea in secret_enc) > 0`, e.auth.secret); n != 0 {
		t.Fatal("secret stored in the clear")
	}
	// Setting up again while on is refused; the secret stays.
	if res := alice.post("/api/users/me/2fa/setup", map[string]string{"current_password": memberPassword}); res.status != http.StatusConflict {
		t.Fatalf("setup while on: %d", res.status)
	}
	if res := alice.post("/api/users/me/2fa/enable", map[string]string{"code": e.auth.code(1)}); res.status != http.StatusConflict {
		t.Fatalf("enable while on: %d", res.status)
	}
}

func TestSignInAsksForTheCodeAfterThePassword(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()

	c := a.anon()
	res := a.password(c, "alice", memberPassword)
	if res.status != http.StatusAccepted || res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("step one: %d %v", res.status, res.header)
	}
	if strings.Contains(res.header.Get("Set-Cookie"), "session") || strings.Contains(string(res.body), `"user"`) {
		t.Fatalf("a password alone must not give a session or a profile: %v %s", res.header, res.body)
	}
	var ch challenge
	res.decode(t, &ch)
	if !ch.TwoFactorRequired || len(ch.Challenge) != 43 || ch.ExpiresIn != 300 {
		t.Fatalf("challenge: %+v", ch)
	}
	// Skipping the second step gets nowhere.
	if me := c.get("/api/auth/me"); me.status != http.StatusUnauthorized {
		t.Fatalf("session without step two: %d", me.status)
	}
	if res := secondStep(c, ch.Challenge, wrongCode(1, e.auth)); res.status != http.StatusUnauthorized {
		t.Fatalf("a wrong code: %d", res.status)
	}
	res = secondStep(c, ch.Challenge, e.auth.code(1))
	if res.status != http.StatusOK || !strings.Contains(res.header.Get("Set-Cookie"), "session") {
		t.Fatalf("a right code: %d %s", res.status, res.body)
	}
	if strings.Contains(string(res.body), "security_notice") {
		t.Fatalf("no notice after a single mistake: %s", res.body)
	}
	if me := c.get("/api/auth/me"); me.status != http.StatusOK {
		t.Fatalf("signed in: %d", me.status)
	}
	// The known-device cookie only comes with the finished sign-in.
	if !strings.Contains(strings.Join(res.header.Values("Set-Cookie"), ";"), "mnema_dev_") {
		t.Fatal("known-device cookie missing after step two")
	}
}

func TestAnAuthenticatorCodeWorksOnce(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()

	code := e.auth.code(0)
	c := a.anon()
	if res := secondStep(c, a.startLogin(c, "alice", memberPassword).Challenge, code); res.status != http.StatusOK {
		t.Fatalf("first use: %d %s", res.status, res.body)
	}
	other := a.anon()
	if res := secondStep(other, a.startLogin(other, "alice", memberPassword).Challenge, code); res.status != http.StatusUnauthorized {
		t.Fatalf("a replay of the same code: %d", res.status)
	}
	// Neither may an older step of the window be used after a newer one.
	older := a.anon()
	if res := secondStep(older, a.startLogin(older, "alice", memberPassword).Challenge, e.auth.code(-1)); res.status != http.StatusUnauthorized {
		t.Fatalf("an older step after a newer: %d", res.status)
	}
}

func TestARecoveryCodeSignsInOnceAndIsCounted(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)

	c := a.anon()
	spaced := strings.ToUpper(strings.ReplaceAll(e.recovery[0], "-", " "))
	if res := secondStep(c, a.startLogin(c, "alice", memberPassword).Challenge, spaced); res.status != http.StatusOK {
		t.Fatalf("recovery code: %d %s", res.status, res.body)
	}
	if v := a.account(alice); v.RecoveryCodesRemaining != 9 {
		t.Fatalf("remaining: %+v", v)
	}
	again := a.anon()
	if res := secondStep(again, a.startLogin(again, "alice", memberPassword).Challenge, e.recovery[0]); res.status != http.StatusUnauthorized {
		t.Fatalf("a used recovery code: %d", res.status)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'auth.recovery_code_used'`); n != 1 {
		t.Fatalf("audit entries: %d", n)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM audit_log WHERE details::text ILIKE '%' || $1 || '%'`, strings.ReplaceAll(e.recovery[0], "-", "")); n != 0 {
		t.Fatal("the audit log must not hold codes")
	}
}

func TestAChallengeIsSingleUseShortLivedAndBoundToTheSignIn(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()

	c := a.anon()
	ch := a.startLogin(c, "alice", memberPassword).Challenge
	// Garbage and foreign values all fail alike.
	for _, bad := range []string{"", "short", strings.Repeat("A", 43), ch + "x"} {
		if res := secondStep(c, bad, e.auth.code(0)); res.status != http.StatusUnauthorized {
			t.Errorf("challenge %q: %d", bad, res.status)
		}
	}
	// Five wrong codes use the challenge up, even for the right code after.
	for i := 0; i < 5; i++ {
		secondStep(c, ch, wrongCode(i+10, e.auth))
	}
	if res := secondStep(c, ch, e.auth.code(0)); res.status != http.StatusUnauthorized {
		t.Fatalf("an exhausted challenge must die: %d", res.status)
	}

	// A finished sign-in spends its challenge.
	ch2 := a.startLogin(c, "alice", memberPassword).Challenge
	if res := secondStep(c, ch2, e.auth.code(0)); res.status != http.StatusOK {
		t.Fatalf("%d", res.status)
	}
	a.freshStep()
	if res := secondStep(a.anon(), ch2, e.auth.code(0)); res.status != http.StatusUnauthorized {
		t.Fatalf("a spent challenge: %d", res.status)
	}

	// Expiry.
	ch3 := a.startLogin(c, "alice", memberPassword).Challenge
	if _, err := a.db.Exec(context.Background(), `UPDATE login_challenges SET expires_at = NOW() - INTERVAL '1 second'`); err != nil {
		t.Fatal(err)
	}
	a.freshStep()
	if res := secondStep(c, ch3, e.auth.code(0)); res.status != http.StatusUnauthorized {
		t.Fatalf("an expired challenge: %d", res.status)
	}
}

func TestAChallengeDiesWhenTheAccountIsSecuredMeanwhile(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()

	c := a.anon()
	ch := a.startLogin(c, "alice", memberPassword).Challenge
	if res := alice.post("/api/auth/logout-all", nil); res.status != http.StatusNoContent {
		t.Fatalf("logout-all: %d", res.status)
	}
	if res := secondStep(c, ch, e.auth.code(0)); res.status != http.StatusUnauthorized {
		t.Fatalf("revoked sessions must kill the challenge: %d", res.status)
	}

	ch = a.startLogin(c, "alice", memberPassword).Challenge
	admin.post("/api/admin/users/"+alice.user.ID.String()+"/disable", nil)
	if res := secondStep(c, ch, e.auth.code(0)); res.status != http.StatusUnauthorized {
		t.Fatalf("a disabled account: %d", res.status)
	}
}

func TestTurningItOffAndNewRecoveryCodesNeedPasswordAndCode(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()

	body := func(pw, code string) map[string]string {
		return map[string]string{"current_password": pw, "code": code}
	}
	if res := alice.post("/api/users/me/2fa/disable", body("wrong-password", e.auth.code(1))); res.status != http.StatusForbidden {
		t.Fatalf("wrong password: %d", res.status)
	}
	if res := alice.post("/api/users/me/2fa/disable", body(memberPassword, wrongCode(3, e.auth))); res.status != http.StatusForbidden {
		t.Fatalf("wrong code: %d", res.status)
	}
	if v := a.account(alice); !v.TwoFactorEnabled {
		t.Fatal("still on")
	}

	res := alice.post("/api/users/me/2fa/recovery-codes", body(memberPassword, e.recovery[0]))
	if res.status != http.StatusOK {
		t.Fatalf("regenerate with a recovery code: %d %s", res.status, res.body)
	}
	var fresh struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	res.decode(t, &fresh)
	if len(fresh.RecoveryCodes) != 10 || fresh.RecoveryCodes[0] == e.recovery[0] {
		t.Fatalf("fresh codes: %v", fresh.RecoveryCodes)
	}
	c := a.anon()
	if res := secondStep(c, a.startLogin(c, "alice", memberPassword).Challenge, e.recovery[1]); res.status != http.StatusUnauthorized {
		t.Fatalf("an old recovery code after regeneration: %d", res.status)
	}

	if res := alice.post("/api/users/me/2fa/disable", body(memberPassword, fresh.RecoveryCodes[0])); res.status != http.StatusNoContent {
		t.Fatalf("disable: %d %s", res.status, res.body)
	}
	if n := a.countRows(`SELECT (SELECT COUNT(*) FROM user_totp) + (SELECT COUNT(*) FROM recovery_codes)`); n != 0 {
		t.Fatalf("everything must be gone: %d", n)
	}
	if got := a.password(a.anon(), "alice", memberPassword); got.status != http.StatusOK {
		t.Fatalf("plain sign-in again: %d", got.status)
	}
	if res := alice.post("/api/users/me/2fa/disable", body(memberPassword, "123456")); res.status != http.StatusConflict {
		t.Fatalf("disable while off: %d", res.status)
	}
}

func TestTheOwnerCanResetAMembersSecondFactor(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")
	a.enroll(alice, memberPassword)

	path := "/api/admin/users/" + alice.user.ID.String() + "/two-factor/reset"
	if res := bob.post(path, nil); res.status != http.StatusForbidden {
		t.Fatalf("members cannot: %d", res.status)
	}
	if res := admin.post("/api/admin/users/"+admin.user.ID.String()+"/two-factor/reset", nil); res.status != http.StatusBadRequest {
		t.Fatalf("the Owner is not a target: %d", res.status)
	}
	if res := admin.post(path, nil); res.status != http.StatusNoContent {
		t.Fatalf("reset: %d", res.status)
	}
	if me := alice.get("/api/auth/me"); me.status != http.StatusUnauthorized {
		t.Fatalf("sessions must end: %d", me.status)
	}
	if got := a.password(a.anon(), "alice", memberPassword); got.status != http.StatusOK {
		t.Fatalf("password alone again: %d", got.status)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'user.two_factor_reset'`); n != 1 {
		t.Fatalf("audited: %d", n)
	}
}

func TestAMailResetDoesNotSkipTheSecondFactor(t *testing.T) {
	a, ms := mailApp(t)
	admin := a.seedAdmin()
	alice := a.confirmedMember(admin, ms, "alice", "alice@example.org")
	e := a.enroll(alice, memberPassword)
	a.freshStep()

	anon := a.anon()
	requestReset(anon, "alice@example.org")
	tok := ms.WaitFor(t, 2)[1].Token()
	res := confirmReset(anon, tok, "a-brand-new-password")
	if res.status != http.StatusNoContent || strings.Contains(res.header.Get("Set-Cookie"), "session") {
		t.Fatalf("reset: %d %v", res.status, res.header)
	}
	c := a.anon()
	if res := a.password(c, "alice", "a-brand-new-password"); res.status != http.StatusAccepted {
		t.Fatalf("the new password still needs the code: %d", res.status)
	}
	_ = e
}

func TestTheAccountPageShowsTheSecondFactorState(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	if v := a.account(alice); v.TwoFactorEnabled || v.RecoveryCodesRemaining != 0 {
		t.Fatalf("off: %+v", v)
	}
	anon := a.anon()
	for _, path := range []string{"/api/users/me/2fa/setup", "/api/users/me/2fa/enable", "/api/users/me/2fa/disable", "/api/users/me/2fa/recovery-codes"} {
		if res := anon.post(path, map[string]string{}); res.status != http.StatusUnauthorized {
			t.Errorf("%s: %d", path, res.status)
		}
	}
	if res := anon.post("/api/auth/login/2fa", map[string]string{"challenge": "x", "code": "1"}); res.status != http.StatusUnauthorized {
		t.Errorf("second step without a challenge: %d", res.status)
	}
}
