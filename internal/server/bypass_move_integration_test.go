//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Moving a channel out of a restricted category would drop the restriction it
// inherits; only an Administrator may do that, while plain layout work stays
// open to Manage channels.
func TestMovingChannelsCannotLiftInheritedRestrictions(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	builder := a.register(owner, "builder")
	assignRoles(t, owner, builder, createRole(t, owner, "Builder", perm.ManageChannels))

	category := func(name string) uuid.UUID {
		res := owner.post("/api/admin/categories", map[string]any{"name": name})
		wantStatus(t, "category", res, http.StatusCreated)
		var c struct{ ID uuid.UUID }
		res.decode(t, &c)
		return c.ID
	}
	private, plain, other := category("Private"), category("Plain"), category("Other")
	every := everyoneID(t, owner)
	wantStatus(t, "restrict", putOverwrites(owner, "categories", private, overwrite("role", every, 0, perm.SendMessages)), http.StatusNoContent)
	res := owner.post("/api/admin/channels", map[string]any{"name": "inside", "category_id": private})
	wantStatus(t, "channel", res, http.StatusCreated)
	var inside struct{ ID uuid.UUID }
	res.decode(t, &inside)
	res = owner.post("/api/admin/channels", map[string]any{"name": "free", "category_id": plain})
	wantStatus(t, "channel", res, http.StatusCreated)
	var free struct{ ID uuid.UUID }
	res.decode(t, &free)

	move := func(c *client, ch, to uuid.UUID) response {
		return c.put("/api/admin/layout", map[string]any{"channels": []map[string]any{{"id": ch, "category_id": to, "sort_order": 1}}})
	}
	wantStatus(t, "out of the restricted category", move(builder, inside.ID, other), http.StatusForbidden)
	wantStatus(t, "into the restricted category", move(builder, free.ID, private), http.StatusForbidden)
	wantStatus(t, "between plain categories", move(builder, free.ID, other), http.StatusNoContent)
	wantStatus(t, "same category, new order", move(builder, inside.ID, private), http.StatusNoContent)
	wantStatus(t, "the Owner may", move(owner, inside.ID, other), http.StatusNoContent)
}
