//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestTheServerDefaultFollowsChannelAndCategoryInReadStates(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	cat := newCategory(t, admin, "Music")
	inCat := channelIn(t, admin, "in-cat", cat)
	loose := a.createChannel(admin, "loose", "text")
	own := a.createChannel(admin, "own", "text")
	time.Sleep(10 * time.Millisecond)
	max := a.register(admin, "max")
	for _, ch := range []string{inCat.String(), loose.String(), own.String()} {
		res := admin.post("/api/channels/"+ch+"/messages", map[string]any{"content": "hey @max"})
		wantStatus(t, "post", res, http.StatusCreated)
	}

	server := "/api/notification-settings/server"
	wantStatus(t, "server mentions", max.put(server, map[string]any{"level": "mentions"}), http.StatusNoContent)
	if got := max.readState(t, loose); got.NotifyLevel != "mentions" {
		t.Fatalf("server default not applied: %q", got.NotifyLevel)
	}
	// The category beats the server default, the channel beats both.
	wantStatus(t, "category all", max.put("/api/categories/"+cat.String()+"/notifications", map[string]any{"level": "all"}), http.StatusNoContent)
	wantStatus(t, "channel mute", max.put("/api/channels/"+own.String()+"/notifications", map[string]any{"level": "mute"}), http.StatusNoContent)
	if got := max.readState(t, inCat); got.NotifyLevel != "all" {
		t.Fatalf("category: %q, want all", got.NotifyLevel)
	}
	if got := max.readState(t, loose); got.NotifyLevel != "mentions" {
		t.Fatalf("loose channel: %q, want mentions", got.NotifyLevel)
	}
	if got := max.readState(t, own); got.NotifyLevel != "mute" {
		t.Fatalf("own channel: %q, want mute", got.NotifyLevel)
	}
	// Handing the decision back reaches the server default again.
	wantStatus(t, "channel inherit", max.put("/api/channels/"+own.String()+"/notifications", map[string]any{"level": "inherit"}), http.StatusNoContent)
	if got := max.readState(t, own); got.NotifyLevel != "mentions" {
		t.Fatalf("inherit: %q, want mentions", got.NotifyLevel)
	}
	// A server-wide mute counts only what is addressed to the member.
	wantStatus(t, "server mute", max.put(server, map[string]any{"level": "mute"}), http.StatusNoContent)
	if got := max.readState(t, loose); got.NotifyLevel != "mute" || got.MentionCount != 1 {
		t.Fatalf("server mute: %+v", got)
	}
}

func TestSuppressingEveryoneDropsAllAndHereFromMentionCounts(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "news", "text")
	time.Sleep(10 * time.Millisecond)
	max := a.register(admin, "max")
	postMessage(t, admin, ch, map[string]any{"content": "@all stand-up"})
	postMessage(t, admin, ch, map[string]any{"content": "hey @max"})
	if got := max.readState(t, ch); got.MentionCount != 2 {
		t.Fatalf("before: mentions=%d, want 2", got.MentionCount)
	}
	wantStatus(t, "suppress", max.put("/api/notification-settings/server", map[string]any{"level": "all", "suppress_everyone": true}), http.StatusNoContent)
	if got := max.readState(t, ch); got.MentionCount != 1 || got.UnreadCount != 2 {
		t.Fatalf("suppressed: unread=%d mentions=%d, want 2 and 1", got.UnreadCount, got.MentionCount)
	}
}

func TestServerNotifySettingsRoundTripAndValidation(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	path := "/api/notification-settings/server"
	var s struct {
		Server struct {
			Level            string     `json:"level"`
			MutedUntil       *time.Time `json:"muted_until"`
			SuppressEveryone bool       `json:"suppress_everyone"`
		} `json:"server"`
	}
	admin.get("/api/notification-settings").decode(t, &s)
	if s.Server.Level != "all" || s.Server.SuppressEveryone {
		t.Fatalf("default: %+v", s.Server)
	}
	wantStatus(t, "inherit is no server level", admin.put(path, map[string]any{"level": "inherit"}), http.StatusBadRequest)
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	wantStatus(t, "past mute", admin.put(path, map[string]any{"level": "mute", "muted_until": past}), http.StatusBadRequest)
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	wantStatus(t, "timed", admin.put(path, map[string]any{"level": "mute", "muted_until": until, "suppress_everyone": true}), http.StatusNoContent)
	admin.get("/api/notification-settings").decode(t, &s)
	if s.Server.Level != "mute" || s.Server.MutedUntil == nil || !s.Server.SuppressEveryone {
		t.Fatalf("stored: %+v", s.Server)
	}
	if _, err := a.db.Exec(context.Background(), `UPDATE server_notify SET muted_until = NOW() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	s.Server.MutedUntil = nil
	admin.get("/api/notification-settings").decode(t, &s)
	if s.Server.Level != "all" || s.Server.MutedUntil != nil {
		t.Fatalf("expired mute should read as all: %+v", s.Server)
	}
}
