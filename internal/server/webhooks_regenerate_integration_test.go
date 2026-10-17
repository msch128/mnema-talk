//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/webhook"
)

// A new token is a key to post with the creator's rights, so only someone who
// could have created the webhook may get one, and they become its creator.
func TestRegeneratingATokenTransfersTheWebhookToTheCaller(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	a.router.Webhooks.SetLimits(1000, 1000)
	h := makeHook(t, admin, ch, "owner's")
	everyone := everyoneID(t, admin)

	role := createRole(t, admin, "hooks", perm.ManageWebhooks)
	mod := a.register(admin, "mod")
	assignRoles(t, admin, mod, role)
	// Make the Owner's webhook the mod's to manage: peers of the Owner's
	// hook cannot touch it, so the mod gets a webhook of their own first.
	own := makeHook(t, mod, ch, "mods")
	regen := "/api/admin/webhooks/" + own.ID.String() + "/token"

	// Without Send messages the mod gets no new URL (and the old one stops
	// working only if the call succeeded).
	if res := putOverwrites(admin, "channels", ch, overwrite("role", everyone, 0, perm.SendMessages)); res.status != 204 {
		t.Fatalf("overwrite: %d %s", res.status, res.body)
	}
	if res := mod.post(regen, nil); res.status != http.StatusForbidden {
		t.Fatalf("regenerate without Send messages: %d %s", res.status, res.body)
	}
	// The Owner's own regeneration of the mod's webhook still works and
	// makes the Owner its creator; the mod's rights no longer matter to it.
	var fresh webhook.CreatedWebhook
	res := admin.post(regen, nil)
	res.decode(t, &fresh)
	if res.status != http.StatusOK {
		t.Fatalf("owner regenerates: %d %s", res.status, res.body)
	}
	if fresh.CreatedBy == nil || *fresh.CreatedBy != admin.user.ID {
		t.Fatalf("creator after regeneration: %+v", fresh.CreatedBy)
	}
	if r := a.exec("/api/webhooks/"+own.ID.String()+"/"+fresh.Token, map[string]any{"content": "ok"}, nil); r.status != http.StatusNoContent {
		t.Fatalf("post with the Owner's rights: %d %s", r.status, r.body)
	}

	// With Send messages back, the mod may regenerate and then holds the
	// webhook: mass mentions are checked against the mod, who lacks them.
	if res := putOverwrites(admin, "channels", ch, overwrite("role", everyone, 0, perm.MentionEveryone)); res.status != 204 {
		t.Fatalf("overwrite: %d %s", res.status, res.body)
	}
	own2 := makeHook(t, mod, ch, "mods again")
	var fresh2 webhook.CreatedWebhook
	if r := mod.post("/api/admin/webhooks/"+own2.ID.String()+"/token", nil); r.status != http.StatusOK {
		t.Fatalf("mod regenerates: %d %s", r.status, r.body)
	} else {
		r.decode(t, &fresh2)
	}
	if fresh2.CreatedBy == nil || *fresh2.CreatedBy != mod.user.ID {
		t.Fatalf("creator after regeneration: %+v", fresh2.CreatedBy)
	}
	r := a.exec("/api/webhooks/"+own2.ID.String()+"/"+fresh2.Token+"?wait=true", map[string]any{"content": "@all", "allowed_mentions": map[string]any{"parse": []string{"everyone"}}}, nil)
	var m chat.Message
	r.decode(t, &m)
	if r.status != 200 || len(m.Mentions) != 0 {
		t.Fatalf("mass mention by a creator without the right: %d %s", r.status, r.body)
	}
	_ = h
}
