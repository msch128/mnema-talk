//go:build integration

package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
)

func nativeRegisterBody(username, invite string) string {
	body, err := json.Marshal(map[string]string{"username": username, "display_name": username, "password": testPassword, "invite_code": invite})
	if err != nil {
		panic(err)
	}
	return string(body)
}

// A newcomer registering natively while rules exist is told right away that
// they still have to accept them; without rules the field stays absent.
func TestNativeRegistrationReportsPendingRules(t *testing.T) {
	pool, owner, h := nativeHTTPFixture(t)
	h.accounts.Perms = perm.NewService(pool)
	ctx := context.Background()
	for _, code := range []string{"first-invite-code", "second-invite-code"} {
		if _, err := CreateInvite(ctx, pool, owner.ID, code, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	route, err := h.RegistrationHandler()
	if err != nil {
		t.Fatal(err)
	}
	register := func(name, code string) map[string]any {
		t.Helper()
		w := nativeStatus(t, route, nativeRequest(http.MethodPost, "/auth/register", nativeRegisterBody(name, code)), http.StatusCreated)
		var got struct {
			User map[string]any `json:"user"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got.User
	}

	if user := register("early", "first-invite-code"); user["rules_pending"] == true {
		t.Fatalf("no rules exist yet: %v", user)
	}
	if _, err := pool.Exec(ctx, `UPDATE instance_settings SET rules_text = 'Be kind.', rules_version = 1`); err != nil {
		t.Fatal(err)
	}
	// The permission snapshot is cached; a rules change invalidates it.
	h.accounts.Perms.Invalidate()
	if user := register("late", "second-invite-code"); user["rules_pending"] != true {
		t.Fatalf("a newcomer under rules must be pending: %v", user)
	}
}
