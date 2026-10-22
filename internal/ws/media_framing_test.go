package ws

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/metrics"
)

// mlsWire is a handshake with the framing of an MLSMessage of the given wire
// format, followed by n opaque bytes.
func mlsWire(format byte, n int) []byte {
	return append([]byte{0, mlsVersion10, 0, format}, make([]byte, n)...)
}

// sizedWire is a heartbeat or capability message ('M' + kind) of n bytes.
func sizedWire(kind byte, n int) []byte {
	b := make([]byte, n)
	b[0], b[1] = 'M', kind
	return b
}

func TestWellFormedHandshakeAcceptsOnlyTheProtocolsMessages(t *testing.T) {
	for name, tc := range map[string]struct {
		wire []byte
		ok   bool
	}{
		"key package":            {mlsWire(mlsKeyPackage, 100), true},
		"welcome":                {mlsWire(mlsWelcome, 100), true},
		"public message":         {mlsWire(mlsPublicMessage, 100), true},
		"heartbeat":              {sizedWire('H', heartbeatBytes), true},
		"capability":             {sizedWire('C', capabilityBytes), true},
		"private message":        {mlsWire(2, 100), false},
		"group info":             {mlsWire(4, 100), false},
		"reserved wire format":   {mlsWire(0, 100), false},
		"unknown wire format":    {mlsWire(9, 100), false},
		"other protocol version": {append([]byte{0, 2, 0, mlsKeyPackage}, 1, 2), false},
		"truncated header":       {[]byte{0, 1, 0}, false},
		"empty":                  {nil, false},
		"short heartbeat":        {sizedWire('H', heartbeatBytes-1), false},
		"long heartbeat":         {sizedWire('H', heartbeatBytes+1), false},
		"short capability":       {sizedWire('C', capabilityBytes-1), false},
		"unknown magic":          {sizedWire('X', 80), false},
		"text":                   {[]byte("hello world"), false},
	} {
		if got := wellFormedHandshake(tc.wire); got != tc.ok {
			t.Errorf("%s: wellFormedHandshake = %v, want %v", name, got, tc.ok)
		}
	}
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
