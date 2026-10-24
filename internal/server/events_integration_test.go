//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (a *app) hiddenTalk(admin *client, name string) uuid.UUID {
	a.t.Helper()
	ch := a.createChannel(admin, name, "voice")
	everyone := everyoneID(a.t, admin)
	wantStatus(a.t, "hide talk", putOverwrites(admin, "channels", ch, overwrite("role", everyone, 0, perm.ViewChannel)), http.StatusNoContent)
	return ch
}

func (a *app) roleWithManageEvents(admin *client) uuid.UUID {
	a.t.Helper()
	return createRole(a.t, admin, "Event Manager", perm.ManageEvents)
}

func TestCommunityEventsPermissionMatrix(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")

	startTime := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	createReq := map[string]any{
		"title":       "Community Gala",
		"description": "Annual celebration",
		"start_time":  startTime,
		"location":    "Main Hall",
	}

	// 1. Regular member without ManageEvents gets 403
	wantStatus(t, "member cannot create event", alice.post("/api/events", createReq), http.StatusForbidden)

	// 2. Admin can create event
	res := admin.post("/api/events", createReq)
	wantStatus(t, "admin creates event", res, http.StatusCreated)
	var ev communityevents.CommunityEvent
	res.decode(t, &ev)

	// 3. Member cannot update or cancel
	wantStatus(t, "member cannot edit", alice.patch("/api/events/"+ev.ID.String(), map[string]any{"title": "Hacked"}), http.StatusForbidden)
	wantStatus(t, "member cannot cancel", alice.post("/api/events/"+ev.ID.String()+"/cancel", nil), http.StatusForbidden)

	// 4. Grant ManageEvents to bob via custom role
	roleID := a.roleWithManageEvents(admin)
	assignRoles(t, admin, bob, roleID)

	// 5. Bob can edit and cancel
	wantStatus(t, "bob can edit", bob.patch("/api/events/"+ev.ID.String(), map[string]any{"title": "Gala (Updated)"}), http.StatusOK)
	wantStatus(t, "bob can cancel", bob.post("/api/events/"+ev.ID.String()+"/cancel", nil), http.StatusOK)
}

func TestCommunityEventsHiddenTalkMatrix(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	insider := a.register(admin, "insider")
	outsider := a.register(admin, "outsider")

	publicTalk := a.createChannel(admin, "lobby-talk", "voice")
	secretTalk := a.hiddenTalk(admin, "vip-lounge")

	// Allow insider to view secret talk
	wantStatus(t, "grant insider view", putOverwrites(admin, "channels", secretTalk,
		overwrite("role", everyoneID(t, admin), 0, perm.ViewChannel),
		overwrite("member", insider.user.ID, perm.ViewChannel, 0),
	), http.StatusNoContent)

	startTime := time.Now().Add(12 * time.Hour).UTC().Format(time.RFC3339)

	// Event on public Talk
	res1 := admin.post("/api/events", map[string]any{
		"title":       "Public Hangout",
		"description": "Everyone welcome",
		"start_time":  startTime,
		"channel_id":  publicTalk,
	})
	wantStatus(t, "create public event", res1, http.StatusCreated)
	var publicEv communityevents.CommunityEvent
	res1.decode(t, &publicEv)

	// Event on hidden Talk
	res2 := admin.post("/api/events", map[string]any{
		"title":       "Secret Meeting",
		"description": "VIP only",
		"start_time":  startTime,
		"channel_id":  secretTalk,
	})
	wantStatus(t, "create secret event", res2, http.StatusCreated)
	var secretEv communityevents.CommunityEvent
	res2.decode(t, &secretEv)

	// Check insider sees both
	var insiderList []communityevents.CommunityEvent
	resIn := insider.get("/api/events")
	wantStatus(t, "insider list", resIn, http.StatusOK)
	resIn.decode(t, &insiderList)
	if len(insiderList) != 2 {
		t.Fatalf("expected 2 events for insider, got %d", len(insiderList))
	}

	// Check outsider sees only public event
	var outsiderList []communityevents.CommunityEvent
	resOut := outsider.get("/api/events")
	wantStatus(t, "outsider list", resOut, http.StatusOK)
	resOut.decode(t, &outsiderList)
	if len(outsiderList) != 1 || outsiderList[0].ID != publicEv.ID {
		t.Fatalf("expected 1 public event for outsider, got %v", outsiderList)
	}

	// Outsider getting secret event returns 404 (NOT 403)
	wantStatus(t, "outsider get hidden event is 404", outsider.get("/api/events/"+secretEv.ID.String()), http.StatusNotFound)
	wantStatus(t, "outsider rsvp hidden event is 404", outsider.put("/api/events/"+secretEv.ID.String()+"/rsvp", map[string]string{"response": "going"}), http.StatusNotFound)
	wantStatus(t, "outsider list rsvps hidden event is 404", outsider.get("/api/events/"+secretEv.ID.String()+"/rsvps"), http.StatusNotFound)
}

func TestCommunityEventsRSVPFlow(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")

	startTime := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	res := admin.post("/api/events", map[string]any{
		"title":       "Board Game Night",
		"description": "Bring snacks",
		"start_time":  startTime,
		"location":    "Clubhouse",
	})
	wantStatus(t, "create event", res, http.StatusCreated)
	var ev communityevents.CommunityEvent
	res.decode(t, &ev)

	// Alice RSVPs "going"
	resGoing := alice.put("/api/events/"+ev.ID.String()+"/rsvp", map[string]string{"response": "going"})
	wantStatus(t, "alice rsvps going", resGoing, http.StatusOK)
	var evGoing communityevents.CommunityEvent
	resGoing.decode(t, &evGoing)
	if evGoing.MyRSVP == nil || *evGoing.MyRSVP != "going" {
		t.Fatalf("expected my_rsvp=going, got %v", evGoing.MyRSVP)
	}
	if evGoing.RSVPCounts.Going != 1 || evGoing.RSVPCounts.Interested != 0 {
		t.Fatalf("unexpected counts: %+v", evGoing.RSVPCounts)
	}
	if len(evGoing.GoingMembers) != 1 || evGoing.GoingMembers[0].Username != "alice" {
		t.Fatalf("unexpected going members: %+v", evGoing.GoingMembers)
	}

	// Bob RSVPs "interested"
	resInt := bob.put("/api/events/"+ev.ID.String()+"/rsvp", map[string]string{"response": "interested"})
	wantStatus(t, "bob rsvps interested", resInt, http.StatusOK)

	// List RSVPs
	resList := alice.get("/api/events/" + ev.ID.String() + "/rsvps")
	wantStatus(t, "list rsvps", resList, http.StatusOK)
	var rsvps []communityevents.RSVPItem
	resList.decode(t, &rsvps)
	if len(rsvps) != 2 {
		t.Fatalf("expected 2 rsvps, got %d", len(rsvps))
	}

	// Alice deletes RSVP
	resDel := alice.delete("/api/events/" + ev.ID.String() + "/rsvp")
	wantStatus(t, "alice deletes rsvp", resDel, http.StatusOK)
	var evDel communityevents.CommunityEvent
	resDel.decode(t, &evDel)
	if evDel.MyRSVP != nil {
		t.Fatalf("expected my_rsvp=nil after delete, got %v", evDel.MyRSVP)
	}
	if evDel.RSVPCounts.Going != 0 {
		t.Fatalf("expected 0 going, got %d", evDel.RSVPCounts.Going)
	}
}

func TestCommunityEventsActivationAndReminderScheduling(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")

	pastStart := time.Now().Add(-1 * time.Minute)
	evPast, err := a.router.Events.Store.Create(context.Background(), communityevents.CreateEventRequest{
		Title:       "Past Starting Event",
		Description: "Should be active",
		StartTime:   pastStart,
		Location:    strPtr("Main Stage"),
	}, admin.user.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Scheduler tick triggers activation
	a.router.EventsScheduler.Tick(context.Background(), time.Now())

	activeEv, err := a.router.Events.Store.Get(context.Background(), evPast.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if activeEv.Status != communityevents.StatusActive {
		t.Fatalf("expected status=active, got %s", activeEv.Status)
	}

	// 2. Event starting in 10 minutes (due for reminder)
	soonStart := time.Now().Add(10 * time.Minute)
	evSoon, err := a.router.Events.Store.Create(context.Background(), communityevents.CreateEventRequest{
		Title:       "Imminent Event",
		Description: "Starts soon",
		StartTime:   soonStart,
		Location:    strPtr("Auditorium"),
	}, admin.user.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Alice RSVPs going
	wantStatus(t, "alice rsvp", alice.put("/api/events/"+evSoon.ID.String()+"/rsvp", map[string]string{"response": "going"}), http.StatusOK)

	// Tick scheduler: triggers reminder
	a.router.EventsScheduler.Tick(context.Background(), time.Now())

	// Verify recorder received event_reminder
	found := false
	for _, rec := range a.events.Snapshot() {
		if rec.Type == events.EventReminder {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected EventReminder publication in recorder, found: %+v", a.events.Snapshot())
	}
}

func TestCommunityEventsAccountEraseAndExport(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")

	startTime := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	// Alice creates an event
	roleID := a.roleWithManageEvents(admin)
	assignRoles(t, admin, alice, roleID)
	res1 := alice.post("/api/events", map[string]any{
		"title":       "Alice's Workshop",
		"description": "Painting",
		"start_time":  startTime,
		"location":    "Studio",
	})
	wantStatus(t, "alice creates event", res1, http.StatusCreated)
	var aliceEv communityevents.CommunityEvent
	res1.decode(t, &aliceEv)

	// Alice RSVPs to her own event and Bob RSVPs
	alice.put("/api/events/"+aliceEv.ID.String()+"/rsvp", map[string]string{"response": "going"})
	bob.put("/api/events/"+aliceEv.ID.String()+"/rsvp", map[string]string{"response": "interested"})

	// 1. Account export includes rsvps.json
	wantStatus(t, "request export", a.requestExport(alice, "member-password-123", ""), http.StatusAccepted)
	a.waitForExport(alice)
	dl := alice.get("/api/users/me/export/download")
	wantStatus(t, "download export", dl, http.StatusOK)
	files := unzipAll(t, dl.body)
	if _, ok := files["rsvps.json"]; !ok {
		t.Fatalf("expected rsvps.json in export archive: %+v", files)
	}
	var exportedRSVPs []map[string]any
	if err := json.Unmarshal([]byte(files["rsvps.json"]), &exportedRSVPs); err != nil {
		t.Fatalf("unmarshal rsvps.json: %v", err)
	}
	if len(exportedRSVPs) != 1 || exportedRSVPs[0]["response"] != "going" {
		t.Fatalf("unexpected exported rsvps: %s", files["rsvps.json"])
	}

	// 2. Alice deletes her account
	wantStatus(t, "delete alice account", a.deleteAccount(alice, "member-password-123", ""), http.StatusNoContent)

	// Verify Alice's created event created_by is now NULL
	evAfter, err := a.router.Events.Store.Get(context.Background(), aliceEv.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if evAfter.CreatedBy != nil {
		t.Fatalf("expected created_by to be NULL after erase, got %v", evAfter.CreatedBy)
	}

	// Verify Bob's RSVP is still present but Alice's is gone
	rsvpsAfter, err := a.router.Events.Store.ListRSVPs(context.Background(), aliceEv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rsvpsAfter) != 1 || rsvpsAfter[0].User.Username != "bob" {
		t.Fatalf("expected only bob's rsvp remaining, got: %+v", rsvpsAfter)
	}
}

func strPtr(s string) *string {
	return &s
}

func TestCommunityEventsEditValidationAndReminderRearm(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ctx := context.Background()

	res := admin.post("/api/events", map[string]any{
		"title": "Soon", "start_time": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339), "location": "Hall",
	})
	wantStatus(t, "create", res, http.StatusCreated)
	var ev communityevents.CommunityEvent
	res.decode(t, &ev)
	path := "/api/events/" + ev.ID.String()

	wantStatus(t, "unknown status", admin.patch(path, map[string]any{"status": "bogus"}), http.StatusBadRequest)
	wantStatus(t, "unknown field", admin.patch(path, map[string]any{"owner": "x"}), http.StatusBadRequest)
	wantStatus(t, "NUL in title", admin.patch(path, map[string]any{"title": "a\u0000b"}), http.StatusBadRequest)

	a.router.EventsScheduler.Tick(ctx, time.Now())
	var sent *time.Time
	q := func() {
		if err := a.router.Events.Store.DB.QueryRow(ctx, `SELECT reminder_sent_at FROM events WHERE id = $1`, ev.ID).Scan(&sent); err != nil {
			t.Fatal(err)
		}
	}
	q()
	if sent == nil {
		t.Fatal("reminder should have been sent")
	}
	wantStatus(t, "rename keeps reminder", admin.patch(path, map[string]any{"title": "Soon!"}), http.StatusOK)
	q()
	if sent == nil {
		t.Fatal("renaming must not re-arm the reminder")
	}
	wantStatus(t, "move", admin.patch(path, map[string]any{"start_time": time.Now().Add(14 * time.Minute).UTC().Format(time.RFC3339)}), http.StatusOK)
	q()
	if sent != nil {
		t.Fatal("moving the start must re-arm the reminder")
	}
}

func TestCommunityEventsHiddenTalkReminderAndAudit(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	insider := a.register(admin, "insider")
	ctx := context.Background()

	talk := a.hiddenTalk(admin, "vip-lounge")
	grant := func(allow bool) {
		ow := []map[string]any{overwrite("role", everyoneID(t, admin), 0, perm.ViewChannel)}
		if allow {
			ow = append(ow, overwrite("member", insider.user.ID, perm.ViewChannel, 0))
		}
		wantStatus(t, "overwrites", putOverwrites(admin, "channels", talk, ow...), http.StatusNoContent)
	}
	grant(true)

	res := admin.post("/api/events", map[string]any{
		"title": "VIP", "start_time": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339), "channel_id": talk,
	})
	wantStatus(t, "create", res, http.StatusCreated)
	var ev communityevents.CommunityEvent
	res.decode(t, &ev)
	wantStatus(t, "insider rsvp", insider.put("/api/events/"+ev.ID.String()+"/rsvp", map[string]string{"response": "going"}), http.StatusOK)

	var action string
	if err := a.router.Events.Store.DB.QueryRow(ctx, `SELECT action FROM audit_log WHERE target_id = $1 ORDER BY id DESC LIMIT 1`, ev.ID.String()).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action != "event.talk_create" {
		t.Fatalf("Talk events must use the channel-scoped audit class, got %q", action)
	}

	grant(false)
	a.router.EventsScheduler.Tick(ctx, time.Now())
	for _, rec := range a.events.Snapshot() {
		if rec.Type == events.EventReminder {
			t.Fatalf("a member who lost access to the Talk got a reminder: %+v", rec)
		}
	}
}
