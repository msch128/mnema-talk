//go:build integration

package server

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (a *app) coverEventAt(admin *client, start time.Time, end *time.Time) communityevents.CommunityEvent {
	a.t.Helper()
	ev, err := a.router.Events.Store.Create(context.Background(), communityevents.CreateEventRequest{
		Title: "Scheduled", StartTime: start, EndTime: end, Location: strPtr("Hall"),
	}, admin.user.ID)
	if err != nil {
		a.t.Fatal(err)
	}
	return *ev
}

func (a *app) setStatus(id uuid.UUID, status string) {
	a.t.Helper()
	if _, err := a.db.Exec(context.Background(), `UPDATE events SET status = $2 WHERE id = $1`, id, status); err != nil {
		a.t.Fatal(err)
	}
}

func TestEventStoreReportsMissingEvents(t *testing.T) {
	a := newApp(t, false)
	store := a.router.Events.Store
	ctx := context.Background()
	missing := uuid.New()
	title := "x"

	checks := map[string]func() error{
		"get": func() error { _, err := store.Get(ctx, missing, nil); return err },
		"update": func() error {
			_, err := store.Update(ctx, missing, communityevents.UpdateEventRequest{Title: &title})
			return err
		},
		"cancel":     func() error { _, err := store.Cancel(ctx, missing); return err },
		"set cover":  func() error { return store.SetCover(ctx, missing, uuid.New()) },
		"drop cover": func() error { _, err := store.ClearCover(ctx, missing); return err },
	}
	for name, run := range checks {
		var apiErr *httpx.APIError
		if err := run(); !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
			t.Errorf("%s of an unknown event: %v, want a 404", name, err)
		}
	}
	if rsvps, err := store.ListRSVPs(ctx, missing); err != nil || len(rsvps) != 0 {
		t.Fatalf("rsvps of an unknown event: %v, %v", rsvps, err)
	}
}

func TestEventStoreFailsWhenTheDatabaseCallIsCancelled(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ev := a.coverEvent(admin, nil)
	store := a.router.Events.Store
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	title := "x"
	uid := admin.user.ID

	checks := map[string]func() error{
		"create": func() error {
			_, err := store.Create(ctx, communityevents.CreateEventRequest{Title: "t", StartTime: time.Now()}, uid)
			return err
		},
		"get": func() error { _, err := store.Get(ctx, ev.ID, &uid); return err },
		"update": func() error {
			_, err := store.Update(ctx, ev.ID, communityevents.UpdateEventRequest{Title: &title})
			return err
		},
		"cancel":         func() error { _, err := store.Cancel(ctx, ev.ID); return err },
		"set cover":      func() error { return store.SetCover(ctx, ev.ID, uuid.New()) },
		"drop cover":     func() error { _, err := store.ClearCover(ctx, ev.ID); return err },
		"set rsvp":       func() error { return store.SetRSVP(ctx, ev.ID, uid, "going") },
		"delete rsvp":    func() error { return store.DeleteRSVP(ctx, ev.ID, uid) },
		"list rsvps":     func() error { _, err := store.ListRSVPs(ctx, ev.ID); return err },
		"list":           func() error { _, err := store.List(ctx, &uid, communityevents.ListQuery{Limit: 5}); return err },
		"activate":       func() error { _, err := store.ActivateScheduled(ctx, time.Now()); return err },
		"complete":       func() error { _, err := store.CompleteActive(ctx, time.Now()); return err },
		"due reminders":  func() error { _, err := store.DueReminders(ctx, time.Now()); return err },
		"mark reminded":  func() error { return store.MarkReminderSent(ctx, ev.ID) },
		"reminder users": func() error { _, err := store.GetRSVPUserIDs(ctx, ev.ID); return err },
	}
	for name, run := range checks {
		if err := run(); err == nil {
			t.Errorf("%s must fail on a cancelled context", name)
		}
	}
	a.router.EventsScheduler.Tick(ctx, time.Now()) // logs, must not panic
	if err := a.router.Events.RetractHidden(ctx, nil, nil); err == nil {
		t.Error("retracting with a cancelled context must fail")
	}
}

func TestEventListWithoutAllowFilterReturnsEverythingInTheWindow(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	secret := a.hiddenTalk(admin, "vip-lounge")
	a.coverEvent(admin, nil)
	a.coverEvent(admin, &secret)
	now := time.Now()
	list, err := a.router.Events.Store.List(context.Background(), nil, communityevents.ListQuery{
		After: now.Add(-time.Hour), Before: now.Add(24 * time.Hour), Limit: 10,
	})
	if err != nil || len(list) != 2 {
		t.Fatalf("got %d events, err %v", len(list), err)
	}
	only, err := a.router.Events.Store.List(context.Background(), nil, communityevents.ListQuery{
		After: now.Add(-time.Hour), Before: now.Add(24 * time.Hour), Limit: 10,
		Allow: func(uuid.UUID) bool { return false },
	})
	if err != nil || len(only) != 1 || only[0].ChannelID != nil {
		t.Fatalf("with every Talk denied only the plain event remains: %v %v", only, err)
	}
}

func TestEventRSVPEdgeCases(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	manager := a.register(admin, "manager")
	secret := a.hiddenTalk(admin, "vip-lounge")
	open := a.coverEvent(admin, nil)
	hidden := a.coverEvent(admin, &secret)
	if _, err := a.db.Exec(context.Background(), `UPDATE users SET avatar_s3_key = 'avatars/a.png' WHERE id = $1`, alice.user.ID); err != nil {
		t.Fatal(err)
	}
	path := func(id uuid.UUID, tail string) string { return "/api/events/" + id.String() + tail }

	wantStatus(t, "bad response", alice.put(path(open.ID, "/rsvp"), map[string]string{"response": "maybe"}), http.StatusBadRequest)
	wantStatus(t, "bad body", alice.put(path(open.ID, "/rsvp"), "nope"), http.StatusBadRequest)
	wantStatus(t, "bad id", alice.put("/api/events/x/rsvp", map[string]string{"response": "going"}), http.StatusBadRequest)
	wantStatus(t, "unknown event", alice.put(path(uuid.New(), "/rsvp"), map[string]string{"response": "going"}), http.StatusNotFound)
	wantStatus(t, "unknown delete", alice.delete(path(uuid.New(), "/rsvp")), http.StatusNotFound)
	wantStatus(t, "unknown list", alice.get(path(uuid.New(), "/rsvps")), http.StatusNotFound)
	wantStatus(t, "unknown get", alice.get(path(uuid.New(), "")), http.StatusNotFound)
	wantStatus(t, "bad id delete", alice.delete("/api/events/x/rsvp"), http.StatusBadRequest)
	wantStatus(t, "bad id list", alice.get("/api/events/x/rsvps"), http.StatusBadRequest)
	wantStatus(t, "bad id get", alice.get("/api/events/x"), http.StatusBadRequest)
	wantStatus(t, "hidden delete", alice.delete(path(hidden.ID, "/rsvp")), http.StatusNotFound)
	wantStatus(t, "hidden get", alice.get(path(hidden.ID, "")), http.StatusNotFound)

	wantStatus(t, "empty list", alice.get(path(open.ID, "/rsvps")), http.StatusOK)
	res := alice.put(path(open.ID, "/rsvp"), map[string]string{"response": " not going "})
	wantStatus(t, "spaced not going", res, http.StatusOK)
	var ev communityevents.CommunityEvent
	res.decode(t, &ev)
	if ev.MyRSVP == nil || *ev.MyRSVP != "not_going" || ev.RSVPCounts.NotGoing != 1 {
		t.Fatalf("not going was not normalised: %+v", ev)
	}
	wantStatus(t, "going", alice.put(path(open.ID, "/rsvp"), map[string]string{"response": "going"}), http.StatusOK)

	var items []communityevents.RSVPItem
	res = manager.get(path(open.ID, "/rsvps"))
	wantStatus(t, "list", res, http.StatusOK)
	res.decode(t, &items)
	if len(items) != 1 || items[0].User.AvatarURL == nil {
		t.Fatalf("rsvps: %+v", items)
	}
	res = manager.get(path(open.ID, ""))
	res.decode(t, &ev)
	if len(ev.GoingMembers) != 1 || ev.GoingMembers[0].AvatarURL == nil {
		t.Fatalf("going members: %+v", ev.GoingMembers)
	}
	res = manager.get("/api/events")
	var list []communityevents.CommunityEvent
	res.decode(t, &list)
	if len(list) == 0 || len(list[0].GoingMembers) != 1 || list[0].GoingMembers[0].AvatarURL == nil {
		t.Fatalf("listed going members: %+v", list)
	}
}

func TestEventSchedulerTransitionsAndReminders(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	going := a.register(admin, "going")
	interested := a.register(admin, "interested")
	declined := a.register(admin, "declined")
	now := time.Now()
	past := now.Add(-time.Minute)
	ended := now.Add(-30 * time.Second)

	finished := a.coverEventAt(admin, now.Add(-time.Hour), &ended)
	a.setStatus(finished.ID, "active")
	soon := a.coverEventAt(admin, now.Add(5*time.Minute), nil)
	started := a.coverEventAt(admin, past, nil)
	a.setStatus(started.ID, "scheduled")
	for c, resp := range map[*client]string{going: "going", interested: "interested", declined: "not_going"} {
		wantStatus(t, "rsvp", c.put("/api/events/"+soon.ID.String()+"/rsvp", map[string]string{"response": resp}), http.StatusOK)
	}

	a.router.EventsScheduler.Tick(context.Background(), now)

	for id, want := range map[uuid.UUID]string{finished.ID: communityevents.StatusCompleted, started.ID: communityevents.StatusActive} {
		got, err := a.router.Events.Store.Get(context.Background(), id, nil)
		if err != nil || got.Status != want {
			t.Fatalf("event %s: status %v (%v), want %s", id, got, err, want)
		}
	}
	reminded := map[uuid.UUID]bool{}
	for _, rec := range a.events.Snapshot() {
		if rec.Type == events.EventReminder {
			for _, r := range rec.Recipients {
				reminded[r] = true
			}
		}
	}
	if !reminded[going.user.ID] || !reminded[interested.user.ID] || reminded[declined.user.ID] || len(reminded) != 2 {
		t.Fatalf("reminders went to %v", reminded)
	}
}

func TestEventSchedulerWithoutOptionalServices(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	soon := a.coverEventAt(admin, time.Now().Add(5*time.Minute), nil)
	wantStatus(t, "rsvp", alice.put("/api/events/"+soon.ID.String()+"/rsvp", map[string]string{"response": "going"}), http.StatusOK)

	bare := &communityevents.Handler{DB: a.db, Store: a.router.Events.Store}
	communityevents.NewScheduler(bare).Tick(context.Background(), time.Now())
	var sent *time.Time
	if err := a.db.QueryRow(context.Background(), `SELECT reminder_sent_at FROM events WHERE id = $1`, soon.ID).Scan(&sent); err != nil || sent == nil {
		t.Fatalf("reminder must be marked sent even without publishers: %v %v", sent, err)
	}
}

func TestEventRetractHiddenForNamedMembers(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	outsider := a.register(admin, "outsider")
	talk := a.createChannel(admin, "open-talk", "voice")
	ev := a.coverEvent(admin, &talk)
	h := a.router.Events

	// Load a snapshot while the Talk is open, then hide it.
	wantStatus(t, "list", outsider.get("/api/events"), http.StatusOK)
	wantStatus(t, "hide", putOverwrites(admin, "channels", talk,
		overwrite("role", everyoneID(t, admin), 0, perm.ViewChannel)), http.StatusNoContent)
	if _, err := h.Perms.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.RetractHidden(context.Background(), h.Perms.Previous(), []uuid.UUID{outsider.user.ID}); err != nil {
		t.Fatal(err)
	}
	if !deletedFor(a, outsider.user.ID, ev.ID) {
		t.Fatal("the named member who lost the view must be told to drop the event")
	}
}

func TestEventRetractionIsANoOpWithoutHistory(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ctx := context.Background()
	bare := &communityevents.Handler{DB: a.db, Store: a.router.Events.Store}

	if err := bare.RetractHidden(ctx, nil, nil); err != nil {
		t.Fatalf("no events: %v", err)
	}
	talk := a.createChannel(admin, "open-talk", "voice")
	a.coverEvent(admin, &talk)
	if err := bare.RetractHidden(ctx, nil, nil); err != nil {
		t.Fatalf("no earlier permission state: %v", err)
	}
	bare.PermissionsChanged(nil) // nothing to publish with, must return at once
	for _, rec := range a.events.Snapshot() {
		if rec.Type == events.EventDelete {
			t.Fatalf("nothing may be retracted: %+v", rec)
		}
	}
}

func TestEventRetractionFailureIsLoggedNotFatal(t *testing.T) {
	a := newApp(t, false)
	pool, err := pgxpool.NewWithConfig(context.Background(), a.db.Config())
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	broken := &db.Pool{Pool: pool}
	h := &communityevents.Handler{DB: broken, Store: communityevents.NewStore(broken), Events: a.events, Perms: a.router.Events.Perms}

	if err := h.RetractHidden(context.Background(), nil, nil); err == nil {
		t.Fatal("a closed database must be reported")
	}
	h.PermissionsChanged(nil) // runs in the background and only logs
	time.Sleep(200 * time.Millisecond)
}

func (a *app) pastPage(c *client, query string) []communityevents.CommunityEvent {
	a.t.Helper()
	res := c.get("/api/events?when=past&" + query)
	wantStatus(a.t, "past list", res, http.StatusOK)
	var list []communityevents.CommunityEvent
	res.decode(a.t, &list)
	return list
}

func TestPastEventsArePagedNewestFirstAndRespectVisibility(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	member := a.register(admin, "member")
	secret := a.hiddenTalk(admin, "vip-lounge")

	now := time.Now()
	var ids []uuid.UUID
	for i := 1; i <= 3; i++ {
		start := now.Add(-time.Duration(i) * 72 * time.Hour)
		end := start.Add(time.Hour)
		ev := a.coverEventAt(admin, start, &end)
		a.setStatus(ev.ID, communityevents.StatusCompleted)
		ids = append(ids, ev.ID)
	}
	cancelled := a.coverEventAt(admin, now.Add(48*time.Hour), nil)
	a.setStatus(cancelled.ID, communityevents.StatusCancelled)
	upcoming := a.coverEventAt(admin, now.Add(24*time.Hour), nil)
	hidden := a.coverEvent(admin, &secret)
	a.setStatus(hidden.ID, communityevents.StatusCompleted)

	all := a.pastPage(member, "")
	want := []uuid.UUID{cancelled.ID, ids[0], ids[1], ids[2]}
	if len(all) != len(want) {
		t.Fatalf("member sees %d past events, want %d", len(all), len(want))
	}
	for i, ev := range all {
		if ev.ID != want[i] {
			t.Fatalf("position %d: got %s, want %s (newest first, no upcoming, no hidden Talk)", i, ev.ID, want[i])
		}
		if ev.ID == upcoming.ID || ev.ID == hidden.ID {
			t.Fatal("an upcoming or hidden event leaked into the past list")
		}
	}
	if got := a.pastPage(admin, ""); len(got) != 5 {
		t.Fatalf("a member who can view the Talk also sees its event: %d", len(got))
	}

	first := a.pastPage(member, "limit=2")
	last := first[len(first)-1]
	second := a.pastPage(member, "limit=2&before="+last.StartTime.UTC().Format(time.RFC3339Nano)+"&before_id="+last.ID.String())
	if len(first) != 2 || len(second) != 2 || second[0].ID != ids[1] || second[1].ID != ids[2] {
		t.Fatalf("pages do not continue seamlessly: %v / %v", first, second)
	}
	if rest := a.pastPage(member, "limit=2&before="+second[1].StartTime.UTC().Format(time.RFC3339Nano)+"&before_id="+second[1].ID.String()); len(rest) != 0 {
		t.Fatalf("past the oldest event the page is empty, got %d", len(rest))
	}

	wantStatus(t, "bad when", member.get("/api/events?when=sometime"), http.StatusBadRequest)
	wantStatus(t, "bad before_id", member.get("/api/events?when=past&before_id=x"), http.StatusBadRequest)
	if def := member.get("/api/events"); def.status != http.StatusOK {
		t.Fatal("default listing unchanged")
	}
}
