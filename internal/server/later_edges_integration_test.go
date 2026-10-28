//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestScheduledMessageOfADisabledAccountIsNotSent(t *testing.T) {
	f := newLaterFixture(t)
	wantStatus(t, "schedule", f.schedule(f.alice, f.channel, "from a disabled account", inFuture(time.Hour)), http.StatusCreated)
	wantStatus(t, "remind", f.remind(f.alice, f.a.send(f.owner, f.channel, "x"), inFuture(time.Hour)), http.StatusCreated)
	if _, err := f.a.db.Exec(context.Background(), `UPDATE users SET disabled_at = NOW() WHERE id = $1`, f.alice.user.ID); err != nil {
		t.Fatal(err)
	}
	f.sweep(t)
	var state string
	if err := f.a.db.QueryRow(context.Background(), `SELECT state FROM scheduled_messages`).Scan(&state); err != nil || state != later.StateFailed {
		t.Fatalf("state %q, %v", state, err)
	}
	if len(f.history(t, f.owner, f.channel)) != 1 {
		t.Fatal("a disabled account posted")
	}
	for _, p := range f.push.take() {
		if p.Kind == "reminder" {
			t.Fatalf("a disabled account was sent a reminder: %+v", p)
		}
	}
}

func TestScheduledMessageIntoAChannelTheAuthorCannotSeeAnyMoreIsUnavailable(t *testing.T) {
	f := newLaterFixture(t)
	f.refused(t, f.channel, f.alice, f.hideChannelFn(t), later.ReasonUnavailable)
	list := f.scheduled(t, f.alice)
	if list[0].ChannelName != "" {
		t.Fatalf("the name of a channel the author cannot see is shown: %+v", list[0])
	}
}

func (f *laterFixture) hideChannelFn(t *testing.T) func() {
	return func() { f.hideChannel(t) }
}

func TestRemindersAndScheduledMessagesWorkWithoutWebPush(t *testing.T) {
	f := newLaterFixture(t)
	f.a.router.Later.Push = nil
	long := strings.Repeat("long words ", 40)
	msg := f.a.send(f.owner, f.channel, long)
	res := f.remind(f.alice, msg, inFuture(time.Hour))
	wantStatus(t, "remind", res, http.StatusCreated)
	var r later.Reminder
	res.decode(t, &r)
	if r.Message == nil || len([]rune(r.Message.Snippet)) != 200 || !strings.HasSuffix(r.Message.Snippet, "…") {
		t.Fatalf("snippet: %+v", r.Message)
	}
	wantStatus(t, "schedule", f.schedule(f.alice, f.channel, "will be refused", inFuture(time.Hour)), http.StatusCreated)
	wantStatus(t, "read-only", putOverwrites(f.owner, "channels", f.channel, overwrite("role", everyoneID(t, f.owner), 0, perm.SendMessages)), http.StatusNoContent)
	f.sweep(t)
	if list := f.reminders(t, f.alice); len(list) != 1 || list[0].DeliveredAt == nil {
		t.Fatalf("reminders: %+v", list)
	}
	if list := f.scheduled(t, f.alice); len(list) != 1 || list[0].State != later.StateFailed {
		t.Fatalf("scheduled: %+v", list)
	}
}

func TestDeliveredRemindersAreRemovedAfterAMonthAndTheLoopStops(t *testing.T) {
	f := newLaterFixture(t)
	msg := f.a.send(f.owner, f.channel, "old")
	wantStatus(t, "remind", f.remind(f.alice, msg, inFuture(time.Hour)), http.StatusCreated)
	f.sweep(t)
	if _, err := f.a.db.Exec(context.Background(), `UPDATE message_reminders SET delivered_at = NOW() - INTERVAL '40 days'`); err != nil {
		t.Fatal(err)
	}
	// The first sweep booked the hourly prune: the next one is due an hour later.
	f.a.router.Later.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	f.sweep(t)
	if got := f.reminders(t, f.alice); len(got) != 0 {
		t.Fatalf("an old delivered reminder is kept: %+v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.a.router.Later.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop with its context")
	}
}

func TestAReminderOfAThreadReplyOpensTheThread(t *testing.T) {
	f := newLaterFixture(t)
	root := f.a.send(f.owner, f.channel, "root")
	res := f.owner.post("/api/channels/"+f.channel.String()+"/messages", map[string]any{"content": "reply", "parent_id": root})
	wantStatus(t, "reply", res, http.StatusCreated)
	var reply struct{ ID string }
	res.decode(t, &reply)
	wantStatus(t, "remind", f.alice.post("/api/me/reminders", map[string]any{"message_id": reply.ID, "remind_at": inFuture(time.Hour)}), http.StatusCreated)
	f.sweep(t)
	got := f.push.take()
	if len(got) != 1 || got[0].ParentID == nil || *got[0].ParentID != root {
		t.Fatalf("push: %+v", got)
	}
}
