//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/webhook"
)

// Regenerating a token changes the token and nothing else: not who made the
// webhook, not what it may mention.
func TestRegeneratingATokenChangesOnlyTheToken(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	a.router.Webhooks.SetLimits(1000, 1000)
	role := createRole(t, admin, "hooks", perm.ManageWebhooks)
	mod := a.register(admin, "mod")
	assignRoles(t, admin, mod, role)

	// The Owner's webhook may mention everyone (the Owner holds the right).
	h := makeHook(t, admin, ch, "owner's")
	post := func(path string) chat.Message {
		t.Helper()
		res := a.exec(path+"?wait=true", map[string]any{"content": "@all", "allowed_mentions": map[string]any{"parse": []string{"everyone"}}}, nil)
		var m chat.Message
		res.decode(t, &m)
		if res.status != http.StatusOK {
			t.Fatalf("post: %d %s", res.status, res.body)
		}
		return m
	}
	if len(post(h.path).Mentions) == 0 {
		t.Fatal("the Owner's webhook cannot mention everyone")
	}

	// A member without Mention everyone regenerates it: a new URL, the old one
	// dead, the same creator, the same abilities.
	if res := putOverwrites(admin, "channels", ch, overwrite("role", everyoneID(t, admin), 0, perm.MentionEveryone|perm.SendMessages)); res.status != 204 {
		t.Fatalf("overwrite: %d %s", res.status, res.body)
	}
	var fresh webhook.CreatedWebhook
	res := mod.post("/api/admin/webhooks/"+h.ID.String()+"/token", nil)
	res.decode(t, &fresh)
	if res.status != http.StatusOK || fresh.Token == h.Token {
		t.Fatalf("regenerate: %d %s", res.status, res.body)
	}
	if fresh.CreatedBy == nil || *fresh.CreatedBy != admin.user.ID {
		t.Fatalf("regenerating moved the webhook: %+v", fresh.CreatedBy)
	}
	if r := a.exec(h.path, map[string]any{"content": "x"}, nil); r.status != http.StatusUnauthorized {
		t.Fatalf("old URL: %d", r.status)
	}
	if len(post("/api/webhooks/"+h.ID.String()+"/"+fresh.Token).Mentions) == 0 {
		t.Fatal("regenerating changed what the webhook may do")
	}
}
