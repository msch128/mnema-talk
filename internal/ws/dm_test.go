package ws

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A conversation's events reach its members' clients that asked for them, are
// shown to the push listener alone, and never to the Owner, to another member,
// to webhooks or to a client from before conversations.
func TestAConversationsEventsReachItsMembersOnly(t *testing.T) {
	f := perm.NewFixture()
	h := NewHub(nil, nil, nil, nil)
	h.SetPermissions(f.Service())
	t.Cleanup(h.Close)

	alice, bob, carol := auth.User{ID: uuid.New()}, auth.User{ID: uuid.New()}, auth.User{ID: uuid.New()}
	owner := auth.User{ID: uuid.New(), Role: auth.RoleAdmin}
	f.Owner(owner.ID)
	dm := f.DM(alice.ID, bob.ID)

	aliceNew, bobNew, bobOld := testClient(h, alice), testClient(h, bob), testClient(h, bob)
	carolNew, ownerNew := testClient(h, carol), testClient(h, owner)
	for _, c := range []*Client{aliceNew, bobNew, carolNew, ownerNew} {
		c.dmAware = true
	}
	for _, c := range []*Client{aliceNew, bobNew, bobOld, carolNew, ownerNew} {
		drainTypes(t, c)
	}
	var tapped, dmTapped []string
	h.Tap = func(_ uuid.UUID, eventType string, _ any) { tapped = append(tapped, eventType) }
	h.DMTap = func(_ uuid.UUID, eventType string, _ any) { dmTapped = append(dmTapped, eventType) }

	h.BroadcastChannel(dm, "message_create", map[string]any{"x": 1})
	aliceNew.emitApplicationEvent("typing", nil, true, nil, dm)

	if count(drainTypes(t, bobNew), "message_create") != 1 || count(drainTypes(t, aliceNew), "message_create") != 1 {
		t.Fatal("members' current clients receive the message")
	}
	for name, c := range map[string]*Client{"a client from before conversations": bobOld, "another member": carolNew, "the Owner": ownerNew} {
		if got := drainTypes(t, c); len(got) != 0 {
			t.Errorf("%s received %v", name, got)
		}
	}
	if len(tapped) != 0 {
		t.Errorf("webhooks and bots heard a conversation: %v", tapped)
	}
	if len(dmTapped) != 2 {
		t.Errorf("push should hear the message and the typing notice: %v", dmTapped)
	}
}
