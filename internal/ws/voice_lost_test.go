package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// A media connection the SFU gave up on must take the member out of the call
// and tell the client, which then rejoins; a stale peer must not.
func TestLostMediaPeerKicksTheSeat(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	t.Cleanup(h.Close)
	c := testClient(h, auth.User{ID: uuid.New()})
	ch := voiceCh()
	h.joinVoice(c, ch)
	current := &sfu.Peer{}
	c.setPeer(current)
	drainTypes(t, c)

	if h.kickLostPeer(c, &sfu.Peer{}) || !inRoom(h, ch.ID, c.User.ID) {
		t.Fatal("a peer the client replaced removed its seat")
	}
	if !h.kickLostPeer(c, current) {
		t.Fatal("the lost peer did not remove the seat")
	}
	if inRoom(h, ch.ID, c.User.ID) || c.currentVoice() != nil {
		t.Fatal("the seat survived the lost media connection")
	}
	raw, ok := nextEventOf(c, "voice_kicked", time.Second)
	if !ok {
		t.Fatal("the client was not told")
	}
	var p struct {
		ChannelID uuid.UUID `json:"channel_id"`
		Reason    string    `json:"reason"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.ChannelID != ch.ID || p.Reason != "connection_lost" {
		t.Fatalf("voice_kicked payload %s (%v)", raw, err)
	}
}
