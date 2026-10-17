package ws

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
)

// A hub without a permission model (tests of unrelated features) limits nothing.
func TestHubWithoutPermissionsLimitsNothing(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	t.Cleanup(h.Close)
	a := testClient(h, auth.User{ID: uuid.New()})
	drainTypes(t, a)
	ch := uuid.New()
	h.BroadcastChannel(ch, "message_create", nil)
	h.PermissionsChanged(nil)
	got := drainTypes(t, a)
	if count(got, "message_create") != 1 || count(got, "permissions_changed") != 1 {
		t.Fatalf("events: %v", got)
	}
	h.joinVoice(a, &chat.ChannelInfo{ID: ch, Type: chat.ChannelTypeVoice})
	if !inRoom(h, ch, a.User.ID) {
		t.Fatal("joining needs no permission without a model")
	}
	h.SetPermissions(nil) // clearing is allowed
}
