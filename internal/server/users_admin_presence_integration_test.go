//go:build integration

package server

import (
	"net/http"
	"testing"
)

// The chosen presence is private to its owner: the admin list, like every other
// payload about someone else, leaves it out.
func TestAdminUserListLeavesOutThePrivatePresence(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	if res := max.put("/api/users/me/presence", map[string]string{"presence": "dnd"}); res.status != http.StatusOK && res.status != http.StatusNoContent {
		t.Fatalf("set presence: %d %s", res.status, res.body)
	}
	res := admin.get("/api/admin/users")
	if res.status != http.StatusOK {
		t.Fatalf("list users: %d %s", res.status, res.body)
	}
	var users []map[string]any
	res.decode(t, &users)
	for _, u := range users {
		if p, ok := u["presence"]; ok {
			t.Errorf("%v: admin list exposes the chosen presence %v", u["username"], p)
		}
	}
}
