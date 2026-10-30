package main

import (
	"fmt"
	"testing"
)

func testRun() *run {
	r := newRun(config{})
	r.id, r.marker = "abc123", "lt abc123 "
	return r
}

func frameOf(typ, payload string) []byte {
	return []byte(fmt.Sprintf(`{"type":%q,"payload":%s}`, typ, payload))
}

func TestMarkedContentIsParsedAndForeignContentIgnored(t *testing.T) {
	seq, tail, ok := parseMarked("lt abc123 ", "lt abc123 42 xxxx")
	if !ok || seq != 42 || tail != "xxxx" {
		t.Fatalf("got %d %q %v", seq, tail, ok)
	}
	for _, bad := range []string{"hello", "lt other 1 x", "lt abc123 nope x", ""} {
		if _, _, ok := parseMarked("lt abc123 ", bad); ok {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestFramesFeedTheTrackerByKind(t *testing.T) {
	r := testRun()
	seq := r.track.newMessage(2)
	r.track.ids.Store("m-1", seq)

	create := frameOf("message_create", fmt.Sprintf(`{"id":"m-1","content":"lt abc123 %d xxxx"}`, seq))
	r.onFrame(create)
	r.onFrame(create)
	r.track.begin(kindEdit, seq, 2)
	r.onFrame(frameOf("message_update", fmt.Sprintf(`{"id":"m-1","content":"lt abc123 %d edit"}`, seq)))
	r.track.begin(kindReaction, seq, 2)
	r.onFrame(frameOf("message_reaction", `{"message_id":"m-1","reactions":[]}`))
	r.onFrame(frameOf("typing", `{"channel_id":"c","user_id":"u"}`))

	if tot := r.track.totals(kindMessage); tot.Got != 2 || tot.Lost != 0 {
		t.Errorf("messages = %+v", tot)
	}
	if tot := r.track.totals(kindEdit); tot.Got != 1 || tot.Lost != 1 {
		t.Errorf("edits = %+v", tot)
	}
	if tot := r.track.totals(kindReaction); tot.Got != 1 || tot.Lost != 1 {
		t.Errorf("reactions = %+v", tot)
	}
	if r.typingReceived.Load() != 1 {
		t.Errorf("typing = %d", r.typingReceived.Load())
	}
}

func TestOtherRunsMessagesAndLateUpdatesAreNotCounted(t *testing.T) {
	r := testRun()
	seq := r.track.newMessage(1)
	r.onFrame(frameOf("message_create", fmt.Sprintf(`{"id":"x","content":"lt zzzzzz %d xxxx"}`, seq)))
	// An update that is not the edit (an embed arrived) must not count as one.
	r.track.begin(kindEdit, seq, 1)
	r.onFrame(frameOf("message_update", fmt.Sprintf(`{"id":"x","content":"lt abc123 %d xxxx"}`, seq)))
	// A message_create carrying the edit text is not a new message either.
	r.onFrame(frameOf("message_create", fmt.Sprintf(`{"id":"x","content":"lt abc123 %d edit"}`, seq)))
	r.onFrame([]byte("not json"))
	if tot := r.track.totals(kindMessage); tot.Got != 0 {
		t.Errorf("messages = %+v", tot)
	}
	if tot := r.track.totals(kindEdit); tot.Got != 0 {
		t.Errorf("edits = %+v", tot)
	}
	if r.framesBad.Load() != 1 {
		t.Errorf("bad frames = %d", r.framesBad.Load())
	}
}

func TestPacerSpacesTheSlots(t *testing.T) {
	p := newPacer(600) // one every 100 ms
	if p.every.Milliseconds() != 100 {
		t.Fatalf("every = %v", p.every)
	}
	if newPacer(0).every != 0 {
		t.Fatal("zero rate must not pace")
	}
}
