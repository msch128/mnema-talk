//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/dm"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

// H-L6-04 (guard; was a known failure). doc/talks.md, "Start a Talk from a
// direct message": "No lobby and no admin setup are needed, so it works on a
// fresh community ... the permission snapshot lets only the conversation's
// members see and join it, the Owner and Administrators included". The call
// used to be resolved like a community channel: in a community that hides
// channels from @everyone (and opens them by role), a member without such a
// role started the call and then could neither see its chat nor join it.
// Membership now decides; no community right reaches the call from outside.
func TestACallOfAConversationNeedsNoViewChannelsAndNoCommunityRightOpensIt(t *testing.T) {
	a := newApp(t, true)
	owner := a.seedAdmin()
	alice, bob := a.register(owner, "alice"), a.register(owner, "bobby")
	viewer, mod, admin := a.register(owner, "viewer"), a.register(owner, "moder"), a.register(owner, "admins")
	ev := everyoneID(t, owner)
	wantStatus(t, "hide channels from @everyone", owner.patch("/api/admin/roles/"+ev.String(),
		map[string]any{"permissions": perm.DefaultEveryone &^ perm.ViewChannel}), http.StatusOK)
	assignRoles(t, owner, viewer, createRole(t, owner, "Seen", perm.ViewChannel))
	assignRoles(t, owner, mod, createRole(t, owner, "Mods",
		perm.ViewChannel|perm.ManageChannels|perm.DisconnectMembers|perm.ManageMessages|perm.ManageStage))
	assignRoles(t, owner, admin, createRole(t, owner, "Admins", perm.Administrator))

	conv := alice.mustDM(t, bob).ID
	wantStatus(t, "the conversation works", alice.dmPost(conv, "hi"), http.StatusCreated)
	aliceWS := alice.dialLiveWSQuery(a.origin(), "?dm=1")
	res := alice.post("/api/dms/"+conv.String()+"/talk", nil)
	wantStatus(t, "start a call", res, http.StatusCreated)
	var call struct {
		ID uuid.UUID `json:"channel_id"`
	}
	res.decode(t, &call)
	path := "/api/channels/" + call.ID.String() + "/messages"

	// (a) The members, who hold no role, see, write in, read and join their call.
	wantStatus(t, "the starter writes in the call's chat", alice.post(path, map[string]any{"content": "can you hear me"}), http.StatusCreated)
	wantStatus(t, "the other member reads it", bob.get(path), http.StatusOK)
	for name, c := range map[string]*client{"alice": alice, "bobby": bob} {
		if p := channelPerms(myPerms(t, c), call.ID); !p.Has(perm.ViewChannel | perm.Connect | perm.Speak | perm.Video | perm.SendMessages) {
			t.Errorf("%s's permission snapshot holds %b in the call", name, p)
		}
	}
	bobWS := bob.dialLiveWSQuery(a.origin(), "?dm=1")
	bobWS.send(events.ClientVoiceJoin, map[string]any{"channel_id": call.ID})
	raw, ok := aliceWS.expect(events.DMCallEnd, 3*time.Second)
	if !ok {
		t.Fatal("bobby could not join the call: the ring was not answered")
	}
	if end := decodeEvent[dm.DMCallEnd](t, raw); end.Reason != dm.CallAnswered {
		t.Fatalf("call end: %+v", end)
	}

	// (b) Nobody outside the conversation gets anything from community rights.
	for name, c := range map[string]*client{"a member with View channels": viewer, "a moderator": mod,
		"an Administrator": admin, "the Owner": owner} {
		wantStatus(t, name+" reads the call's chat", c.get(path), http.StatusNotFound)
		if p := channelPerms(myPerms(t, c), call.ID); p != 0 {
			t.Errorf("%s's permission snapshot holds %b in the call", name, p)
		}
	}
}
