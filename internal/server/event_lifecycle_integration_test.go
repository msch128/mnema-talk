//go:build integration

package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (a *app) getEvent(c *client, id uuid.UUID) communityevents.CommunityEvent {
	a.t.Helper()
	res := c.get("/api/events/" + id.String())
	wantStatus(a.t, "get event", res, http.StatusOK)
	var ev communityevents.CommunityEvent
	res.decode(a.t, &ev)
	return ev
}

func TestEventEditCanClearTheEndTime(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	end := time.Now().Add(2 * time.Hour)
	ev := a.coverEventAt(admin, time.Now().Add(time.Hour), &end)
	path := "/api/events/" + ev.ID.String()

	wantStatus(t, "edit title", admin.patch(path, map[string]any{"title": "Renamed"}), http.StatusOK)
	if got := a.getEvent(admin, ev.ID); got.EndTime == nil {
		t.Fatal("an edit that does not mention end_time must keep it")
	}
	res := admin.patch(path, map[string]any{"end_time": nil})
	wantStatus(t, "clear end", res, http.StatusOK)
	var got communityevents.CommunityEvent
	res.decode(t, &got)
	if got.EndTime != nil || a.getEvent(admin, ev.ID).EndTime != nil {
		t.Fatalf("end_time null must remove the end time: %+v", got.EndTime)
	}
	wantStatus(t, "unknown field", admin.patch(path, map[string]any{"nope": 1}), http.StatusBadRequest)
}

func TestEventWithoutEndCompletesAfterTheDefaultDuration(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	now := time.Now()
	long := a.coverEventAt(admin, now.Add(-3*time.Hour), nil)
	recent := a.coverEventAt(admin, now.Add(-time.Hour), nil)
	a.setStatus(long.ID, communityevents.StatusActive)
	a.setStatus(recent.ID, communityevents.StatusActive)

	a.router.EventsScheduler.Tick(context.Background(), now)

	if got := a.getEvent(admin, long.ID); got.Status != communityevents.StatusCompleted {
		t.Fatalf("an event without an end started 3 h ago is %s, want completed", got.Status)
	}
	if got := a.getEvent(admin, recent.ID); got.Status != communityevents.StatusActive {
		t.Fatalf("an event without an end started 1 h ago is %s, want active", got.Status)
	}
}

func TestEventEditRederivesTheStatusFromTheTimes(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	later := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)

	done := a.coverEventAt(admin, time.Now().Add(-3*time.Hour), nil)
	a.setStatus(done.ID, communityevents.StatusCompleted)
	wantStatus(t, "postpone", admin.patch("/api/events/"+done.ID.String(), map[string]any{"start_time": later}), http.StatusOK)
	if got := a.getEvent(admin, done.ID); got.Status != communityevents.StatusScheduled {
		t.Fatalf("a postponed event is %s, want scheduled", got.Status)
	}

	running := a.coverEventAt(admin, time.Now().Add(-time.Minute), nil)
	a.setStatus(running.ID, communityevents.StatusActive)
	wantStatus(t, "postpone running", admin.patch("/api/events/"+running.ID.String(), map[string]any{"start_time": later}), http.StatusOK)
	if got := a.getEvent(admin, running.ID); got.Status != communityevents.StatusScheduled {
		t.Fatalf("a running event moved to next week is %s, want scheduled", got.Status)
	}

	cancelled := a.coverEventAt(admin, time.Now().Add(time.Hour), nil)
	wantStatus(t, "cancel", admin.post("/api/events/"+cancelled.ID.String()+"/cancel", nil), http.StatusOK)
	wantStatus(t, "move cancelled", admin.patch("/api/events/"+cancelled.ID.String(), map[string]any{"start_time": later}), http.StatusOK)
	if got := a.getEvent(admin, cancelled.ID); got.Status != communityevents.StatusCancelled {
		t.Fatalf("editing a cancelled event revived it: %s", got.Status)
	}
}

func (a *app) lastAuditAction(id uuid.UUID) string {
	a.t.Helper()
	var action string
	if err := a.db.QueryRow(context.Background(), `SELECT action FROM audit_log WHERE target_id = $1 ORDER BY id DESC LIMIT 1`, id.String()).Scan(&action); err != nil {
		a.t.Fatal(err)
	}
	return action
}

func TestEventStatusChangesFollowTheTransitions(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	patch := func(id uuid.UUID, body map[string]any) response { return admin.patch("/api/events/"+id.String(), body) }

	done := a.coverEventAt(admin, time.Now().Add(-3*time.Hour), nil)
	a.setStatus(done.ID, communityevents.StatusCompleted)
	wantStatus(t, "reopen completed", patch(done.ID, map[string]any{"status": "scheduled"}), http.StatusConflict)
	wantStatus(t, "activate completed", patch(done.ID, map[string]any{"status": "active"}), http.StatusConflict)
	wantStatus(t, "cancel completed by patch", patch(done.ID, map[string]any{"status": "cancelled"}), http.StatusConflict)
	wantStatus(t, "cancel completed", admin.post("/api/events/"+done.ID.String()+"/cancel", nil), http.StatusConflict)
	wantStatus(t, "unknown status", patch(done.ID, map[string]any{"status": "paused"}), http.StatusBadRequest)
	if got := a.getEvent(admin, done.ID); got.Status != communityevents.StatusCompleted {
		t.Fatalf("a refused change must not alter the status: %s", got.Status)
	}

	open := a.coverEventAt(admin, time.Now().Add(time.Hour), nil)
	wantStatus(t, "same status", patch(open.ID, map[string]any{"status": "scheduled", "title": "Same"}), http.StatusOK)
	if got := a.lastAuditAction(open.ID); got != "event.edit" {
		t.Fatalf("audit action %q, want event.edit", got)
	}
	res := patch(open.ID, map[string]any{"status": "cancelled"})
	wantStatus(t, "cancel by patch", res, http.StatusOK)
	var got communityevents.CommunityEvent
	res.decode(t, &got)
	if got.Status != communityevents.StatusCancelled {
		t.Fatalf("status %s, want cancelled", got.Status)
	}
	if action := a.lastAuditAction(open.ID); action != "event.cancel" {
		t.Fatalf("cancelling through PATCH is audited as %q, want event.cancel", action)
	}

	wantStatus(t, "cancel again", admin.post("/api/events/"+open.ID.String()+"/cancel", nil), http.StatusConflict)
	wantStatus(t, "patch cancelled again", patch(open.ID, map[string]any{"status": "cancelled"}), http.StatusOK)
	wantStatus(t, "revive", patch(open.ID, map[string]any{"status": "scheduled"}), http.StatusConflict)
}

// Cancelling and moving the end into the past in one PATCH must not save the
// edit and then fail the cancel with a conflict.
func TestPatchCancelWithAPastEndCancelsAndKeepsTheEdit(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ev := a.coverEventAt(admin, time.Now().Add(-time.Hour), nil)
	end := time.Now().Add(-30 * time.Minute).UTC().Truncate(time.Second)
	res := admin.patch("/api/events/"+ev.ID.String(), map[string]any{"status": "cancelled", "end_time": end.Format(time.RFC3339)})
	wantStatus(t, "cancel with past end", res, http.StatusOK)
	got := a.getEvent(admin, ev.ID)
	if got.Status != communityevents.StatusCancelled || got.EndTime == nil || !got.EndTime.Equal(end) {
		t.Fatalf("event after PATCH: status %s end %v, want cancelled with the new end", got.Status, got.EndTime)
	}
}

func TestEventCreateTreatsAnEmptyLocationAsNone(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	talk := a.createChannel(admin, "events-talk", "voice")
	res := admin.post("/api/events", map[string]any{
		"title": "Talk event", "channel_id": talk, "location": "  ", "start_time": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
	wantStatus(t, "create", res, http.StatusCreated)
	var ev communityevents.CommunityEvent
	res.decode(t, &ev)
	if ev.Location != nil {
		t.Fatalf("a Talk event has no location text, got %q", *ev.Location)
	}
	if strings.Contains(string(res.body), `"location"`) {
		t.Fatalf("the response must not carry an empty location: %s", res.body)
	}
}

func TestEventRetractionCoversEventsThatEndedLongAgo(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	outsider := a.register(admin, "outsider")
	talk := a.createChannel(admin, "old-talk", "voice")
	start := time.Now().Add(-21 * 24 * time.Hour)
	end := start.Add(time.Hour)
	old, err := a.router.Events.Store.Create(context.Background(), communityevents.CreateEventRequest{
		Title: "Three weeks ago", StartTime: start, EndTime: &end, ChannelID: &talk,
	}, admin.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The outsider loads the server once, so a snapshot exists to compare.
	wantStatus(t, "outsider lists", outsider.get("/api/events"), http.StatusOK)

	wantStatus(t, "hide talk", putOverwrites(admin, "channels", talk,
		overwrite("role", everyoneID(t, admin), 0, perm.ViewChannel)), http.StatusNoContent)
	deadline := time.Now().Add(5 * time.Second)
	for !deletedFor(a, outsider.user.ID, old.ID) {
		if time.Now().After(deadline) {
			t.Fatal("a member who lost the Talk must drop its old events too")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// fragmentFailingQuerier fails every statement that mentions one of the fragments.
type fragmentFailingQuerier struct {
	db.Querier
	fragments []string
}

func (f fragmentFailingQuerier) fails(sql string) bool {
	for _, frag := range f.fragments {
		if strings.Contains(sql, frag) {
			return true
		}
	}
	return false
}

func (f fragmentFailingQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.fails(sql) {
		return nil, errors.New("injected failure")
	}
	return f.Querier.Query(ctx, sql, args...)
}

func (f fragmentFailingQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.fails(sql) {
		return pgconn.CommandTag{}, errors.New("injected failure")
	}
	return f.Querier.Exec(ctx, sql, args...)
}

func TestEventReminderStaysDueWhenTheRecipientsCannotBeLoaded(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	soon := a.coverEventAt(admin, time.Now().Add(5*time.Minute), nil)
	wantStatus(t, "rsvp", alice.put("/api/events/"+soon.ID.String()+"/rsvp", map[string]string{"response": "going"}), http.StatusOK)
	sent := func() *time.Time {
		var at *time.Time
		if err := a.db.QueryRow(context.Background(), `SELECT reminder_sent_at FROM events WHERE id = $1`, soon.ID).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}

	flaky := &communityevents.Handler{DB: a.db, Events: a.events,
		Store: communityevents.NewStore(fragmentFailingQuerier{a.db, []string{"FROM event_rsvps WHERE event_id = $1 AND response IN"}})}
	communityevents.NewScheduler(flaky).Tick(context.Background(), time.Now())
	if sent() != nil {
		t.Fatal("a failure while loading the recipients must leave the reminder due")
	}

	a.router.EventsScheduler.Tick(context.Background(), time.Now())
	if sent() == nil {
		t.Fatal("the next tick must send the reminder")
	}
}

func TestEventReminderIsClaimedOnce(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	soon := a.coverEventAt(admin, time.Now().Add(5*time.Minute), nil)
	store := a.router.Events.Store
	ctx := context.Background()

	first, err := store.ClaimReminder(ctx, soon.ID)
	if err != nil || !first {
		t.Fatalf("first claim: %v, %v", first, err)
	}
	if second, err := store.ClaimReminder(ctx, soon.ID); err != nil || second {
		t.Fatalf("a second claim must lose: %v, %v", second, err)
	}
	cancelled := a.coverEventAt(admin, time.Now().Add(5*time.Minute), nil)
	a.setStatus(cancelled.ID, communityevents.StatusCancelled)
	if got, err := store.ClaimReminder(ctx, cancelled.ID); err != nil || got {
		t.Fatalf("a cancelled event has no reminder: %v, %v", got, err)
	}
	reminded := 0
	for _, rec := range a.events.Snapshot() {
		if rec.Type == events.EventReminder {
			reminded++
		}
	}
	if reminded != 0 {
		t.Fatalf("claiming sends nothing itself: %d", reminded)
	}
}

// failingDeleteStore keeps every object: deleting it always fails.
type failingDeleteStore struct{ media.Store }

func (failingDeleteStore) Delete(context.Context, string) error {
	return errors.New("storage unavailable")
}

func TestEventCoverReplacementKeepsTheOldRowWhenStorageFails(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ev := a.coverEvent(admin, nil)
	wantStatus(t, "upload", a.uploadCover(admin, ev.ID, "cover", bannerPNG(t, 900, 400)), http.StatusOK)

	h := a.router.Events
	h.MediaStore = failingDeleteStore{a.store}
	defer func() { h.MediaStore = a.store }()
	wantStatus(t, "replace", a.uploadCover(admin, ev.ID, "cover", bannerPNG(t, 600, 600)), http.StatusOK)

	// The old object is still stored, so its row must still say so: only a row
	// that is not marked deleted can be found by a later cleanup.
	var orphaned int
	if err := a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM media WHERE original_filename LIKE 'cover%' AND is_deleted`).Scan(&orphaned); err != nil {
		t.Fatal(err)
	}
	if orphaned != 0 || a.store.Len() != 2 {
		t.Fatalf("rows marked deleted=%d objects=%d, want 0 and 2", orphaned, a.store.Len())
	}
}
