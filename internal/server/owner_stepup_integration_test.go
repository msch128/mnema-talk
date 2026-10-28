//go:build integration

package server

import (
	"net/http"
	"testing"
)

// ownerConfirm is the Owner's confirmation for the takeover routes when their
// account has no second factor.
func ownerConfirm() map[string]string {
	return map[string]string{"current_password": ownerPassword}
}

func TestTakeoverRoutesNeedTheOwnersConfirmation(t *testing.T) {
	routes := []struct {
		name, suffix string
		okStatus     int
		body         func(proof map[string]string) map[string]string
	}{
		{"set password", "/password", http.StatusNoContent, func(proof map[string]string) map[string]string {
			body := map[string]string{"password": "chosen-by-admin-123"}
			for k, v := range proof {
				body[k] = v
			}
			return body
		}},
		{"temporary password", "/password-reset", http.StatusOK, func(proof map[string]string) map[string]string { return proof }},
		{"two-factor reset", "/two-factor/reset", http.StatusNoContent, func(proof map[string]string) map[string]string { return proof }},
	}
	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			// A fresh server per route: wrong answers share one lockout.
			a := newApp(t, false)
			admin := a.seedAdmin()
			alice := a.register(admin, "alice")
			a.enroll(alice, memberPassword)
			path := "/api/admin/users/" + alice.user.ID.String() + rt.suffix

			for label, proof := range map[string]map[string]string{
				"no proof":        {},
				"wrong password":  {"current_password": "not-the-owners-password"},
				"member password": {"current_password": memberPassword},
			} {
				if res := admin.post(path, rt.body(proof)); res.status != http.StatusForbidden {
					t.Errorf("%s: want 403, got %d %s", label, res.status, res.body)
				}
			}
			if res := a.password(a.anon(), "alice", memberPassword); res.status != http.StatusAccepted {
				t.Fatalf("a refused takeover changed the account: %d", res.status)
			}
			if n := a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action IN ('user.two_factor_reset', 'user.password_set', 'user.password_reset')`); n != 0 {
				t.Fatalf("refused requests were audited as done: %d", n)
			}
			// Without a second factor on the Owner's account the password is enough.
			if res := admin.post(path, rt.body(ownerConfirm())); res.status != rt.okStatus {
				t.Fatalf("with the Owner's password: want %d, got %d %s", rt.okStatus, res.status, res.body)
			}
		})
	}
}

func TestTakeoverRoutesAskTheOwnersSecondFactor(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	e := a.enroll(admin, ownerPassword)
	path := "/api/admin/users/" + alice.user.ID.String() + "/two-factor/reset"
	a.enroll(alice, memberPassword)

	if res := admin.post(path, ownerConfirm()); res.status != http.StatusForbidden {
		t.Fatalf("password without the code: %d %s", res.status, res.body)
	}
	if res := admin.post(path, map[string]string{"current_password": ownerPassword, "code": wrongCode(1, e.auth)}); res.status != http.StatusForbidden {
		t.Fatalf("wrong code: %d %s", res.status, res.body)
	}
	if res := admin.post(path, map[string]string{"current_password": "wrong-password-123", "code": e.auth.code(1)}); res.status != http.StatusForbidden {
		t.Fatalf("wrong password with a right code: %d %s", res.status, res.body)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'user.two_factor_reset'`); n != 0 {
		t.Fatalf("refused reset was audited: %d", n)
	}
	if res := admin.post(path, map[string]string{"current_password": ownerPassword, "code": e.auth.code(1)}); res.status != http.StatusNoContent {
		t.Fatalf("right password and code: %d %s", res.status, res.body)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'user.two_factor_reset'`); n != 1 {
		t.Fatalf("reset audited %d times", n)
	}
}

func TestTakeoverConfirmationIsRateLimited(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	path := "/api/admin/users/" + alice.user.ID.String() + "/password-reset"
	for i := 0; i < 5; i++ {
		if res := admin.post(path, map[string]string{"current_password": "wrong-password-123"}); res.status != http.StatusForbidden {
			t.Fatalf("attempt %d: %d", i, res.status)
		}
	}
	if res := admin.post(path, ownerConfirm()); res.status != http.StatusTooManyRequests {
		t.Fatalf("locked out: want 429, got %d %s", res.status, res.body)
	}
}
