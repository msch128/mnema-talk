//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

type dupChannel struct {
	ID         uuid.UUID  `json:"id"`
	CategoryID *uuid.UUID `json:"category_id"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Topic      string     `json:"topic"`
	SortOrder  int        `json:"sort_order"`
}

type dupHierarchy struct {
	Categories []struct {
		ID       uuid.UUID    `json:"id"`
		Channels []dupChannel `json:"channels"`
	} `json:"categories"`
	Uncategorized []dupChannel `json:"uncategorized"`
}

func (a *app) hierarchy(c *client) dupHierarchy {
	a.t.Helper()
	res := c.get("/api/channels")
	if res.status != http.StatusOK {
		a.t.Fatalf("list channels: %d %s", res.status, res.body)
	}
	var h dupHierarchy
	res.decode(a.t, &h)
	return h
}

func (a *app) duplicate(c *client, id uuid.UUID) dupChannel {
	a.t.Helper()
	res := c.post("/api/admin/channels/"+id.String()+"/duplicate", nil)
	if res.status != http.StatusCreated {
		a.t.Fatalf("duplicate %s: %d %s", id, res.status, res.body)
	}
	var ch dupChannel
	res.decode(a.t, &ch)
	return ch
}

func channelIDs(chs []dupChannel) []uuid.UUID {
	ids := make([]uuid.UUID, len(chs))
	for i, c := range chs {
		ids[i] = c.ID
	}
	return ids
}

func assertOrder(t *testing.T, what string, got []dupChannel, want ...uuid.UUID) {
	t.Helper()
	ids := channelIDs(got)
	if len(ids) != len(want) {
		t.Fatalf("%s: got %v, want %v", what, ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", what, ids, want)
		}
	}
}

func TestDuplicateChannelInCategory(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")

	var cat struct{ ID uuid.UUID }
	admin.post("/api/admin/categories", map[string]any{"name": "Text", "sort_order": 0}).decode(t, &cat)
	mk := func(name string, order int) uuid.UUID {
		res := admin.post("/api/admin/channels", map[string]any{
			"name": name, "type": "text", "category_id": cat.ID, "sort_order": order, "topic": "Thema " + name,
		})
		if res.status != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, res.status, res.body)
		}
		var ch struct{ ID uuid.UUID }
		res.decode(t, &ch)
		return ch.ID
	}
	one, two, three := mk("eins", 0), mk("zwei", 1), mk("drei", 2)

	// State that belongs to the source and its members must not be copied.
	a.send(admin, two, "hallo")
	if res := max.put("/api/channels/"+two.String()+"/notifications", map[string]any{"level": "mute"}); res.status != http.StatusNoContent && res.status != http.StatusOK {
		t.Fatalf("mute: %d %s", res.status, res.body)
	}

	before := len(a.events.Snapshot())
	cp := a.duplicate(admin, two)
	if cp.ID == two || cp.Name != "zwei" || cp.Type != "text" || cp.Topic != "Thema zwei" ||
		cp.CategoryID == nil || *cp.CategoryID != cat.ID {
		t.Fatalf("copy: %+v", cp)
	}
	events := a.events.Snapshot()[before:]
	if len(events) != 1 || events[0].Type != "channels_changed" || events[0].Recipients != nil {
		t.Fatalf("events after duplicate: %+v", events)
	}

	h := a.hierarchy(max)
	if len(h.Categories) != 1 {
		t.Fatalf("categories: %+v", h.Categories)
	}
	got := h.Categories[0].Channels
	assertOrder(t, "category", got, one, two, cp.ID, three)
	for i, want := range []int{0, 1, 2, 3} {
		if got[i].SortOrder != want {
			t.Fatalf("sort orders: %+v", got)
		}
	}

	// Duplicating the copy again lands directly below the copy.
	cp2 := a.duplicate(admin, cp.ID)
	assertOrder(t, "category after second copy", a.hierarchy(max).Categories[0].Channels, one, two, cp.ID, cp2.ID, three)

	// Duplicating the last channel appends.
	cp3 := a.duplicate(admin, three)
	assertOrder(t, "category after last copy", a.hierarchy(max).Categories[0].Channels, one, two, cp.ID, cp2.ID, three, cp3.ID)

	var msgs []struct{ ID uuid.UUID }
	max.get("/api/channels/"+cp.ID.String()+"/messages").decode(t, &msgs)
	if len(msgs) != 0 {
		t.Fatalf("messages copied: %+v", msgs)
	}
	var states []struct {
		ChannelID   uuid.UUID `json:"channel_id"`
		NotifyLevel string    `json:"notify_level"`
	}
	max.get("/api/read-state").decode(t, &states)
	levels := map[uuid.UUID]string{}
	for _, s := range states {
		levels[s.ChannelID] = s.NotifyLevel
	}
	if levels[two] != "mute" || levels[cp.ID] != "all" {
		t.Fatalf("notify levels: source %q, copy %q", levels[two], levels[cp.ID])
	}
}

func TestDuplicateUncategorizedAndVoiceChannel(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()

	// The helper creates every channel at sort_order 0, so the group is ordered
	// by creation time alone: the copy must still land directly below.
	x := a.createChannel(admin, "x", "text")
	voice := a.createChannel(admin, "Lounge", "voice")
	z := a.createChannel(admin, "z", "text")

	cp := a.duplicate(admin, voice)
	if cp.Type != "voice" || cp.Name != "Lounge" || cp.CategoryID != nil {
		t.Fatalf("voice copy: %+v", cp)
	}
	h := a.hierarchy(admin)
	assertOrder(t, "uncategorized", h.Uncategorized, x, voice, cp.ID, z)

	cpx := a.duplicate(admin, x)
	assertOrder(t, "uncategorized after copying the first", a.hierarchy(admin).Uncategorized, x, cpx.ID, voice, cp.ID, z)
}

func TestDuplicateChannelErrors(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	ch := a.createChannel(admin, "allgemein", "text")
	before := len(a.events.Snapshot())

	if res := max.post("/api/admin/channels/"+ch.String()+"/duplicate", nil); res.status != http.StatusForbidden {
		t.Fatalf("member duplicated a channel: %d %s", res.status, res.body)
	}
	if res := a.anon().post("/api/admin/channels/"+ch.String()+"/duplicate", nil); res.status != http.StatusUnauthorized {
		t.Fatalf("anonymous duplicate: %d %s", res.status, res.body)
	}
	if res := admin.post("/api/admin/channels/"+uuid.NewString()+"/duplicate", nil); res.status != http.StatusNotFound {
		t.Fatalf("unknown channel: %d %s", res.status, res.body)
	}
	if res := admin.post("/api/admin/channels/not-a-uuid/duplicate", nil); res.status != http.StatusBadRequest {
		t.Fatalf("bad id: %d %s", res.status, res.body)
	}
	if res := admin.do(http.MethodPost, "/api/admin/channels/"+ch.String()+"/duplicate", nil,
		map[string]string{"Origin": "https://evil.example"}); res.status != http.StatusForbidden {
		t.Fatalf("cross-origin duplicate: %d %s", res.status, res.body)
	}
	for _, e := range a.events.Snapshot()[before:] {
		if e.Type == "channels_changed" {
			t.Fatalf("rejected duplicate broadcast channels_changed")
		}
	}
	if n := len(a.hierarchy(admin).Uncategorized); n != 1 {
		t.Fatalf("rejected duplicates created channels: %d", n)
	}
}
