package ws

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// events drains c and returns the payloads of the given event type.
func events(t *testing.T, c *Client, eventType string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for {
		select {
		case raw := <-c.send:
			var ev struct {
				Type    string         `json:"type"`
				Payload map[string]any `json:"payload"`
			}
			if err := json.Unmarshal(raw, &ev); err != nil {
				t.Fatal(err)
			}
			if ev.Type == eventType {
				out = append(out, ev.Payload)
			}
		default:
			return out
		}
	}
}

func TestMutedMembersAreNeverShownSpeaking(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	chID := uuid.New()
	user := auth.User{ID: uuid.New(), Username: "max"}
	c := &Client{hub: h, User: user, send: make(chan []byte, 64)}
	h.register(c)
	h.mu.Lock()
	h.voice[chID] = map[uuid.UUID]auth.User{user.ID: user}
	c.voiceCh = &chID
	h.mu.Unlock()
	events(t, c, "")

	c.handle("voice_speaking", []byte(`{"active":true}`))
	if got := events(t, c, "voice_speaking"); len(got) != 1 || got[0]["active"] != true {
		t.Fatalf("speaking = %v, want active true", got)
	}

	c.handle("voice_mute_state", []byte(`{"muted":false,"deafened":true}`))
	var types []string
	for len(c.send) > 0 {
		var ev Event
		if err := json.Unmarshal(<-c.send, &ev); err != nil {
			t.Fatal(err)
		}
		types = append(types, ev.Type)
	}
	if len(types) != 2 || types[0] != "voice_mute_state" || types[1] != "voice_speaking" {
		t.Fatalf("events after deafening = %v, want the mute state and speaking stopped", types)
	}
	h.mu.RLock()
	st := h.voiceMute[voiceKey{user.ID, chID}]
	h.mu.RUnlock()
	if !st.Muted || !st.Deafened {
		t.Fatalf("mute state = %+v, want muted and deafened (deafen implies mute)", st)
	}
	if snap := h.voiceSnapshot(); !snap[chID][user.ID].Deafened {
		t.Fatal("snapshot does not carry the deafened state")
	}

	// Already shown as silent: speaking while deafened changes nothing.
	c.handle("voice_speaking", []byte(`{"active":true}`))
	if got := events(t, c, "voice_speaking"); len(got) != 0 {
		t.Fatalf("speaking while deafened = %v, want no event", got)
	}

	c.handle("voice_mute_state", []byte(`{"muted":false,"deafened":false}`))
	c.handle("voice_speaking", []byte(`{"active":true}`))
	if got := events(t, c, "voice_speaking"); len(got) != 1 || got[0]["active"] != true {
		t.Fatalf("speaking after unmute = %v, want active true", got)
	}
}
