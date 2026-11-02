//go:build integration

package server

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/communityevents"
)

// Roles and sign-ups through the real router: the waitlist moves up when the
// member in front deletes their account, and the health check advertises the feature.
func TestAnErasedMembersSlotMovesTheWaitlistUp(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")
	ev := a.coverEventAt(admin, time.Now().Add(48*time.Hour), nil)

	res := admin.put("/api/events/"+ev.ID.String()+"/roles", map[string]any{"roles": []map[string]any{{"name": "Tank", "capacity": 1}}})
	wantStatus(t, "set roles", res, http.StatusOK)
	var got communityevents.CommunityEvent
	res.decode(t, &got)
	role := got.Roles[0].ID

	wantStatus(t, "alice signs up", alice.put("/api/events/"+ev.ID.String()+"/signup", map[string]any{"role_id": role}), http.StatusOK)
	res = bob.put("/api/events/"+ev.ID.String()+"/signup", map[string]any{"role_id": role})
	wantStatus(t, "bob signs up", res, http.StatusOK)
	res.decode(t, &got)
	if got.MySignup == nil || got.MySignup.State != communityevents.SignupWaitlist || got.MySignup.WaitlistPosition != 1 {
		t.Fatalf("bob should wait first in line: %+v", got.MySignup)
	}

	res = admin.get("/api/events/" + ev.ID.String())
	res.decode(t, &got)
	if len(got.Signups) != 2 || got.Roles[0].Confirmed != 1 || got.Roles[0].Waitlist != 1 {
		t.Fatalf("details: %+v %+v", got.Roles, got.Signups)
	}

	wantStatus(t, "alice deletes her account", a.deleteAccount(alice, "member-password-123", ""), http.StatusNoContent)
	a.router.EventsScheduler.Tick(context.Background(), time.Now())
	a.router.EventsScheduler.Wait()
	res = bob.get("/api/events/" + ev.ID.String())
	res.decode(t, &got)
	if got.MySignup == nil || got.MySignup.State != communityevents.SignupConfirmed {
		t.Fatalf("bob should have moved up: %+v", got.MySignup)
	}

	res = a.anon().get("/api/health")
	if !bytes.Contains(res.body, []byte("events.roles")) {
		t.Fatalf("health should advertise events.roles: %s", res.body)
	}
}
