//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// H-L6-05 (known failure). doc/talks.md, "Start a Talk from a direct
// message": nothing about a conversation's call reaches the audit log, "that
// would tell the Owner who called whom", and only the conversation's members
// see the Talk. A conversation member who holds Manage events can name the
// call Talk as the location of a community event (validateEventTalk checks
// only that the channel is a voice Talk the caller may view, and a member
// views the call). The event is then audited as event.talk_create with the
// call Talk's id and the event's title, and the Owner and every
// Administrator outside the conversation read both in the audit log.
//
// The assertion is inverted: the test passes while the call Talk is accepted
// and leaks, and turns red the day it is refused, so the fix must flip it.
func TestKnownFailureAnEventLocatedInACallOfAConversationReachesTheAuditLog(t *testing.T) {
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

	res = alice.post("/api/events", map[string]any{
		"title": "okapievent", "channel_id": call.ID, "start_time": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
	if res.status != http.StatusCreated {
		t.Fatalf("H-L6-05 no longer reproduces: the call Talk is refused as an event location (%d). Flip this test into a guard", res.status)
	}
	var leaked []string
	for name, c := range map[string]*client{"the Owner": owner, "an Administrator": admin} {
		log := c.get("/api/admin/audit-log")
		wantStatus(t, name+" reads the audit log", log, http.StatusOK)
		for _, word := range []string{"okapievent", call.ID.String()} {
			if strings.Contains(string(log.body), word) {
				leaked = append(leaked, name+" reads "+word)
			}
		}
	}
	if len(leaked) == 0 {
		t.Fatal("H-L6-05 no longer reproduces: the audit entry is hidden now. Flip this test into a guard")
	}
	t.Logf("H-L6-05 still reproduces: %v", leaked)
}
