package ws

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// Outgoing webhooks hear only what is really published: an event from a
// native connection that is no longer live (revoked, sealed or closed) is not
// published, so it is not tapped either.
func TestTapSkipsPublicationsThatAreNotLive(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	t.Cleanup(h.Close)
	var tapped []string
	h.Tap = func(_ uuid.UUID, eventType string, _ any) { tapped = append(tapped, eventType) }
	ch := uuid.New()

	// A browser connection publishes.
	web := testClient(h, auth.User{ID: uuid.New()})
	tapped = nil
	web.emitApplicationEvent("typing", nil, true, nil, ch)
	if len(tapped) != 1 {
		t.Fatalf("a live publication was not tapped: %v", tapped)
	}

	// A native connection without a live lease does not.
	native := newClient(h, nil, auth.User{ID: uuid.New()}, 0) // not registered
	tapped = nil
	native.native = &nativeSocketState{}
	tapped = nil
	native.emitApplicationEvent("typing", nil, true, nil, ch)
	if len(tapped) != 0 {
		t.Fatalf("a publication that was not live was tapped: %v", tapped)
	}

	// An unencodable payload is published nowhere and tapped nowhere.
	h.Broadcast("bad", make(chan int))
	h.BroadcastChannel(ch, "bad", make(chan int))
	if len(tapped) != 0 {
		t.Fatalf("an unencodable event was tapped: %v", tapped)
	}
	tapped = nil
	h.Broadcast("member_joined", map[string]any{})
	if len(tapped) != 1 {
		t.Fatalf("broadcast was not tapped: %v", tapped)
	}
}
