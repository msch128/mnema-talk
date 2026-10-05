//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

type readState struct {
	ChannelID    uuid.UUID `json:"channel_id"`
	UnreadCount  int       `json:"unread_count"`
	MentionCount int       `json:"mention_count"`
	NotifyLevel  string    `json:"notify_level"`
}

func (c *client) readState(t *testing.T, ch uuid.UUID) readState {
	t.Helper()
	res := c.get("/api/read-state")
	if res.status != http.StatusOK {
		t.Fatalf("read-state: %d %s", res.status, res.body)
	}
	var all []readState
	res.decode(t, &all)
	for _, s := range all {
		if s.ChannelID == ch {
			return s
		}
	}
	return readState{ChannelID: ch, NotifyLevel: "all"}
}

func postMessage(t *testing.T, c *client, ch uuid.UUID, body map[string]any) uuid.UUID {
	t.Helper()
	res := c.post("/api/channels/"+ch.String()+"/messages", body)
	if res.status != http.StatusCreated {
		t.Fatalf("post: %d %s", res.status, res.body)
	}
	var m struct{ ID uuid.UUID }
	res.decode(t, &m)
	return m.ID
}

func TestUnreadAndMentionCounts(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "allgemein", "text")
	postMessage(t, admin, ch, map[string]any{"content": "before max existed"})
	time.Sleep(10 * time.Millisecond)

	max := a.register(admin, "max")
	own := postMessage(t, max, ch, map[string]any{"content": "hallo zusammen"})
	first := postMessage(t, admin, ch, map[string]any{"content": "hi"})
	postMessage(t, admin, ch, map[string]any{"content": "hey @max, schau mal"})
	postMessage(t, admin, ch, map[string]any{"content": "genau", "reply_to_id": own})
	postMessage(t, admin, ch, map[string]any{"content": "@maximilian is someone else"})

	got := max.readState(t, ch)
	if got.UnreadCount != 4 || got.MentionCount != 2 {
		t.Fatalf("unread=%d mentions=%d, want 4 and 2", got.UnreadCount, got.MentionCount)
	}

	if res := max.post("/api/channels/"+ch.String()+"/read", nil); res.status != http.StatusNoContent {
		t.Fatalf("mark read: %d %s", res.status, res.body)
	}
	if got := max.readState(t, ch); got.UnreadCount != 0 || got.MentionCount != 0 {
		t.Fatalf("after read: %+v", got)
	}

	if res := max.post("/api/channels/"+ch.String()+"/unread", map[string]any{"message_id": first}); res.status != http.StatusNoContent {
		t.Fatalf("mark unread: %d %s", res.status, res.body)
	}
	if got := max.readState(t, ch); got.UnreadCount != 4 {
		t.Fatalf("mark unread from %s: unread=%d, want 4", first, got.UnreadCount)
	}

	// The admin's own messages never count for the admin.
	if got := admin.readState(t, ch); got.UnreadCount != 1 {
		t.Fatalf("admin unread=%d, want 1 (max's message)", got.UnreadCount)
	}
}

func TestNotificationLevel(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "musik", "text")
	path := "/api/channels/" + ch.String() + "/notifications"

	if res := admin.put(path, map[string]string{"level": "mute"}); res.status != http.StatusNoContent {
		t.Fatalf("set level: %d %s", res.status, res.body)
	}
	if got := admin.readState(t, ch); got.NotifyLevel != "mute" {
		t.Fatalf("level %q, want mute", got.NotifyLevel)
	}
	if res := admin.put(path, map[string]string{"level": "loud"}); res.status != http.StatusBadRequest {
		t.Fatalf("invalid level accepted: %d", res.status)
	}
	if res := admin.post("/api/channels/"+uuid.NewString()+"/read", nil); res.status != http.StatusNotFound {
		t.Fatalf("unknown channel: %d", res.status)
	}
}
