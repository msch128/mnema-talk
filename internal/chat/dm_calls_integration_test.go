//go:build integration

package chat

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

type callWorldDB struct {
	*talksWorld
	h     *Handler
	calls *DMCalls
	dm    uuid.UUID
	talk  uuid.UUID
}

// startedCall has Alice start a Talk from a conversation with Bob.
func startedCall(t *testing.T, ring time.Duration) *callWorldDB {
	t.Helper()
	w := newTalksWorld(t)
	calls := NewDMCalls(&DMCalls{DB: w.p, Events: w.rec, Ring: ring})
	t.Cleanup(calls.stop)
	h := &Handler{DB: w.p, Events: w.rec, Perms: perm.NewService(w.p), Talks: w.mgr, Calls: calls}
	dm, _, err := CreateDM(context.Background(), w.p, NewDM{Creator: w.alice.ID, Others: []uuid.UUID{w.bob.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.startDMTalk(httptest.NewRecorder(), talkRequest(w.alice, ``, map[string]string{"id": dm.String()})); err != nil {
		t.Fatal(err)
	}
	c := &callWorldDB{talksWorld: w, h: h, calls: calls, dm: dm}
	if call := calls.Active(dm); call != nil {
		c.talk = call.TalkID
	}
	return c
}

// announce runs the queued announcements and returns the events of type typ.
func (c *callWorldDB) announce(typ string) []events.Event {
	for c.calls.announceNext(context.Background()) {
	}
	var out []events.Event
	for _, e := range c.rec.Snapshot() {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func (c *callWorldDB) missedMessages(t *testing.T) int {
	t.Helper()
	var n int
	err := c.p.QueryRow(context.Background(),
		`SELECT count(*) FROM messages WHERE channel_id = $1 AND embeds->0->>'call' = 'missed' AND user_id = $2`, c.dm, c.alice.ID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStartingATalkFromAConversationRingsIt(t *testing.T) {
	c := startedCall(t, time.Hour)
	rings := c.announce(events.DMCallRing)
	if len(rings) != 1 || rings[0].ChannelID != c.dm {
		t.Fatalf("rings = %+v", rings)
	}
	ring := rings[0].Payload.(DMCallRing)
	if ring.TalkID == uuid.Nil || ring.CallerID != c.alice.ID || ring.TalkName != "Talk 1" || ring.MessageID == uuid.Nil {
		t.Fatalf("ring = %+v", ring)
	}
	listed, err := ListDMs(context.Background(), c.p, c.bob.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.h.markCalls(listed)
	if len(listed) != 1 || listed[0].Call == nil || !listed[0].Call.Ringing || listed[0].Call.TalkID != ring.TalkID {
		t.Fatalf("the conversation list carries the call: %+v", listed)
	}
}

func TestACallTheCallerHangsUpOnLeavesAMissedCall(t *testing.T) {
	c := startedCall(t, time.Hour)
	c.calls.OnPresence(c.alice.ID, c.talk, true)
	c.calls.OnPresence(c.alice.ID, c.talk, false)
	ends := c.announce(events.DMCallEnd)
	if len(ends) != 1 {
		t.Fatalf("ends = %+v", ends)
	}
	end := ends[0].Payload.(DMCallEnd)
	if end.Reason != CallCancelled || end.Active || end.MessageID == nil {
		t.Fatalf("end = %+v", end)
	}
	if n := c.missedMessages(t); n != 1 {
		t.Fatalf("missed call messages = %d", n)
	}
	var created int
	for _, e := range c.rec.Snapshot() {
		if m, ok := e.Payload.(*Message); ok && e.Type == events.MessageCreate && m.ID == *end.MessageID && e.ChannelID == c.dm {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("the missed call is announced as a new message: %d", created)
	}
	if c.calls.Active(c.dm) != nil {
		t.Fatal("a cancelled call is over")
	}
}

func TestACallNobodyAnswersLeavesAMissedCallButStaysOpenForLateJoiners(t *testing.T) {
	c := startedCall(t, 30*time.Millisecond)
	c.calls.OnPresence(c.alice.ID, c.talk, true)
	deadline := time.Now().Add(5 * time.Second)
	for c.missedMessagesOrZero() == 0 && time.Now().Before(deadline) {
		for c.calls.announceNext(context.Background()) {
		}
		time.Sleep(10 * time.Millisecond)
	}
	ends := c.announce(events.DMCallEnd)
	if len(ends) != 1 {
		t.Fatalf("ends = %+v", ends)
	}
	end := ends[0].Payload.(DMCallEnd)
	if end.Reason != CallTimeout || !end.Active || end.MessageID == nil {
		t.Fatalf("end = %+v", end)
	}
	if call := c.calls.Active(c.dm); call == nil || call.Ringing {
		t.Fatalf("the caller is still waiting in the Talk: %+v", call)
	}
	// Bob joins late: nothing rings any more and no second record is written.
	c.calls.OnPresence(c.bob.ID, c.talk, true)
	if got := len(c.announce(events.DMCallEnd)); got != 1 {
		t.Fatalf("end events = %d", got)
	}
	if n := c.missedMessages(t); n != 1 {
		t.Fatalf("missed call messages = %d", n)
	}
}

func (c *callWorldDB) missedMessagesOrZero() int {
	var n int
	_ = c.p.QueryRow(context.Background(),
		`SELECT count(*) FROM messages WHERE channel_id = $1 AND embeds->0->>'call' = 'missed'`, c.dm).Scan(&n)
	return n
}

func TestAnAnsweredCallLeavesNoMissedCall(t *testing.T) {
	c := startedCall(t, time.Hour)
	c.calls.OnPresence(c.alice.ID, c.talk, true)
	c.calls.OnPresence(c.bob.ID, c.talk, true)
	ends := c.announce(events.DMCallEnd)
	if len(ends) != 1 || ends[0].Payload.(DMCallEnd).Reason != CallAnswered || ends[0].Payload.(DMCallEnd).MessageID != nil {
		t.Fatalf("ends = %+v", ends)
	}
	c.calls.OnPresence(c.alice.ID, c.talk, false)
	c.calls.OnPresence(c.bob.ID, c.talk, false)
	if n := c.missedMessages(t); n != 0 {
		t.Fatalf("missed call messages = %d", n)
	}
	if ends := c.announce(events.DMCallEnd); len(ends) != 2 || ends[1].Payload.(DMCallEnd).Reason != CallEnded {
		t.Fatalf("ends = %+v", ends)
	}
}

func TestOnlyTheServerWritesTheCallMarks(t *testing.T) {
	in := InEmbed{Title: "x"}
	embeds, err := SanitizeEmbeds([]InEmbed{in})
	if err != nil || len(embeds) != 1 || embeds[0].Call != "" || embeds[0].TalkID != nil {
		t.Fatalf("embeds = %+v, %v", embeds, err)
	}
}

func TestACallNeedsNoLobbyAndItsTalkBelongsToTheConversation(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	if _, err := w.p.Exec(ctx, `DELETE FROM channels WHERE is_lobby`); err != nil {
		t.Fatal(err)
	}
	h := &Handler{DB: w.p, Events: w.rec, Perms: perm.NewService(w.p), Talks: w.mgr}
	dm, _, err := CreateDM(ctx, w.p, NewDM{Creator: w.alice.ID, Others: []uuid.UUID{w.bob.ID}})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := h.startDMTalk(rec, talkRequest(w.alice, ``, map[string]string{"id": dm.String()})); err != nil {
		t.Fatalf("a fresh community has no lobby: %v", err)
	}
	var talk uuid.UUID
	var owner, lobby *uuid.UUID
	var name string
	var temp bool
	err = w.p.QueryRow(ctx, `SELECT id, dm_id, lobby_id, is_temp, name FROM channels WHERE dm_id IS NOT NULL`).Scan(&talk, &owner, &lobby, &temp, &name)
	if err != nil {
		t.Fatal(err)
	}
	if owner == nil || *owner != dm || lobby != nil || !temp || name != "Talk 1" {
		t.Fatalf("talk of the conversation: dm %v lobby %v temp %v name %q", owner, lobby, temp, name)
	}
	if leader, _ := w.mgr.Leader(talk); leader != w.alice.ID {
		t.Fatalf("the starter leads: %v", leader)
	}
	// Only the two members may see and join it; a stranger and the Owner may not.
	carol := newTalksUser(t, w.p, "carol_t")
	h.Perms.Invalidate()
	for who, want := range map[*auth.User]bool{w.alice: true, w.bob: true, carol: false} {
		got, err := h.Perms.Channel(ctx, who.Subject(), talk)
		if err != nil {
			t.Fatal(err)
		}
		if got.Has(perm.ViewChannel|perm.Connect) != want {
			t.Errorf("%s may use the Talk: %v, want %v", who.Username, !want, want)
		}
	}
	if got, _ := h.Perms.Channel(ctx, perm.Subject{ID: uuid.New(), Owner: true}, talk); got != 0 {
		t.Errorf("the Owner holds %b in a Talk of a conversation they are not in", got)
	}
	// Empty, it goes away like any temporary Talk.
	w.mgr.removeIfEmpty(talk)
	if w.exists(t, talk) {
		t.Fatal("an empty Talk of a conversation should be removed")
	}
	// A second call in the same conversation reuses the lowest free number.
	if err := h.startDMTalk(httptest.NewRecorder(), talkRequest(w.alice, ``, map[string]string{"id": dm.String()})); err != nil {
		t.Fatal(err)
	}
	if err := w.p.QueryRow(ctx, `SELECT name FROM channels WHERE dm_id = $1`, dm).Scan(&name); err != nil || name != "Talk 1" {
		t.Fatalf("second call: %q, %v", name, err)
	}
}

func TestTheTalkOfAConversationGoesWithIt(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	dm, _, err := CreateDM(ctx, w.p, NewDM{Creator: w.alice.ID, Others: []uuid.UUID{w.bob.ID}})
	if err != nil {
		t.Fatal(err)
	}
	talk, err := w.mgr.OpenForDM(ctx, w.alice.ID, dm)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.Exec(ctx, `DELETE FROM channels WHERE id = $1`, dm); err != nil {
		t.Fatal(err)
	}
	if w.exists(t, talk.ID) {
		t.Fatal("deleting the conversation deletes its Talk")
	}
}

func TestACallerWhoMayNotConnectGetsNoTalkAndNoRing(t *testing.T) {
	c := startedCallWorld(t)
	ctx := context.Background()
	var everyone uuid.UUID
	_ = c.p.QueryRow(ctx, `SELECT id FROM roles WHERE is_everyone`).Scan(&everyone)
	if _, err := c.p.Exec(ctx, `UPDATE roles SET permissions = permissions & ~$2::bigint WHERE id = $1`, everyone, int64(perm.Connect)); err != nil {
		t.Fatal(err)
	}
	c.h.Perms.Invalidate()
	before := len(c.rec.Snapshot())
	err := c.h.startDMTalk(httptest.NewRecorder(), talkRequest(c.alice, ``, map[string]string{"id": c.dm.String()}))
	if err == nil {
		t.Fatal("a member who cannot join Talks started one")
	}
	var n int
	_ = c.p.QueryRow(ctx, `SELECT count(*) FROM channels WHERE dm_id = $1`, c.dm).Scan(&n)
	if n != 0 || len(c.rec.Snapshot()) != before {
		t.Fatalf("talks %d, events %d -> %d", n, before, len(c.rec.Snapshot()))
	}
}

// startedCallWorld is a conversation of Alice and Bob without a call.
func startedCallWorld(t *testing.T) *callWorldDB {
	t.Helper()
	w := newTalksWorld(t)
	calls := NewDMCalls(&DMCalls{DB: w.p, Events: w.rec, Ring: time.Hour})
	t.Cleanup(calls.stop)
	h := &Handler{DB: w.p, Events: w.rec, Perms: perm.NewService(w.p), Talks: w.mgr, Calls: calls}
	dm, _, err := CreateDM(context.Background(), w.p, NewDM{Creator: w.alice.ID, Others: []uuid.UUID{w.bob.ID}})
	if err != nil {
		t.Fatal(err)
	}
	return &callWorldDB{talksWorld: w, h: h, calls: calls, dm: dm}
}

func TestTheFilesOfAConversationsTalkGoWhenTheConversationDoes(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	carol := newTalksUser(t, w.p, "carol_t")
	dm, _, err := CreateDM(ctx, w.p, NewDM{Creator: w.alice.ID, Others: []uuid.UUID{w.bob.ID, carol.ID}})
	if err != nil {
		t.Fatal(err)
	}
	talk, err := w.mgr.OpenForDM(ctx, w.alice.ID, dm)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := InsertMessage(ctx, w.p, nil, NewMessage{ChannelID: talk.ID, UserID: w.alice.ID, Content: "look"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.Exec(ctx, `
		INSERT INTO media (uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, $3, 'bucket', 'talk-file', 'a.png', 'image/png', 10)`, w.alice.ID, talk.ID, msg); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, u := range []*auth.User{w.alice, w.bob, carol} {
		_, got, err := LeaveDM(ctx, w.p, dm, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, got...)
	}
	if len(keys) != 1 || keys[0] != "talk-file" {
		t.Fatalf("the Talk's file is handed back for deletion: %v", keys)
	}
	if w.exists(t, talk.ID) {
		t.Fatal("the Talk is gone with the conversation")
	}
}

func TestTheLastMemberLeavingAGroupClosesTheRoomOfItsTalk(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	carol := newTalksUser(t, w.p, "carol_t")
	dm, _, err := CreateDM(ctx, w.p, NewDM{Creator: w.alice.ID, Others: []uuid.UUID{w.bob.ID, carol.ID}})
	if err != nil {
		t.Fatal(err)
	}
	talk, err := w.mgr.OpenForDM(ctx, w.alice.ID, dm)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{DB: w.p, Events: w.rec, Perms: perm.NewService(w.p), Talks: w.mgr, Voice: voiceCloser{w}}
	leave := func(u *auth.User) {
		t.Helper()
		params := map[string]string{"id": dm.String(), "user": "@me"}
		if err := h.leaveDM(httptest.NewRecorder(), talkRequest(u, ``, params)); err != nil {
			t.Fatal(err)
		}
	}
	leave(w.alice)
	leave(w.bob)
	if w.closed.Load() != 0 || !w.exists(t, talk.ID) {
		t.Fatal("the Talk stays while somebody is in the conversation")
	}
	leave(carol)
	if w.closed.Load() != 2 { // the conversation's own id and its Talk
		t.Fatalf("rooms closed: %d", w.closed.Load())
	}
	if w.exists(t, talk.ID) {
		t.Fatal("the Talk goes with the conversation")
	}
}

func TestLeavingASharedTalkOnlyTheCallerWasInCancelsTheRingAndLeavesAMissedCall(t *testing.T) {
	c := startedCallWorld(t)
	talk := uuid.New()
	c.calls.Members = func(uuid.UUID) []uuid.UUID { return []uuid.UUID{c.alice.ID} }
	c.calls.Begin(c.dm, talk, c.alice.ID, uuid.New(), "Talk 1")
	if len(c.announce(events.DMCallRing)) != 1 {
		t.Fatal("it rings: nobody but the caller is in")
	}
	c.calls.OnPresence(c.alice.ID, talk, false)
	ends := c.announce(events.DMCallEnd)
	if len(ends) != 1 {
		t.Fatalf("ends = %+v", ends)
	}
	if end := ends[0].Payload.(DMCallEnd); end.Reason != CallCancelled || end.Active || end.MessageID == nil {
		t.Fatalf("end = %+v", end)
	}
	if n := c.missedMessages(t); n != 1 {
		t.Fatalf("missed call messages = %d", n)
	}
}
