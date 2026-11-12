//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/perm"
)

// H-L6-05 (guard; was a known failure). doc/talks.md, "Start a Talk from a
// direct message": nothing about a conversation's call reaches the audit log,
// "that would tell the Owner who called whom", and only the conversation's
// members see the Talk. A conversation member who holds Manage events used to
// name the call Talk as the location of a community event (validateEventTalk
// checked only that the channel is a voice Talk the caller may view, and a
// member views the call); the event was audited as event.talk_create with the
// call's id and the event's title, which the Owner and every Administrator
// outside the conversation read. A call Talk is no community channel now: as a
// location it is refused like a channel that does not exist (same status, same
// bytes), on create, on a move of an event, for a series and for the rest of a
// series, and nothing about it reaches the audit log or the event lists.
func TestAnEventCannotBeLocatedInACallOfAConversation(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	alice, bob := a.register(owner, "alice"), a.register(owner, "bobby")
	admin := a.register(owner, "admins")
	assignRoles(t, owner, alice, createRole(t, owner, "Planners", perm.ManageEvents))
	assignRoles(t, owner, admin, createRole(t, owner, "Admins", perm.Administrator))

	conv := alice.mustDM(t, bob).ID
	wantStatus(t, "the conversation works", alice.dmPost(conv, "hi"), http.StatusCreated)
	res := alice.post("/api/dms/"+conv.String()+"/talk", nil)
	wantStatus(t, "start a call", res, http.StatusCreated)
	var call struct {
		ID uuid.UUID `json:"channel_id"`
	}
	res.decode(t, &call)

	start := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	ghost := alice.post("/api/events", map[string]any{"title": "okapighost", "channel_id": uuid.New(), "start_time": start})
	wantStatus(t, "a channel that does not exist", ghost, http.StatusBadRequest)
	wantLikeGhost := func(what string, got response) {
		t.Helper()
		wantStatus(t, what, got, http.StatusBadRequest)
		if string(got.body) != string(ghost.body) {
			t.Errorf("%s answers %s, a channel that does not exist answers %s", what, got.body, ghost.body)
		}
	}

	wantLikeGhost("create in the call", alice.post("/api/events", map[string]any{
		"title": "okapievent", "channel_id": call.ID, "start_time": start}))
	wantLikeGhost("a series in the call", alice.post("/api/events", map[string]any{
		"title": "okapiseries", "channel_id": call.ID, "start_time": start,
		"recurrence": map[string]any{"frequency": "weekly"}}))

	open := a.createChannel(owner, "lounge", "voice")
	plain := alice.post("/api/events", map[string]any{"title": "okapiplain", "channel_id": open, "start_time": start})
	wantStatus(t, "an event in a community Talk", plain, http.StatusCreated)
	wantLikeGhost("moving an event into the call", alice.patch("/api/events/"+idOf(t, plain).String(), map[string]any{"channel_id": call.ID}))
	series := alice.post("/api/events", map[string]any{"title": "okapiweekly", "channel_id": open, "start_time": start,
		"recurrence": map[string]any{"frequency": "weekly"}})
	wantStatus(t, "a series in a community Talk", series, http.StatusCreated)
	wantLikeGhost("moving a series into the call", alice.patch("/api/events/"+idOf(t, series).String()+"?scope=following",
		map[string]any{"channel_id": call.ID}))

	for name, c := range map[string]*client{"the Owner": owner, "an Administrator": admin} {
		for _, path := range []string{"/api/admin/audit-log", "/api/events"} {
			got := c.get(path)
			wantStatus(t, name+" reads "+path, got, http.StatusOK)
			if strings.Contains(string(got.body), "okapievent") || strings.Contains(string(got.body), call.ID.String()) {
				t.Errorf("%s reads the call or its event in %s", name, path)
			}
		}
	}
}

// H-L6-05 (sibling). An automod rule names the channels it exempts; one of
// them may not be a conversation's call Talk, or the Administrator who is in
// the conversation would publish its id to every Administrator in the rule
// list and the audit log. It is refused as an unknown channel.
func TestAnAutomodRuleCannotExemptACallOfAConversation(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	adm, bob := a.register(owner, "admins"), a.register(owner, "bobby")
	assignRoles(t, owner, adm, createRole(t, owner, "Admins", perm.Administrator))
	conv := adm.mustDM(t, bob).ID
	wantStatus(t, "the conversation works", adm.dmPost(conv, "hi"), http.StatusCreated)
	res := adm.post("/api/dms/"+conv.String()+"/talk", nil)
	wantStatus(t, "start a call", res, http.StatusCreated)
	var call struct {
		ID uuid.UUID `json:"channel_id"`
	}
	res.decode(t, &call)

	rule := func(ch uuid.UUID) map[string]any {
		return map[string]any{"kind": "words", "name": "w", "action": "block", "enabled": true,
			"config": map[string]any{"words": []string{"zzz"}}, "exempt_channels": []uuid.UUID{ch}}
	}
	ghost := adm.post("/api/admin/automod/rules", rule(uuid.New()))
	wantStatus(t, "a channel that does not exist", ghost, http.StatusBadRequest)
	wantStatus(t, "exempt the call", adm.post("/api/admin/automod/rules", rule(call.ID)), http.StatusBadRequest)
	for _, path := range []string{"/api/admin/automod", "/api/admin/audit-log"} {
		if got := owner.get(path); strings.Contains(string(got.body), call.ID.String()) {
			t.Errorf("the call's id reached %s", path)
		}
	}
}

// H-L6-05 (events made before the location was refused). An event that names
// a conversation's call Talk can no longer be created, but one that predates
// the check can be cancelled; the entry (event.talk_cancel) would carry the
// call's id and the event's title to the Owner and the Administrators outside
// the conversation. Such an event is not audited.
func TestCancellingAnEventOfACallOfAConversationWritesNoAuditEntry(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	alice, bob := a.register(owner, "alice"), a.register(owner, "bobby")
	assignRoles(t, owner, alice, createRole(t, owner, "Planners", perm.ManageEvents))
	conv := alice.mustDM(t, bob).ID
	wantStatus(t, "the conversation works", alice.dmPost(conv, "hi"), http.StatusCreated)
	res := alice.post("/api/dms/"+conv.String()+"/talk", nil)
	wantStatus(t, "start a call", res, http.StatusCreated)
	var call struct {
		ID uuid.UUID `json:"channel_id"`
	}
	res.decode(t, &call)

	// The API refuses such an event now; the store does not.
	ev, err := a.router.Events.Store.Create(context.Background(), communityevents.CreateEventRequest{
		Title: "okapilegacy", StartTime: time.Now().Add(time.Hour), ChannelID: &call.ID}, alice.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "cancel", alice.post("/api/events/"+ev.ID.String()+"/cancel", nil), http.StatusOK)
	got := owner.get("/api/admin/audit-log")
	wantStatus(t, "the Owner reads the audit log", got, http.StatusOK)
	if strings.Contains(string(got.body), "okapilegacy") || strings.Contains(string(got.body), call.ID.String()) {
		t.Error("the cancellation of an event of a call reached the audit log")
	}
}
