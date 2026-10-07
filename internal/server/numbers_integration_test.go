//go:build integration

package server

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"
)

func TestChannelsAndMessagesCarryLinkNumbers(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	general := a.createChannel(admin, "general", "text")
	other := a.createChannel(admin, "other", "text")

	var hierarchy struct {
		Categories []struct {
			Channels []struct {
				ID     uuid.UUID `json:"id"`
				Number int64     `json:"number"`
			} `json:"channels"`
		} `json:"categories"`
		Uncategorized []struct {
			ID     uuid.UUID `json:"id"`
			Number int64     `json:"number"`
		} `json:"uncategorized"`
	}
	admin.get("/api/channels").decode(t, &hierarchy)
	numbers := map[uuid.UUID]int64{}
	for _, c := range hierarchy.Categories {
		for _, ch := range c.Channels {
			numbers[ch.ID] = ch.Number
		}
	}
	for _, ch := range hierarchy.Uncategorized {
		numbers[ch.ID] = ch.Number
	}
	if numbers[general] < 1 || numbers[other] <= numbers[general] {
		t.Fatalf("channel numbers general=%d other=%d, want positive and increasing", numbers[general], numbers[other])
	}

	root := a.send(admin, general, "root")
	reply := admin.post("/api/channels/"+general.String()+"/messages", map[string]any{"content": "reply", "parent_id": root})
	if reply.status != http.StatusCreated {
		t.Fatalf("reply: %d %s", reply.status, reply.body)
	}
	var created struct {
		ID       uuid.UUID  `json:"id"`
		Number   int64      `json:"number"`
		ParentID *uuid.UUID `json:"parent_id"`
	}
	reply.decode(t, &created)

	// The number resolves to the message, with what a link needs to open it.
	var got struct {
		ID        uuid.UUID  `json:"id"`
		Number    int64      `json:"number"`
		ChannelID uuid.UUID  `json:"channel_id"`
		ParentID  *uuid.UUID `json:"parent_id"`
	}
	admin.get("/api/messages/by-number/"+strconv.FormatInt(created.Number, 10)).decode(t, &got)
	if got.ID != created.ID || got.ChannelID != general || got.ParentID == nil || *got.ParentID != root {
		t.Fatalf("by-number: %+v, want reply %s in %s under %s", got, created.ID, general, root)
	}

	// Numbers are never reused: a deleted message's number stays dead.
	if res := admin.delete("/api/channels/" + general.String() + "/messages/" + created.ID.String()); res.status != http.StatusNoContent && res.status != http.StatusOK {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	if res := admin.get("/api/messages/by-number/" + strconv.FormatInt(created.Number, 10)); res.status != http.StatusNotFound {
		t.Errorf("deleted message by number: %d", res.status)
	}
	var next struct {
		Number int64 `json:"number"`
	}
	admin.post("/api/channels/"+general.String()+"/messages", map[string]string{"content": "after"}).decode(t, &next)
	if next.Number <= created.Number {
		t.Errorf("new message number %d, want above the deleted %d", next.Number, created.Number)
	}

	for _, bad := range []string{"0", "-1", "abc", "99999999999999999999"} {
		if res := admin.get("/api/messages/by-number/" + bad); res.status != http.StatusBadRequest {
			t.Errorf("by-number/%s: %d, want 400", bad, res.status)
		}
	}
	if res := admin.get("/api/messages/by-number/999999"); res.status != http.StatusNotFound {
		t.Errorf("unknown number: %d, want 404", res.status)
	}
	if res := a.anon().get("/api/messages/by-number/1"); res.status != http.StatusUnauthorized {
		t.Errorf("anonymous: %d, want 401", res.status)
	}
}
