package ws

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// presenceUpdates drains c and returns the statuses of presence_update events for userID.
func presenceUpdates(t *testing.T, c *Client, userID uuid.UUID) []string {
	t.Helper()
	var out []string
	for {
		select {
		case raw := <-c.send:
			var ev struct {
				Type    string `json:"type"`
				Payload struct {
					UserID uuid.UUID `json:"user_id"`
					Status string    `json:"status"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(raw, &ev); err != nil {
				t.Fatal(err)
			}
			if ev.Type == "presence_update" && ev.Payload.UserID == userID {
				out = append(out, ev.Payload.Status)
			}
		default:
			return out
		}
	}
}

func TestPresenceFollowsChoiceAndIdle(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	watcher := &Client{hub: h, User: auth.User{ID: uuid.New()}, send: make(chan []byte, 64)}
	h.register(watcher)
	defer h.unregister(watcher)

	user := auth.User{ID: uuid.New(), Presence: auth.PresenceDND}
	c := &Client{hub: h, User: user, send: make(chan []byte, 64)}
	h.register(c)
	if got := presenceUpdates(t, watcher, user.ID); len(got) != 1 || got[0] != auth.PresenceDND {
		t.Fatalf("on connect: got %v, want [dnd]", got)
	}
	if snap := h.presenceSnapshot(); snap[user.ID] != auth.PresenceDND {
		t.Fatalf("snapshot = %q, want dnd", snap[user.ID])
	}

	// Idle never overrides a deliberate choice.
	h.setIdle(c, true)
	if got := presenceUpdates(t, watcher, user.ID); len(got) != 0 {
		t.Fatalf("idle while dnd announced %v", got)
	}

	// Back to online while idle reads as away; activity makes it online.
	h.SetPresence(user.ID, auth.PresenceOnline)
	h.setIdle(c, false)
	if got := presenceUpdates(t, watcher, user.ID); len(got) != 2 || got[0] != auth.PresenceAway || got[1] != auth.PresenceOnline {
		t.Fatalf("got %v, want [away online]", got)
	}

	// A second, active connection keeps the user online while the first idles.
	c2 := &Client{hub: h, User: auth.User{ID: user.ID, Presence: auth.PresenceOnline}, send: make(chan []byte, 64)}
	h.register(c2)
	h.setIdle(c, true)
	if got := presenceUpdates(t, watcher, user.ID); len(got) != 0 {
		t.Fatalf("one active connection left, got %v", got)
	}
	h.setIdle(c2, true)
	if got := presenceUpdates(t, watcher, user.ID); len(got) != 1 || got[0] != auth.PresenceAway {
		t.Fatalf("all idle: got %v, want [away]", got)
	}

	h.unregister(c)
	h.unregister(c2)
	if got := presenceUpdates(t, watcher, user.ID); len(got) != 1 || got[0] != StatusOffline {
		t.Fatalf("after disconnect: got %v, want [offline]", got)
	}
	if _, ok := h.presenceSnapshot()[user.ID]; ok {
		t.Fatal("offline user must not be in the snapshot")
	}
}

func TestSetPresenceIgnoresInvalidAndOffline(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	id := uuid.New()
	h.SetPresence(id, auth.PresenceFocus) // not connected: nothing to apply
	h.SetPresence(id, "offline")          // never choosable
	if len(h.presenceSnapshot()) != 0 {
		t.Fatal("snapshot must stay empty")
	}
}
