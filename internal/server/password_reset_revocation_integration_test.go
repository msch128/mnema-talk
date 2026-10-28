//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
)

// An open reset link must die with every event that changes how the account
// is secured: a stolen link must not outlive the owner's answer to it.
func TestOpenResetLinkDiesWhenTheAccountIsSecured(t *testing.T) {
	cases := map[string]func(a *app, admin, alice *client){
		"member changes the password": func(a *app, _, alice *client) {
			res := alice.put("/api/auth/password", map[string]string{"current_password": memberPassword, "new_password": "changed-by-the-member"})
			if res.status != http.StatusNoContent {
				t.Fatalf("change password: %d %s", res.status, res.body)
			}
		},
		"member logs out everywhere": func(a *app, _, alice *client) {
			if res := alice.post("/api/auth/logout-all", nil); res.status != http.StatusNoContent {
				t.Fatalf("logout all: %d", res.status)
			}
		},
		"owner sets a password": func(a *app, admin, alice *client) {
			res := admin.post("/api/admin/users/"+alice.user.ID.String()+"/password", map[string]string{"password": "set-by-the-owner", "current_password": ownerPassword})
			if res.status != http.StatusNoContent {
				t.Fatalf("set password: %d %s", res.status, res.body)
			}
		},
		"owner issues a temporary password": func(a *app, admin, alice *client) {
			if res := admin.post("/api/admin/users/"+alice.user.ID.String()+"/password-reset", ownerConfirm()); res.status != http.StatusOK {
				t.Fatalf("temporary password: %d", res.status)
			}
		},
		"owner revokes all sessions": func(a *app, admin, alice *client) {
			if res := admin.post("/api/admin/users/"+alice.user.ID.String()+"/sessions/revoke", nil); res.status != http.StatusNoContent {
				t.Fatalf("revoke: %d", res.status)
			}
		},
		"the version changes by any other path": func(a *app, _, alice *client) {
			_, err := a.db.Exec(context.Background(), `UPDATE users SET token_version = token_version + 1 WHERE id = $1`, alice.user.ID)
			if err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, secure := range cases {
		t.Run(name, func(t *testing.T) {
			a, ms := mailApp(t)
			admin := a.seedAdmin()
			alice := a.confirmedMember(admin, ms, "alice", "alice@example.org")
			anon := a.anon()
			requestReset(anon, "alice@example.org")
			link := ms.WaitFor(t, 2)[1].Token()

			secure(a, admin, alice)

			if res := confirmReset(anon, link, "the-attackers-password"); res.status != http.StatusBadRequest {
				t.Fatalf("the link must be refused: %d %s", res.status, res.body)
			}
			if res := a.anon().post("/api/auth/login", map[string]string{"username": "alice", "password": "the-attackers-password"}); res.status == http.StatusOK {
				t.Fatal("the attacker's password must not work")
			}
		})
	}
}

func TestResetLinkStillWorksWhenNothingChanged(t *testing.T) {
	a, ms := mailApp(t)
	a.confirmedMember(a.seedAdmin(), ms, "alice", "alice@example.org")
	anon := a.anon()
	requestReset(anon, "alice@example.org")
	link := ms.WaitFor(t, 2)[1].Token()
	if res := confirmReset(anon, link, "a-brand-new-password"); res.status != http.StatusNoContent {
		t.Fatalf("%d %s", res.status, res.body)
	}
}
