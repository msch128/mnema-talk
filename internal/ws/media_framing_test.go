package ws

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/metrics"
)

// The wire constants of the handshake framing (see package mediaframe).
const (
	mlsVersion10  = 1
	mlsWelcome    = 3
	mlsKeyPackage = 5
)

// mlsWire is a handshake with the framing of an MLSMessage of the given wire
// format, followed by n opaque bytes.
func mlsWire(format byte, n int) []byte {
	return append([]byte{0, mlsVersion10, 0, format}, make([]byte, n)...)
}

// The relay refuses blobs that are not shaped like any handshake message and
// counts them as rejected, while the content of a well-framed message stays
// untouched.
func TestMediaHandshakeRelayRefusesUnframedBlobs(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	room := voiceCh()
	alice, bob := testClient(h, auth.User{ID: uuid.New()}), testClient(h, auth.User{ID: uuid.New()})
	defer h.unregister(alice)
	defer h.unregister(bob)
	h.joinVoice(alice, room)
	h.joinVoice(bob, room)
	drainTypes(t, bob)
	send := func(raw []byte) {
		body, _ := json.Marshal(map[string]any{"channel_id": room.ID, "wire": base64.StdEncoding.EncodeToString(raw)})
		alice.handleMediaHandshake(body)
	}
	rejected := metrics.MLSHandshakes.With("rejected").Load()
	send(make([]byte, 20000))
	send([]byte("not mls at all"))
	send(mlsWire(2, 50))
	if got := metrics.MLSHandshakes.With("rejected").Load() - rejected; got != 3 {
		t.Fatalf("rejected = %d, want 3", got)
	}
	if got := mediaEvents(t, bob); len(got) != 0 {
		t.Fatalf("unframed blobs were relayed: %+v", got)
	}
	good := mlsWire(mlsKeyPackage, 300)
	send(good)
	got := mediaEvents(t, bob)
	if len(got) != 1 || got[0]["wire"] != base64.StdEncoding.EncodeToString(good) {
		t.Fatalf("well-framed message not relayed unchanged: %+v", got)
	}
}
