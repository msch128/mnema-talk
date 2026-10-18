package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

func TestBotPresenceIsOnlineWithoutBeingCounted(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	watcher := &Client{hub: h, User: auth.User{ID: uuid.New(), Username: "w"}, send: make(chan []byte, 16)}
	h.register(watcher)
	defer h.unregister(watcher)
	for len(watcher.send) > 0 {
		<-watcher.send
	}
	bot := uuid.New()

	h.SetBotPresence(bot, false) // already offline: nothing to announce
	select {
	case msg := <-watcher.send:
		t.Fatalf("announced no change: %s", msg)
	case <-time.After(50 * time.Millisecond):
	}

	h.SetBotPresence(bot, true)
	var ev Event
	select {
	case msg := <-watcher.send:
		if err := json.Unmarshal(msg, &ev); err != nil || ev.Type != "presence_update" {
			t.Fatalf("event: %s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no presence update")
	}
	if h.presenceSnapshot()[bot] != auth.PresenceOnline {
		t.Fatal("a connected bot is online in the snapshot")
	}
	if isUserInSlice(h.OnlineUserIDs(), bot) || h.OnlineCount() != 1 {
		t.Fatal("bots are not counted as people (@here, online count)")
	}

	h.SetBotPresence(bot, true) // again: no second announcement
	h.SetBotPresence(bot, false)
	if _, on := h.presenceSnapshot()[bot]; on {
		t.Fatal("a bot without a connection is offline")
	}
}
