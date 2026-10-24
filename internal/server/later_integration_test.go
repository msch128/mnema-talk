//go:build integration

package server

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/push"
)

// recordingPush collects the pushes the later service asks for.
type recordingPush struct {
	mu  sync.Mutex
	got []push.Direct
}

func (r *recordingPush) Direct(_ context.Context, d push.Direct) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, d)
}

func (r *recordingPush) take() []push.Direct {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.got
	r.got = nil
	return out
}

type laterFixture struct {
	a       *app
	owner   *client
	alice   *client
	bob     *client
	channel uuid.UUID
	push    *recordingPush
}

func newLaterFixture(t *testing.T) *laterFixture {
	t.Helper()
	a := newApp(t, false)
	owner := a.seedAdmin()
	f := &laterFixture{a: a, owner: owner, alice: a.register(owner, "alice"), bob: a.register(owner, "bobby"),
		channel: a.createChannel(owner, "general", "text"), push: &recordingPush{}}
	// The background ticker is 15 seconds; the tests drive the sweeps themselves.
	a.router.Later.Push = f.push
	return f
}

func inFuture(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339Nano) }

// due makes everything waiting fall due now.
func (f *laterFixture) due(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.a.db.Exec(ctx, `UPDATE message_reminders SET remind_at = NOW() - INTERVAL '1 second' WHERE delivered_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(ctx, `UPDATE scheduled_messages SET send_at = NOW() - INTERVAL '1 second' WHERE state = 'pending'`); err != nil {
		t.Fatal(err)
	}
}

func (f *laterFixture) sweep(t *testing.T) {
	t.Helper()
	f.due(t)
	f.a.router.Later.Sweep(context.Background())
}

func (f *laterFixture) remind(c *client, msg uuid.UUID, at string) response {
	return c.post("/api/me/reminders", map[string]any{"message_id": msg, "remind_at": at})
}

func (f *laterFixture) reminders(t *testing.T, c *client) []later.Reminder {
	t.Helper()
	res := c.get("/api/me/reminders")
	wantStatus(t, "list reminders", res, http.StatusOK)
	var out later.ReminderList
	res.decode(t, &out)
	return out.Items
}

func (f *laterFixture) schedule(c *client, channel uuid.UUID, content, at string) response {
	return c.post("/api/channels/"+channel.String()+"/scheduled", map[string]any{"content": content, "send_at": at})
}

func (f *laterFixture) scheduled(t *testing.T, c *client) []later.Scheduled {
	t.Helper()
	res := c.get("/api/me/scheduled")
	wantStatus(t, "list scheduled", res, http.StatusOK)
	var out later.ScheduledList
	res.decode(t, &out)
	return out.Items
}

func (f *laterFixture) history(t *testing.T, c *client, channel uuid.UUID) []chat.Message {
	t.Helper()
	res := c.get("/api/channels/" + channel.String() + "/messages")
	wantStatus(t, "history", res, http.StatusOK)
	var msgs []chat.Message
	res.decode(t, &msgs)
	return msgs
}

func (f *laterFixture) hideChannel(t *testing.T) {
	t.Helper()
	wantStatus(t, "hide", putOverwrites(f.owner, "channels", f.channel, overwrite("role", everyoneID(t, f.owner), 0, perm.ViewChannel)), http.StatusNoContent)
}

func (f *laterFixture) updates(event string) []any {
	var out []any
	for _, e := range f.a.events.Snapshot() {
		if e.Type == event {
			out = append(out, e.Payload)
		}
	}
	return out
}

func TestReminderCreateListAndCancelAreOwnerOnly(t *testing.T) {
	f := newLaterFixture(t)
	msg := f.a.send(f.owner, f.channel, "remember this")

	wantStatus(t, "no session", f.remind(f.a.anon(), msg, inFuture(time.Hour)), http.StatusUnauthorized)
	wantStatus(t, "in the past", f.remind(f.alice, msg, inFuture(-time.Hour)), http.StatusBadRequest)
	wantStatus(t, "too far", f.remind(f.alice, msg, inFuture(400*24*time.Hour)), http.StatusBadRequest)
	wantStatus(t, "unknown message", f.remind(f.alice, uuid.New(), inFuture(time.Hour)), http.StatusNotFound)

	res := f.remind(f.alice, msg, inFuture(time.Hour))
	wantStatus(t, "create", res, http.StatusCreated)
	var r later.Reminder
	res.decode(t, &r)
	if !r.Available || r.Message == nil || r.Message.Snippet != "remember this" || r.DeliveredAt != nil {
		t.Fatalf("reminder: %s", res.body)
	}
	if got := f.reminders(t, f.alice); len(got) != 1 || got[0].ID != r.ID {
		t.Fatalf("alice's list: %+v", got)
	}
	if got := f.reminders(t, f.bob); len(got) != 0 {
		t.Fatalf("bob sees alice's reminders: %+v", got)
	}
	wantStatus(t, "someone else's reminder", f.bob.delete("/api/me/reminders/"+r.ID.String()), http.StatusNotFound)
	wantStatus(t, "cancel", f.alice.delete("/api/me/reminders/"+r.ID.String()), http.StatusNoContent)
	wantStatus(t, "cancel twice", f.alice.delete("/api/me/reminders/"+r.ID.String()), http.StatusNotFound)
	if len(f.updates(events.ReminderUpdate)) != 2 {
		t.Fatalf("events: %+v", f.updates(events.ReminderUpdate))
	}
}

func TestReminderNeedsAMessageTheMemberCanSeeAndIsCapped(t *testing.T) {
	f := newLaterFixture(t)
	msg := f.a.send(f.owner, f.channel, "secret")
	f.hideChannel(t)
	wantStatus(t, "hidden channel", f.remind(f.alice, msg, inFuture(time.Hour)), http.StatusNotFound)

	// A message of a conversation exists for its members only.
	dm := f.alice.mustDM(t, f.bob)
	if res := f.alice.dmPost(dm.ID, "hi bob"); res.status != http.StatusCreated {
		t.Fatalf("dm: %d %s", res.status, res.body)
	}
	var dmMsg uuid.UUID
	for _, m := range f.history(t, f.alice, dm.ID) {
		dmMsg = m.ID
	}
	wantStatus(t, "the owner is no member", f.remind(f.owner, dmMsg, inFuture(time.Hour)), http.StatusNotFound)
	wantStatus(t, "a member", f.remind(f.bob, dmMsg, inFuture(time.Hour)), http.StatusCreated)

	for i := 0; i < later.MaxReminders; i++ {
		if _, err := f.a.db.Exec(context.Background(), `INSERT INTO message_reminders (user_id, message_id, remind_at) VALUES ($1, $2, NOW() + INTERVAL '1 day')`, f.alice.user.ID, dmMsg); err != nil {
			t.Fatal(err)
		}
	}
	wantStatus(t, "the cap", f.remind(f.alice, dmMsg, inFuture(time.Hour)), http.StatusConflict)
}

func TestReminderIsDeliveredWithTheMessageWhileTheMemberCanSeeIt(t *testing.T) {
	f := newLaterFixture(t)
	msg := f.a.send(f.owner, f.channel, "ping me")
	wantStatus(t, "create", f.remind(f.alice, msg, inFuture(time.Hour)), http.StatusCreated)

	f.sweep(t)
	got := f.push.take()
	if len(got) != 1 || got[0].User != f.alice.user.ID || got[0].Gone || got[0].MessageID == nil || *got[0].MessageID != msg || got[0].Content != "ping me" {
		t.Fatalf("push: %+v", got)
	}
	list := f.reminders(t, f.alice)
	if len(list) != 1 || list[0].DeliveredAt == nil || !list[0].Available {
		t.Fatalf("delivered reminder: %+v", list)
	}
	f.sweep(t)
	if again := f.push.take(); len(again) != 0 {
		t.Fatalf("delivered twice: %+v", again)
	}
	wantStatus(t, "dismiss", f.alice.delete("/api/me/reminders/"+list[0].ID.String()), http.StatusNoContent)
}

func TestReminderChecksAccessAgainWhenItFallsDue(t *testing.T) {
	f := newLaterFixture(t)
	msg := f.a.send(f.owner, f.channel, "members only text")
	wantStatus(t, "create", f.remind(f.alice, msg, inFuture(time.Hour)), http.StatusCreated)

	f.hideChannel(t) // alice loses the channel before the reminder is due
	f.sweep(t)
	got := f.push.take()
	if len(got) != 1 || !got[0].Gone || got[0].MessageID != nil || got[0].ChannelID != nil || got[0].Content != "" || got[0].Sender != "" {
		t.Fatalf("a message the member can no longer see is described: %+v", got)
	}
	list := f.reminders(t, f.alice)
	if len(list) != 1 || list[0].Available || list[0].Message != nil || list[0].DeliveredAt == nil {
		t.Fatalf("list: %+v", list)
	}
	due := f.updates(events.ReminderUpdate)
	if last, ok := due[len(due)-1].(later.ReminderEvent); !ok || last.State != "due" {
		t.Fatalf("events: %+v", due)
	}
}

func TestReminderOfADeletedMessageStillFallsDue(t *testing.T) {
	f := newLaterFixture(t)
	msg := f.a.send(f.alice, f.channel, "oops")
	wantStatus(t, "create", f.remind(f.alice, msg, inFuture(time.Hour)), http.StatusCreated)
	wantStatus(t, "delete", f.alice.delete("/api/channels/"+f.channel.String()+"/messages/"+msg.String()), http.StatusNoContent)
	f.sweep(t)
	if got := f.push.take(); len(got) != 1 || !got[0].Gone {
		t.Fatalf("push: %+v", got)
	}
}

func TestScheduledMessageLifecycleAndLimits(t *testing.T) {
	f := newLaterFixture(t)
	wantStatus(t, "no session", f.schedule(f.a.anon(), f.channel, "x", inFuture(time.Hour)), http.StatusUnauthorized)
	wantStatus(t, "past", f.schedule(f.alice, f.channel, "x", inFuture(-time.Hour)), http.StatusBadRequest)
	wantStatus(t, "empty", f.schedule(f.alice, f.channel, "  ", inFuture(time.Hour)), http.StatusBadRequest)
	wantStatus(t, "unknown channel", f.schedule(f.alice, uuid.New(), "x", inFuture(time.Hour)), http.StatusNotFound)

	res := f.schedule(f.alice, f.channel, "later", inFuture(time.Hour))
	wantStatus(t, "create", res, http.StatusCreated)
	var s later.Scheduled
	res.decode(t, &s)
	if s.State != later.StatePending || s.ChannelName != "general" || s.Content != "later" {
		t.Fatalf("scheduled: %s", res.body)
	}
	if len(f.scheduled(t, f.bob)) != 0 {
		t.Fatal("bob sees alice's scheduled messages")
	}
	wantStatus(t, "edit text", f.alice.patch("/api/me/scheduled/"+s.ID.String(), map[string]any{"content": "later, edited"}), http.StatusOK)
	wantStatus(t, "edit someone else's", f.bob.patch("/api/me/scheduled/"+s.ID.String(), map[string]any{"content": "mine now"}), http.StatusNotFound)
	wantStatus(t, "past time", f.alice.patch("/api/me/scheduled/"+s.ID.String(), map[string]any{"send_at": inFuture(-time.Hour)}), http.StatusBadRequest)
	if got := f.scheduled(t, f.alice); len(got) != 1 || got[0].Content != "later, edited" {
		t.Fatalf("list: %+v", got)
	}
	wantStatus(t, "delete someone else's", f.bob.delete("/api/me/scheduled/"+s.ID.String()), http.StatusNotFound)
	wantStatus(t, "delete", f.alice.delete("/api/me/scheduled/"+s.ID.String()), http.StatusNoContent)

	for i := 0; i < later.MaxScheduled; i++ {
		if _, err := f.a.db.Exec(context.Background(), `INSERT INTO scheduled_messages (user_id, channel_id, content, send_at) VALUES ($1, $2, 'x', NOW() + INTERVAL '1 day')`, f.alice.user.ID, f.channel); err != nil {
			t.Fatal(err)
		}
	}
	wantStatus(t, "the cap", f.schedule(f.alice, f.channel, "one too many", inFuture(time.Hour)), http.StatusConflict)

	// Scheduling needs the right to send now.
	wantStatus(t, "read-only", putOverwrites(f.owner, "channels", f.channel, overwrite("role", everyoneID(t, f.owner), 0, perm.SendMessages)), http.StatusNoContent)
	wantStatus(t, "cannot send now", f.schedule(f.bob, f.channel, "x", inFuture(time.Hour)), http.StatusForbidden)
}

func TestScheduledMessageIsSentAsTheAuthorAtTheTime(t *testing.T) {
	f := newLaterFixture(t)
	wantStatus(t, "schedule", f.schedule(f.alice, f.channel, "good morning @bobby", inFuture(time.Hour)), http.StatusCreated)
	f.sweep(t)

	msgs := f.history(t, f.bob, f.channel)
	if len(msgs) != 1 || msgs[0].UserID != f.alice.user.ID || msgs[0].Content != "good morning @bobby" {
		t.Fatalf("history: %+v", msgs)
	}
	if len(f.scheduled(t, f.alice)) != 0 {
		t.Fatal("a sent message stays scheduled")
	}
	sent := false
	for _, e := range f.a.events.Snapshot() {
		if m, ok := e.Payload.(*chat.Message); ok && e.Type == events.MessageCreate && e.ChannelID == f.channel && m.Content == msgs[0].Content {
			sent = true
		}
	}
	if !sent {
		t.Fatal("message_create was not broadcast to the channel")
	}
	f.sweep(t)
	if len(f.history(t, f.bob, f.channel)) != 1 {
		t.Fatal("sent twice")
	}
}

// refused schedules a message, lets something change, and expects the send to
// be refused with reason: no message appears, a failed draft stays, the
// author is told in the app and by push.
func (f *laterFixture) refused(t *testing.T, channel uuid.UUID, author *client, change func(), reason string) later.Scheduled {
	t.Helper()
	wantStatus(t, "schedule", f.schedule(author, channel, "scheduled text", inFuture(time.Hour)), http.StatusCreated)
	change()
	f.sweep(t)
	list := f.scheduled(t, author)
	if len(list) != 1 || list[0].State != later.StateFailed || list[0].FailureReason != reason {
		t.Fatalf("expected a failed draft (%s): %+v", reason, list)
	}
	pushes := f.push.take()
	if len(pushes) != 1 || pushes[0].Kind != push.DirectScheduledFailed || pushes[0].User != author.user.ID {
		t.Fatalf("push: %+v", pushes)
	}
	var told bool
	for _, p := range f.updates(events.ScheduledUpdate) {
		if e, ok := p.(later.ScheduledEvent); ok && e.State == "failed" && e.Reason == reason {
			told = true
		}
	}
	if !told {
		t.Fatal("the author's sessions were not told")
	}
	return list[0]
}

func TestScheduledSendChecksPermissionsAtSendTime(t *testing.T) {
	f := newLaterFixture(t)
	f.refused(t, f.channel, f.alice, func() {
		wantStatus(t, "read-only", putOverwrites(f.owner, "channels", f.channel, overwrite("role", everyoneID(t, f.owner), 0, perm.SendMessages)), http.StatusNoContent)
	}, later.ReasonNotAllowed)
	if len(f.history(t, f.owner, f.channel)) != 0 {
		t.Fatal("a refused message was posted")
	}
}

func TestScheduledSendIsRefusedWhileTheAuthorIsTimedOut(t *testing.T) {
	f := newLaterFixture(t)
	f.refused(t, f.channel, f.alice, func() {
		res := f.owner.post("/api/admin/users/"+f.alice.user.ID.String()+"/timeout", map[string]any{"duration_seconds": 3600, "reason": "cool off"})
		wantStatus(t, "timeout", res, http.StatusOK)
	}, later.ReasonNotAllowed)
}

func TestScheduledSendHonoursSlowMode(t *testing.T) {
	f := newLaterFixture(t)
	f.refused(t, f.channel, f.alice, func() {
		wantStatus(t, "slow mode", f.owner.patch("/api/admin/channels/"+f.channel.String(), map[string]any{"slow_mode_seconds": 600}), http.StatusOK)
		f.a.send(f.alice, f.channel, "typed by hand")
	}, later.ReasonSlowMode)
	if len(f.history(t, f.owner, f.channel)) != 1 {
		t.Fatal("slow mode let the scheduled message through")
	}
}

func TestScheduledSendIntoAConversationHonoursBlocksSetSinceScheduling(t *testing.T) {
	f := newLaterFixture(t)
	dm := f.alice.mustDM(t, f.bob)
	if res := f.alice.dmPost(dm.ID, "hello"); res.status != http.StatusCreated {
		t.Fatalf("dm: %d %s", res.status, res.body)
	}
	item := f.refused(t, dm.ID, f.alice, func() {
		wantStatus(t, "block", f.bob.put("/api/me/blocks/"+f.alice.user.ID.String(), nil), http.StatusNoContent)
	}, later.ReasonNotAllowed)
	if item.ChannelName != "" || !item.DM {
		t.Fatalf("a conversation shows no channel name: %+v", item)
	}
	if len(f.history(t, f.bob, dm.ID)) != 1 {
		t.Fatal("the scheduled message reached a member who blocked the author")
	}
}

func TestScheduledSendWorksInAConversationAndAFailedDraftCanBeRescheduled(t *testing.T) {
	f := newLaterFixture(t)
	dm := f.alice.mustDM(t, f.bob)
	if res := f.alice.dmPost(dm.ID, "hello"); res.status != http.StatusCreated {
		t.Fatalf("dm: %d %s", res.status, res.body)
	}
	if res := f.bob.dmPost(dm.ID, "hi alice"); res.status != http.StatusCreated {
		t.Fatalf("dm reply: %d %s", res.status, res.body)
	}
	wantStatus(t, "schedule", f.schedule(f.alice, dm.ID, "see you at noon", inFuture(time.Hour)), http.StatusCreated)
	f.sweep(t)
	if msgs := f.history(t, f.bob, dm.ID); len(msgs) != 3 || msgs[2].Content != "see you at noon" {
		t.Fatalf("conversation: %+v", msgs)
	}

	// A refused message, once the cause is gone, goes out after being rescheduled.
	wantStatus(t, "schedule in channel", f.schedule(f.alice, f.channel, "second try", inFuture(time.Hour)), http.StatusCreated)
	wantStatus(t, "read-only", putOverwrites(f.owner, "channels", f.channel, overwrite("role", everyoneID(t, f.owner), 0, perm.SendMessages)), http.StatusNoContent)
	f.sweep(t)
	list := f.scheduled(t, f.alice)
	if len(list) != 1 || list[0].State != later.StateFailed {
		t.Fatalf("list: %+v", list)
	}
	wantStatus(t, "lift", putOverwrites(f.owner, "channels", f.channel), http.StatusNoContent)
	wantStatus(t, "reschedule", f.alice.patch("/api/me/scheduled/"+list[0].ID.String(), map[string]any{"send_at": inFuture(time.Hour)}), http.StatusOK)
	if got := f.scheduled(t, f.alice); got[0].State != later.StatePending || got[0].FailureReason != "" {
		t.Fatalf("rescheduled: %+v", got)
	}
	f.sweep(t)
	if msgs := f.history(t, f.owner, f.channel); len(msgs) != 1 || msgs[0].Content != "second try" {
		t.Fatalf("channel: %+v", msgs)
	}
}

func TestScheduledThreadReplyToADeletedMessageIsRefused(t *testing.T) {
	f := newLaterFixture(t)
	root := f.a.send(f.owner, f.channel, "thread root")
	res := f.alice.post("/api/channels/"+f.channel.String()+"/scheduled", map[string]any{"content": "reply", "send_at": inFuture(time.Hour), "parent_id": root})
	wantStatus(t, "schedule a reply", res, http.StatusCreated)
	wantStatus(t, "delete root", f.owner.delete("/api/channels/"+f.channel.String()+"/messages/"+root.String()), http.StatusNoContent)
	f.sweep(t)
	if list := f.scheduled(t, f.alice); len(list) != 1 || list[0].State != later.StateFailed {
		t.Fatalf("list: %+v", list)
	}
}
