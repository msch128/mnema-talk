//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// A voice channel has its own text chat (the panel next to the Talk): posting,
// uploading, replying, reacting, editing, reading and searching work exactly
// like in a text channel.
func TestVoiceChannelHasItsOwnTextChat(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	voice := a.createChannel(admin, "Lounge", "voice")
	text := a.createChannel(admin, "allgemein", "text")
	base := "/api/channels/" + voice.String()

	first := postMessage(t, admin, voice, map[string]any{"content": "hallo @max, hörst du mich?"})
	reply := postMessage(t, max, voice, map[string]any{"content": "laut und deutlich", "reply_to_id": first})
	thread := postMessage(t, max, voice, map[string]any{"content": "im Thread", "parent_id": first})

	up := admin.upload(base+"/upload", "file", "talk.png", "image/png", pngBytes, map[string]string{"content": "Screenshot vom Stream"})
	if up.status != http.StatusCreated {
		t.Fatalf("upload in a voice channel: %d %s", up.status, up.body)
	}
	var shot uploaded
	up.decode(t, &shot)
	if len(shot.Attachments) != 1 {
		t.Fatalf("upload without attachment: %s", up.body)
	}

	if res := admin.put(base+"/messages/"+first.String(), map[string]string{"content": "hallo @max, hörst du mich jetzt?"}); res.status != http.StatusOK {
		t.Fatalf("edit in a voice channel: %d %s", res.status, res.body)
	}
	if res := admin.post("/api/messages/"+reply.String()+"/reactions", map[string]string{"emoji": "👍"}); res.status != http.StatusOK && res.status != http.StatusCreated && res.status != http.StatusNoContent {
		t.Fatalf("reaction in a voice channel: %d %s", res.status, res.body)
	}

	// The channel's history: top-level messages only, oldest first.
	res := max.get(base + "/messages")
	if res.status != http.StatusOK {
		t.Fatalf("history: %d %s", res.status, res.body)
	}
	var history []struct {
		ID          uuid.UUID  `json:"id"`
		ChannelID   uuid.UUID  `json:"channel_id"`
		Content     string     `json:"content"`
		ReplyToID   *uuid.UUID `json:"reply_to_id"`
		ReplyCount  int        `json:"reply_count"`
		Attachments []struct{} `json:"attachments"`
		Reactions   []struct {
			Emoji string `json:"emoji"`
			Count int    `json:"count"`
		} `json:"reactions"`
	}
	res.decode(t, &history)
	if len(history) != 3 {
		t.Fatalf("history: %d messages, want 3: %s", len(history), res.body)
	}
	if history[0].ID != first || history[0].Content != "hallo @max, hörst du mich jetzt?" || history[0].ReplyCount != 1 {
		t.Errorf("first message: %+v", history[0])
	}
	if history[1].ID != reply || history[1].ReplyToID == nil || *history[1].ReplyToID != first {
		t.Errorf("reply: %+v", history[1])
	}
	if len(history[1].Reactions) != 1 || history[1].Reactions[0].Count != 1 {
		t.Errorf("reaction not stored: %+v", history[1].Reactions)
	}
	if len(history[2].Attachments) != 1 {
		t.Errorf("upload not in the history: %+v", history[2])
	}
	for _, m := range history {
		if m.ChannelID != voice {
			t.Errorf("message %s in channel %s", m.ID, m.ChannelID)
		}
		if m.ID == thread {
			t.Error("thread reply listed in the channel history")
		}
	}

	// The thread opens like in a text channel; a reply cannot cross into another channel.
	if res := max.get("/api/messages/" + first.String() + "/thread"); res.status != http.StatusOK {
		t.Errorf("thread in a voice channel: %d %s", res.status, res.body)
	}
	if res := max.post("/api/channels/"+text.String()+"/messages", map[string]any{"content": "x", "parent_id": first}); res.status != http.StatusBadRequest {
		t.Errorf("thread reply from another channel: %d", res.status)
	}

	// Search covers the voice channel's chat, also filtered to it.
	if got, status := max.search(t, map[string]string{"q": "deutlich"}); status != http.StatusOK || len(got.Messages) != 1 || got.Messages[0].ChannelID != voice {
		t.Errorf("search across channels: %d %+v", status, got.Messages)
	}
	if got, status := max.search(t, map[string]string{"channel_id": voice.String(), "has": "image"}); status != http.StatusOK || len(got.Messages) != 1 {
		t.Errorf("search in the voice channel for images: %d %+v", status, got.Messages)
	}
}

// Unread and mention counts, read markers and notification levels exist for
// a voice channel's chat like for a text channel.
func TestVoiceChannelChatHasReadState(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	voice := a.createChannel(admin, "Lounge", "voice")

	first := postMessage(t, admin, voice, map[string]any{"content": "wer ist da?"})
	postMessage(t, admin, voice, map[string]any{"content": "@max kommst du?"})
	postMessage(t, max, voice, map[string]any{"content": "gleich"})

	if got := max.readState(t, voice); got.UnreadCount != 2 || got.MentionCount != 1 {
		t.Fatalf("voice unread=%d mentions=%d, want 2 and 1", got.UnreadCount, got.MentionCount)
	}
	if got := admin.readState(t, voice); got.UnreadCount != 1 {
		t.Fatalf("admin voice unread=%d, want 1", got.UnreadCount)
	}

	if res := max.post("/api/channels/"+voice.String()+"/read", nil); res.status != http.StatusNoContent {
		t.Fatalf("mark voice chat read: %d %s", res.status, res.body)
	}
	if got := max.readState(t, voice); got.UnreadCount != 0 || got.MentionCount != 0 {
		t.Fatalf("after read: %+v", got)
	}
	if res := max.post("/api/channels/"+voice.String()+"/unread", map[string]any{"message_id": first}); res.status != http.StatusNoContent {
		t.Fatalf("mark voice chat unread: %d %s", res.status, res.body)
	}
	if got := max.readState(t, voice); got.UnreadCount != 2 {
		t.Fatalf("after mark unread: unread=%d, want 2", got.UnreadCount)
	}
	if res := max.put("/api/channels/"+voice.String()+"/notifications", map[string]string{"level": "mentions"}); res.status != http.StatusNoContent {
		t.Fatalf("voice chat notification level: %d %s", res.status, res.body)
	}
	if got := max.readState(t, voice); got.NotifyLevel != "mentions" {
		t.Fatalf("notify level: %+v", got)
	}

	// Unknown channels still have no read state.
	if res := max.post("/api/channels/"+uuid.New().String()+"/read", nil); res.status != http.StatusNotFound {
		t.Fatalf("read on an unknown channel: %d", res.status)
	}
}
