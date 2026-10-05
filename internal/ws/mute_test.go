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

	c.handle("voice_mute_state", []byte(`{"muted":false,"deafened":true}`))
	if got := events(t, c, "voice_mute_state"); len(got) != 1 || got[0]["muted"] != true || got[0]["deafened"] != true {
		t.Fatalf("mute state = %v, want muted and deafened (deafen implies mute)", got)
	}
	if snap := h.voiceSnapshot(); !snap[chID][user.ID].Deafened {
		t.Fatal("snapshot does not carry the deafened state")
	}

	c.handle("voice_speaking", []byte(`{"active":true}`))
	if got := events(t, c, "voice_speaking"); len(got) != 1 || got[0]["active"] != false {
		t.Fatalf("speaking while deafened = %v, want active false", got)
	}

	c.handle("voice_mute_state", []byte(`{"muted":false,"deafened":false}`))
	c.handle("voice_speaking", []byte(`{"active":true}`))
	if got := events(t, c, "voice_speaking"); len(got) != 1 || got[0]["active"] != true {
		t.Fatalf("speaking after unmute = %v, want active true", got)
	}
}
