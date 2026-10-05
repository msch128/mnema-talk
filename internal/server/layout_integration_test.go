//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

type layoutResp struct {
	Categories []struct {
		ID       uuid.UUID `json:"id"`
		Name     string    `json:"name"`
		Channels []struct {
			ID    uuid.UUID `json:"id"`
			Name  string    `json:"name"`
			Topic string    `json:"topic"`
		} `json:"channels"`
	} `json:"categories"`
	Uncategorized []struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	} `json:"uncategorized"`
}

func TestAdminRenamesAndReordersChannels(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")

	var catA, catB struct{ ID uuid.UUID }
	admin.post("/api/admin/categories", map[string]any{"name": "Text", "sort_order": 0}).decode(t, &catA)
	admin.post("/api/admin/categories", map[string]any{"name": "Spiele", "sort_order": 1}).decode(t, &catB)
	one := a.createChannel(admin, "eins", "text")
	two := a.createChannel(admin, "zwei", "text")

	if res := max.patch("/api/admin/channels/"+one.String(), map[string]any{"name": "x"}); res.status != http.StatusForbidden {
		t.Fatalf("member renamed a channel: %d", res.status)
	}
	if res := admin.patch("/api/admin/channels/"+one.String(), map[string]any{"name": "allgemein", "topic": "Alles Mögliche"}); res.status != http.StatusOK {
		t.Fatalf("rename channel: %d %s", res.status, res.body)
	}
	if res := admin.patch("/api/admin/categories/"+catB.ID.String(), map[string]any{"name": "Games"}); res.status != http.StatusOK {
		t.Fatalf("rename category: %d %s", res.status, res.body)
	}
	if res := admin.patch("/api/admin/channels/"+one.String(), map[string]any{"name": "   "}); res.status != http.StatusBadRequest {
		t.Fatalf("empty name accepted: %d", res.status)
	}

	// Games first; zwei moves into Games, allgemein into Text.
	layout := map[string]any{
		"categories": []map[string]any{{"id": catB.ID, "sort_order": 0}, {"id": catA.ID, "sort_order": 1}},
		"channels": []map[string]any{
			{"id": two, "category_id": catB.ID, "sort_order": 0},
			{"id": one, "category_id": catA.ID, "sort_order": 0},
		},
	}
	if res := admin.put("/api/admin/layout", layout); res.status != http.StatusNoContent {
		t.Fatalf("reorder: %d %s", res.status, res.body)
	}
	var got layoutResp
	max.get("/api/channels").decode(t, &got)
	if len(got.Categories) != 2 || got.Categories[0].Name != "Games" || got.Categories[1].Name != "Text" {
		t.Fatalf("category order: %+v", got.Categories)
	}
	if len(got.Categories[0].Channels) != 1 || got.Categories[0].Channels[0].ID != two {
		t.Fatalf("zwei not in Games: %+v", got.Categories[0].Channels)
	}
	if c := got.Categories[1].Channels; len(c) != 1 || c[0].Name != "allgemein" || c[0].Topic != "Alles Mögliche" {
		t.Fatalf("allgemein not renamed/moved: %+v", c)
	}

	bad := map[string]any{"channels": []map[string]any{{"id": uuid.New(), "sort_order": 0}}}
	if res := admin.put("/api/admin/layout", bad); res.status != http.StatusNotFound {
		t.Fatalf("unknown channel in layout: %d", res.status)
	}
}
