//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
)

// A banned or disabled member loses the built-in relay at once, not when their
// credential runs out.
func TestBuiltinTURNFollowsAccountState(t *testing.T) {
	relay := startTestRelay(t, nil)
	a := newAppWithDeps(t, false, func(c *config.Config) { c.PublicURL = "https://talk.example.test" }, func(d *Deps) { d.TURN = relay })
	owner := a.seedAdmin()
	if res := owner.post("/api/admin/channels", map[string]any{"name": "relay-talk", "type": "voice"}); res.status != http.StatusCreated {
		t.Fatalf("create talk: %d %s", res.status, res.body)
	}
	member := a.register(owner, "relaymember")
	id := member.user.ID

	if !relay.MayRelay(id) {
		t.Fatal("an active member was refused the relay")
	}
	base := "/api/admin/users/" + id.String()
	if res := owner.post(base+"/ban", map[string]any{"reason": "test"}); res.status != http.StatusOK && res.status != http.StatusNoContent {
		t.Fatalf("ban: %d %s", res.status, res.body)
	}
	if relay.MayRelay(id) {
		t.Fatal("a banned member keeps the relay")
	}
	if res := owner.do(http.MethodDelete, base+"/ban", nil, nil); res.status >= 300 {
		t.Fatalf("unban: %d %s", res.status, res.body)
	}
	if !relay.MayRelay(id) {
		t.Fatal("the relay stays closed after the ban was lifted")
	}
}
