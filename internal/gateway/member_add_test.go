package gateway

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/wire"
)

func TestANewMemberIsAnnouncedToBotsHoweverTheEventWasPublished(t *testing.T) {
	joined := auth.MemberJoined{ID: uuid.New(), Username: "anna", DisplayName: "Anna"}
	for name, ev := range map[string]events.Event{"as a value": joined, "as a pointer": &joined} {
		m, ok := memberOf(ev)
		if !ok || m.ID != joined.ID || m.Username != "anna" || m.DisplayName != "Anna" {
			t.Errorf("%s: member = %+v, %v", name, m, ok)
		}
	}
	for name, ev := range map[string]events.Event{"without an id": auth.MemberJoined{Username: "anna"}, "another event": wire.Typing{}} {
		if m, ok := memberOf(ev); ok {
			t.Errorf("%s: announced %+v", name, m)
		}
	}
}
