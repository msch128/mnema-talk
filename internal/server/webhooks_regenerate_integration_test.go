//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/webhook"
)

// Regenerating a token changes the token and nothing else: not the settings,
// not the record of who made the webhook.
func TestRegeneratingATokenChangesOnlyTheToken(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	a.router.Webhooks.SetLimits(1000, 1000)
	role := createRole(t, admin, "admins", perm.Administrator)
	boss := a.register(admin, "boss")
	assignRoles(t, admin, boss, role)

	var h hook
	res := admin.post("/api/admin/channels/"+ch.String()+"/webhooks", map[string]any{"name": "owner's", "allow_everyone": true, "allow_embeds": false})
	res.decode(t, &h.CreatedWebhook)
	h.path = "/api/webhooks/" + h.ID.String() + "/" + h.Token

	var fresh webhook.CreatedWebhook
	res = boss.post("/api/admin/webhooks/"+h.ID.String()+"/token", nil)
	res.decode(t, &fresh)
	if res.status != http.StatusOK || fresh.Token == h.Token {
		t.Fatalf("regenerate: %d %s", res.status, res.body)
	}
	if fresh.CreatedBy == nil || *fresh.CreatedBy != admin.user.ID {
		t.Fatalf("unexpected creator: %v", fresh.CreatedBy)
	}
	if fresh.Name != "owner's" {
		t.Fatalf("unexpected name: %q", fresh.Name)
	}
	if !fresh.AllowEveryone || fresh.AllowEmbeds || !fresh.AllowAttachments {
		t.Fatalf("regenerating changed permissions: %+v", fresh.Webhook)
	}
	if r := a.exec(h.path, map[string]any{"content": "x"}, nil); r.status != http.StatusUnauthorized {
		t.Fatalf("old URL: %d", r.status)
	}
	if r := a.exec("/api/webhooks/"+h.ID.String()+"/"+fresh.Token, map[string]any{"content": "x"}, nil); r.status != http.StatusNoContent {
		t.Fatalf("new URL: %d", r.status)
	}
}
