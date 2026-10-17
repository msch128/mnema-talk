//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A restriction on yourself is not yours to lift, and a bit that is denied to
// you does not count as one you hold.
func TestOverwritesOnYourselfAndDeniedBits(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	mod := a.register(owner, "mod")
	assignRoles(t, owner, mod, createRole(t, owner, "ChanMod", perm.ManageChannels|perm.ViewChannel))
	ch := a.createChannel(owner, "room", "text")
	res := owner.post("/api/admin/categories", map[string]any{"name": "Area"})
	var cat struct{ ID uuid.UUID }
	res.decode(t, &cat)

	// The Owner restricts the moderator in the channel and the category.
	wantStatus(t, "deny in channel", putOverwrites(owner, "channels", ch, overwrite("member", mod.user.ID, 0, perm.SendMessages)), http.StatusNoContent)
	wantStatus(t, "deny in category", putOverwrites(owner, "categories", cat.ID, overwrite("member", mod.user.ID, 0, perm.AddReactions)), http.StatusNoContent)

	// Lifting or replacing the restriction on themselves is refused...
	wantStatus(t, "clear own channel entry", putOverwrites(mod, "channels", ch), http.StatusForbidden)
	wantStatus(t, "allow what is denied", putOverwrites(mod, "channels", ch, overwrite("member", mod.user.ID, perm.SendMessages, 0)), http.StatusForbidden)
	wantStatus(t, "clear own category entry", putOverwrites(mod, "categories", cat.ID), http.StatusForbidden)
	// ...also for the role route: a bit denied to them is not one they hold.
	every := everyoneID(t, owner)
	wantStatus(t, "allow a denied bit to a role in the channel", putOverwrites(mod, "channels", ch,
		overwrite("member", mod.user.ID, 0, perm.SendMessages), overwrite("role", every, perm.SendMessages, 0)), http.StatusForbidden)
	wantStatus(t, "allow a denied bit to a role in the category", putOverwrites(mod, "categories", cat.ID,
		overwrite("member", mod.user.ID, 0, perm.AddReactions), overwrite("role", every, perm.AddReactions, 0)), http.StatusForbidden)
	// Nothing changed.
	var back OverwritesResponse
	owner.get("/api/admin/channels/"+ch.String()+"/overwrites").decode(t, &back)
	if len(back.Overwrites) != 1 || back.Overwrites[0].Deny != perm.SendMessages {
		t.Fatalf("the restriction changed: %+v", back)
	}
	// The Owner may change their moderator's entry.
	wantStatus(t, "owner lifts it", putOverwrites(owner, "channels", ch), http.StatusNoContent)
}
