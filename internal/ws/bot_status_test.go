package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

func TestBotStatusIsShownAsMembersSeeIt(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	watcher := &Client{hub: h, User: auth.User{ID: uuid.New(), Username: "w"}, send: make(chan []byte, 16)}
	h.register(watcher)
	defer h.unregister(watcher)
	for len(watcher.send) > 0 {
		<-watcher.send
	}
	next := func() map[string]any {
		t.Helper()
		select {
		case msg := <-watcher.send:
			var ev struct {
				Type    string         `json:"type"`
				Payload map[string]any `json:"payload"`
			}
			if err := json.Unmarshal(msg, &ev); err != nil || ev.Type != "presence_update" {
				t.Fatalf("event: %s", msg)
			}
			return ev.Payload
		case <-time.After(time.Second):
			t.Fatal("no event")
		}
		return nil
	}
	quiet := func(what string) {
		t.Helper()
		select {
		case msg := <-watcher.send:
			t.Fatalf("%s: %s", what, msg)
		case <-time.After(50 * time.Millisecond):
		}
	}
	bot := uuid.New()
	act := &BotActivity{Type: 0, Name: "chess"}

	// A status set before the bot connects is kept, and announced with it.
	h.SetBotStatus(bot, "dnd", act)
	quiet("announced an offline bot")
	h.SetBotPresence(bot, true)
	if ev := next(); ev["status"] != "dnd" || ev["activity"] == nil {
		t.Fatalf("event %v", ev)
	}
	if h.presenceSnapshot()[bot] != auth.PresenceDND {
		t.Fatal("the snapshot shows the set status")
	}

	h.SetBotStatus(bot, "idle", nil)
	if ev := next(); ev["status"] != auth.PresenceAway || ev["activity"] != nil {
		t.Fatalf("idle: %v", ev)
	}
	h.SetBotStatus(bot, "invisible", act)
	if ev := next(); ev["status"] != StatusOffline || ev["activity"] != nil {
		t.Fatalf("invisible: %v", ev)
	}
	if _, on := h.presenceSnapshot()[bot]; on {
		t.Fatal("an invisible bot is absent from the snapshot")
	}
	h.SetBotStatus(bot, "invisible", nil)
	quiet("the same visible state announced again")

	// Going offline forgets the status.
	h.SetBotPresence(bot, false)
	h.SetBotPresence(bot, true)
	if ev := next(); ev["status"] != auth.PresenceOnline {
		t.Fatalf("status survived a reconnect: %v", ev)
	}
}
