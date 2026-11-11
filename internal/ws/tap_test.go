package ws

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/eventstest"
)

// Outgoing webhooks hear only what is really published: a publication is
// tapped once, and an event that cannot be encoded is tapped nowhere.
func TestTapHearsOnlyRealPublications(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	var tapped []string
	h.Tap = func(_ uuid.UUID, ev events.Event) {
		eventType := ev.EventName()
		tapped = append(tapped, eventType)
	}
	ch := uuid.New()

	// A browser connection publishes.
	web := testClient(h, auth.User{ID: uuid.New()})
	tapped = nil
	web.emitApplicationEvent(eventstest.Named("typing", nil), true, nil, ch)
	if len(tapped) != 1 {
		t.Fatalf("a live publication was not tapped: %v", tapped)
	}

	// An unencodable payload is published nowhere and tapped nowhere.
	tapped = nil
	h.Broadcast(eventstest.Named("bad", make(chan int)))
	h.BroadcastChannel(ch, eventstest.Named("bad", make(chan int)))
	if len(tapped) != 0 {
		t.Fatalf("an unencodable event was tapped: %v", tapped)
	}
	tapped = nil
	h.Broadcast(eventstest.Named("member_joined", map[string]any{}))
	if len(tapped) != 1 {
		t.Fatalf("broadcast was not tapped: %v", tapped)
	}
}
