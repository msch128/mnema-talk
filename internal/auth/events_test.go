package auth

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/wire"
)

func TestAProfileEventIsReadAsAValueAndAsAPointer(t *testing.T) {
	u := User{ID: uuid.New(), Username: "anna"}
	update, joined := UserUpdate(u), MemberJoined(u)
	for name, ev := range map[string]events.Event{"value": update, "pointer": &update} {
		if got, ok := UserUpdateOf(ev); !ok || got.ID != u.ID {
			t.Errorf("user_update as a %s: %+v, %v", name, got, ok)
		}
		if _, ok := MemberJoinedOf(ev); ok {
			t.Errorf("user_update as a %s was read as member_joined", name)
		}
	}
	for name, ev := range map[string]events.Event{"value": joined, "pointer": &joined} {
		if got, ok := MemberJoinedOf(ev); !ok || got.ID != u.ID {
			t.Errorf("member_joined as a %s: %+v, %v", name, got, ok)
		}
		if _, ok := UserUpdateOf(ev); ok {
			t.Errorf("member_joined as a %s was read as user_update", name)
		}
	}
	// The short form of user_update carries no profile.
	if _, ok := UserUpdateOf(wire.UserDisabled{ID: u.ID, Disabled: true}); ok {
		t.Error("a disable notice was read as a profile")
	}
}
