//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Someone who sees a category but not every channel in it cannot change its
// overwrites or delete it: that could open the channels they cannot see.
func TestCategoryChangesNeedSightOfEveryChannelInIt(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	builder := a.register(owner, "builder")
	role := createRole(t, owner, "Builder", perm.ManageChannels)
	assignRoles(t, owner, builder, role)
	every := everyoneID(t, owner)

	res := owner.post("/api/admin/categories", map[string]any{"name": "Area"})
	var cat struct{ ID uuid.UUID }
	res.decode(t, &cat)
	wantStatus(t, "category deny", putOverwrites(owner, "categories", cat.ID, overwrite("role", every, 0, perm.SendMessages)), http.StatusNoContent)
	for _, name := range []string{"visible", "secret"} {
		wantStatus(t, name, owner.post("/api/admin/channels", map[string]any{"name": name, "category_id": cat.ID}), http.StatusCreated)
	}
	var h struct {
		Categories []struct {
			ID       uuid.UUID
			Channels []struct {
				ID   uuid.UUID
				Name string
			}
		}
	}
	owner.get("/api/channels").decode(t, &h)
	var secret uuid.UUID
	for _, c := range h.Categories {
		for _, ch := range c.Channels {
			if ch.Name == "secret" {
				secret = ch.ID
			}
		}
	}

	// While the builder sees everything in the category they may manage it.
	wantStatus(t, "sees all", putOverwrites(builder, "categories", cat.ID, overwrite("role", every, 0, perm.SendMessages)), http.StatusNoContent)

	wantStatus(t, "hide the secret channel", putOverwrites(owner, "channels", secret, overwrite("member", builder.user.ID, 0, perm.ViewChannel)), http.StatusNoContent)
	wantStatus(t, "edit the category", putOverwrites(builder, "categories", cat.ID), http.StatusNotFound)
	wantStatus(t, "read the category", builder.get("/api/admin/categories/"+cat.ID.String()+"/overwrites"), http.StatusOK)
	wantStatus(t, "delete the category", builder.delete("/api/admin/categories/"+cat.ID.String()), http.StatusNotFound)
	// Nothing changed, and the Owner still can.
	var back OverwritesResponse
	owner.get("/api/admin/categories/"+cat.ID.String()+"/overwrites").decode(t, &back)
	if len(back.Overwrites) != 1 {
		t.Fatalf("category overwrites changed: %+v", back)
	}
	// Category and channel both carry overwrites: folding them into one layer
	// would not be exact, so the deletion is refused until one side is clear.
	wantStatus(t, "inexact fold", owner.delete("/api/admin/categories/"+cat.ID.String()), http.StatusConflict)
	wantStatus(t, "clear the channel", putOverwrites(owner, "channels", secret), http.StatusNoContent)
	wantStatus(t, "owner deletes", owner.delete("/api/admin/categories/"+cat.ID.String()), http.StatusNoContent)
}
