//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// The Owner's words are only the Owner's to remove, whatever a moderator holds.
func TestModeratorsCannotDeleteTheOwnersMessages(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	mod := a.register(owner, "mod")
	max := a.register(owner, "max")
	ch := a.createChannel(owner, "talk", "text")
	assignRoles(t, owner, mod, createRole(t, owner, "Mod", perm.ManageMessages))

	base := "/api/channels/" + ch.String() + "/messages/"
	ownerMsg := postMessage(t, owner, ch, map[string]any{"content": "from the Owner"})
	wantStatus(t, "owner's message", mod.delete(base+ownerMsg.String()), http.StatusForbidden)
	// A member's thread that the Owner replied in is protected as well.
	root := postMessage(t, max, ch, map[string]any{"content": "a question"})
	postMessage(t, owner, ch, map[string]any{"content": "an answer", "parent_id": root})
	wantStatus(t, "thread with an Owner reply", mod.delete(base+root.String()), http.StatusForbidden)
	// Ordinary moderation still works, and the author and the Owner may delete.
	plain := postMessage(t, max, ch, map[string]any{"content": "spam"})
	wantStatus(t, "member's message", mod.delete(base+plain.String()), http.StatusNoContent)
	wantStatus(t, "the author", max.delete(base+root.String()), http.StatusNoContent)
	wantStatus(t, "the Owner", owner.delete(base+ownerMsg.String()), http.StatusNoContent)
}

// Member totals count only what the viewer can see.
func TestMessageCountsHideHiddenChannels(t *testing.T) {
	f := newAccessFixture(t, false)
	secret := f.privateChannel(t, "staff-only", "text")
	open := f.a.createChannel(f.owner, "open", "text")
	for range 3 {
		postMessage(t, f.insider, secret, map[string]any{"content": "hidden"})
	}
	postMessage(t, f.insider, open, map[string]any{"content": "visible"})

	count := func(c *client) int64 {
		var members []struct {
			ID           uuid.UUID
			MessageCount int64 `json:"message_count"`
		}
		c.get("/api/members").decode(t, &members)
		for _, m := range members {
			if m.ID == f.insider.user.ID {
				return m.MessageCount
			}
		}
		t.Fatal("member missing")
		return 0
	}
	if got := count(f.outsider); got != 1 {
		t.Fatalf("outsider sees %d messages, want 1", got)
	}
	if got := count(f.insider); got != 4 {
		t.Fatalf("insider sees %d messages, want 4", got)
	}
	if got := count(f.owner); got != 4 {
		t.Fatalf("owner sees %d messages, want 4", got)
	}
}

// Deleting a category with permissions, and a category change that reaches
// channels the editor holds less in, are not for plain channel managers.
func TestCategoryDeletionAndHeldBitsAcrossItsChannels(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	mod := a.register(owner, "mod")
	assignRoles(t, owner, mod, createRole(t, owner, "Mod", perm.ManageChannels|perm.ManageMessages|perm.ViewChannel))
	every := everyoneID(t, owner)

	res := owner.post("/api/admin/categories", map[string]any{"name": "Area"})
	var cat struct{ ID uuid.UUID }
	res.decode(t, &cat)
	res = owner.post("/api/admin/channels", map[string]any{"name": "inside", "category_id": cat.ID})
	var inside struct{ ID uuid.UUID }
	res.decode(t, &inside)
	// The moderator may moderate messages in general, but not in this channel.
	wantStatus(t, "channel deny", putOverwrites(owner, "channels", inside.ID, overwrite("member", mod.user.ID, 0, perm.ManageMessages)), http.StatusNoContent)

	wantStatus(t, "allow what a channel inside denies you", putOverwrites(mod, "categories", cat.ID, overwrite("role", every, perm.ManageMessages, 0)), http.StatusForbidden)
	wantStatus(t, "deny it too", putOverwrites(mod, "categories", cat.ID, overwrite("role", every, 0, perm.ManageMessages)), http.StatusForbidden)
	wantStatus(t, "something they hold everywhere", putOverwrites(mod, "categories", cat.ID, overwrite("role", every, 0, perm.AddReactions)), http.StatusNoContent)

	// The category now has overwrites: deleting it is an Administrator's job.
	wantStatus(t, "clear the channel", putOverwrites(owner, "channels", inside.ID), http.StatusNoContent)
	wantStatus(t, "plain manager deletes", mod.delete("/api/admin/categories/"+cat.ID.String()), http.StatusForbidden)
	wantStatus(t, "owner deletes", owner.delete("/api/admin/categories/"+cat.ID.String()), http.StatusNoContent)
}
