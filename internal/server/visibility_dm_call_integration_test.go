//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// H-L6-04 (known failure). doc/talks.md, "Start a Talk from a direct
// message": "No lobby and no admin setup are needed, so it works on a fresh
// community ... the permission snapshot lets only the conversation's members
// see and join it". The call is resolved like a community channel, though
// (perm.Snapshot.channelPerms): a member whose roles give no View channels
// server-wide (a community that hides channels from @everyone and opens them
// by role) starts the call, and then can neither see its chat nor join it.
//
// Inverted: the test passes while the starter is locked out of their own
// call and fails the day they are not.
func TestKnownFailureACallOfAConversationNeedsTheCommunitysViewChannels(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	alice, bob := a.register(owner, "alice"), a.register(owner, "bobby")
	ev := everyoneID(t, owner)
	wantStatus(t, "hide channels from @everyone", owner.patch("/api/admin/roles/"+ev.String(),
		map[string]any{"permissions": perm.DefaultEveryone &^ perm.ViewChannel}), http.StatusOK)

	dm := alice.mustDM(t, bob).ID
	wantStatus(t, "the conversation works", alice.dmPost(dm, "hi"), http.StatusCreated)
	res := alice.post("/api/dms/"+dm.String()+"/talk", nil)
	if res.status != http.StatusCreated && res.status != http.StatusOK {
		t.Fatalf("start a call: %d %s", res.status, res.body)
	}
	var call struct {
		ID uuid.UUID `json:"channel_id"`
	}
	res.decode(t, &call)

	chat := alice.post("/api/channels/"+call.ID.String()+"/messages", map[string]any{"content": "can you hear me"})
	history := bob.get("/api/channels/" + call.ID.String() + "/messages")
	info := myPerms(t, alice)
	if chat.status == http.StatusCreated && history.status == http.StatusOK && channelPerms(info, call.ID).Has(perm.Connect) {
		t.Fatal("H-L6-04 no longer reproduces: members reach their call without View channels. Flip this test into a guard")
	}
	t.Logf("H-L6-04 still reproduces: the starter's chat answers %d, the other member's history %d, Connect %v",
		chat.status, history.status, channelPerms(info, call.ID).Has(perm.Connect))
}
