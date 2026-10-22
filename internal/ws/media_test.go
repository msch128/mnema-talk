package ws

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

func mediaEvents(t *testing.T, c *Client) []map[string]any {
	t.Helper()
	var out []map[string]any
	for len(c.send) > 0 {
		var ev Event
		if err := json.Unmarshal(<-c.send, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type != "media_handshake" {
			continue
		}
		body, ok := ev.Payload.(map[string]any)
		if !ok {
			t.Fatalf("handshake payload %T", ev.Payload)
		}
		out = append(out, body)
	}
	return out
}

func TestMediaHandshakeStaysInsideTheVoiceRoom(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	room, elsewhere := voiceCh(), voiceCh()
	aliceUser := auth.User{ID: uuid.New(), Username: "alice"}
	bobUser := auth.User{ID: uuid.New(), Username: "bob"}
	caraUser := auth.User{ID: uuid.New(), Username: "cara"}
	alice := testClient(h, aliceUser)
	otherTab := testClient(h, aliceUser)
	bob := testClient(h, bobUser)
	outsider := testClient(h, caraUser)
	defer h.unregister(alice)
	defer h.unregister(otherTab)
	defer h.unregister(bob)
	defer h.unregister(outsider)

	h.joinVoice(alice, room)
	h.joinVoice(otherTab, room)
	h.joinVoice(bob, room)
	h.joinVoice(outsider, elsewhere)
	drainTypes(t, alice)
	drainTypes(t, otherTab)
	drainTypes(t, bob)
	drainTypes(t, outsider)

	wire := base64.StdEncoding.EncodeToString(mlsWire(mlsKeyPackage, 2))
	alice.handle("media_handshake", []byte(`{"channel_id":"`+room.ID.String()+`","wire":"`+wire+`"}`))

	if got := mediaEvents(t, alice); len(got) != 0 {
		t.Fatalf("sender received its own handshake: %+v", got)
	}
	if got := mediaEvents(t, outsider); len(got) != 0 {
		t.Fatalf("another room received the handshake: %+v", got)
	}
	for _, member := range []*Client{bob, otherTab} {
		got := mediaEvents(t, member)
		if len(got) != 1 {
			t.Fatalf("room member got %d handshakes", len(got))
		}
		if got[0]["channel_id"] != room.ID.String() || got[0]["user_id"] != aliceUser.ID.String() || got[0]["wire"] != wire {
			t.Fatalf("relayed handshake = %+v", got[0])
		}
	}

	gone := testClient(h, auth.User{ID: uuid.New(), Username: "gone"})
	defer h.unregister(gone)
	raw := make([]byte, maxMediaHandshakeBytes+1)
	oversize := base64.StdEncoding.EncodeToString(raw)
	for _, payload := range []string{
		`{"channel_id":"` + room.ID.String() + `","wire":"@@@@"}`,
		`{"channel_id":"` + room.ID.String() + `","wire":""}`,
		`{"channel_id":"` + room.ID.String() + `","wire":1}`,
		`{"channel_id":"` + elsewhere.ID.String() + `","wire":"` + wire + `"}`,
		`{"channel_id":"` + room.ID.String() + `","wire":"` + oversize + `"}`,
		`not-json`,
	} {
		alice.handle("media_handshake", []byte(payload))
	}
	gone.handle("media_handshake", []byte(`{"channel_id":"`+room.ID.String()+`","wire":"`+wire+`"}`))
	if got := mediaEvents(t, bob); len(got) != 0 {
		t.Fatalf("rejected handshake was relayed: %+v", got)
	}
}
