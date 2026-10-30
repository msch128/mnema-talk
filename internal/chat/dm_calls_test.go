package chat

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
)

type callWorld struct {
	calls                       *DMCalls
	rec                         *events.Recorder
	dm, talk, caller, callee, m uuid.UUID
}

func newCallWorld() *callWorld {
	rec := &events.Recorder{}
	return &callWorld{
		calls: NewDMCalls(&DMCalls{Events: rec, Ring: time.Hour}), rec: rec,
		dm: uuid.New(), talk: uuid.New(), caller: uuid.New(), callee: uuid.New(), m: uuid.New(),
	}
}

// heard announces everything queued and returns the events it published.
func (w *callWorld) heard() []events.Event {
	for w.calls.announceNext(context.Background()) {
	}
	out := w.rec.Snapshot()
	w.rec.Events = nil
	return out
}

func (w *callWorld) begin() { w.calls.Begin(w.dm, w.talk, w.caller, w.m, "Talk 1") }

func TestACallRingsTheConversation(t *testing.T) {
	w := newCallWorld()
	t.Cleanup(w.calls.stop)
	w.begin()
	got := w.heard()
	if len(got) != 1 || got[0].Type != events.DMCallRing || got[0].ChannelID != w.dm {
		t.Fatalf("events = %+v", got)
	}
	ring, ok := got[0].Payload.(DMCallRing)
	if !ok || ring.TalkID != w.talk || ring.CallerID != w.caller || ring.MessageID != w.m || ring.TalkName != "Talk 1" || ring.RingSeconds != 3600 {
		t.Fatalf("ring = %+v", got[0].Payload)
	}
	if call := w.calls.Active(w.dm); call == nil || !call.Ringing || call.RingSeconds < 3599 || call.TalkID != w.talk {
		t.Fatalf("active = %+v", call)
	}
	w.begin()
	if got := w.heard(); len(got) != 0 {
		t.Fatalf("a call that rings is not rung again: %+v", got)
	}
}

func TestTheCallerEnteringTheTalkDoesNotAnswerIt(t *testing.T) {
	w := newCallWorld()
	t.Cleanup(w.calls.stop)
	w.begin()
	w.heard()
	w.calls.OnPresence(w.caller, w.talk, true)
	w.calls.OnPresence(w.callee, uuid.New(), true) // another Talk
	if got := w.heard(); len(got) != 0 {
		t.Fatalf("events = %+v", got)
	}
	if call := w.calls.Active(w.dm); call == nil || !call.Ringing {
		t.Fatalf("active = %+v", call)
	}
}

func TestAnsweringStopsTheRingAndTheLastToLeaveEndsTheCall(t *testing.T) {
	w := newCallWorld()
	t.Cleanup(w.calls.stop)
	w.begin()
	w.calls.OnPresence(w.caller, w.talk, true)
	w.heard()

	w.calls.OnPresence(w.callee, w.talk, true)
	got := w.heard()
	if len(got) != 1 || got[0].Type != events.DMCallEnd {
		t.Fatalf("events = %+v", got)
	}
	end := got[0].Payload.(DMCallEnd)
	if end.Reason != CallAnswered || !end.Active || end.UserID == nil || *end.UserID != w.callee || end.ConversationID != w.dm || end.TalkID != w.talk {
		t.Fatalf("end = %+v", end)
	}
	if call := w.calls.Active(w.dm); call == nil || call.Ringing || call.RingSeconds != 0 {
		t.Fatalf("an answered call goes on without ringing: %+v", call)
	}

	w.calls.OnPresence(w.caller, w.talk, false)
	if got := w.heard(); len(got) != 0 {
		t.Fatalf("somebody is still in the Talk: %+v", got)
	}
	w.calls.OnPresence(w.callee, w.talk, false)
	got = w.heard()
	if len(got) != 1 {
		t.Fatalf("events = %+v", got)
	}
	if end := got[0].Payload.(DMCallEnd); end.Reason != CallEnded || end.Active {
		t.Fatalf("end = %+v", end)
	}
	if w.calls.Active(w.dm) != nil {
		t.Fatal("an ended call is forgotten")
	}
}

func TestACallToAnotherTalkReplacesTheRingingOne(t *testing.T) {
	w := newCallWorld()
	t.Cleanup(w.calls.stop)
	w.begin()
	w.heard()
	other := uuid.New()
	w.calls.Begin(w.dm, other, w.caller, uuid.New(), "Talk 2")
	got := w.heard()
	if len(got) != 2 || got[0].Type != events.DMCallEnd || got[1].Type != events.DMCallRing {
		t.Fatalf("events = %+v", got)
	}
	if end := got[0].Payload.(DMCallEnd); end.TalkID != w.talk || end.Reason != CallCancelled || end.Active {
		t.Fatalf("end = %+v", end)
	}
	if call := w.calls.Active(w.dm); call == nil || call.TalkID != other {
		t.Fatalf("active = %+v", call)
	}
	// The replaced Talk's presence no longer concerns the conversation.
	w.calls.OnPresence(w.callee, w.talk, true)
	if got := w.heard(); len(got) != 0 {
		t.Fatalf("events = %+v", got)
	}
}

func TestWithoutTrackingCallsAreIgnored(t *testing.T) {
	var none *DMCalls
	none.Begin(uuid.New(), uuid.New(), uuid.New(), uuid.New(), "x")
	none.OnPresence(uuid.New(), uuid.New(), true)
	if none.Active(uuid.New()) != nil {
		t.Fatal("a nil tracker has no calls")
	}
}

func TestSharingATalkSomebodyIsInAlreadyRingsNobody(t *testing.T) {
	w := newCallWorld()
	t.Cleanup(w.calls.stop)
	w.calls.Members = func(uuid.UUID) []uuid.UUID { return []uuid.UUID{w.caller, w.callee} }
	w.begin()
	if got := w.heard(); len(got) != 0 {
		t.Fatalf("nothing rings for a Talk that is occupied: %+v", got)
	}
	if call := w.calls.Active(w.dm); call == nil || call.Ringing {
		t.Fatalf("it is tracked as a call going on: %+v", call)
	}
	// The last one out ends it, though neither joined after it began.
	w.calls.OnPresence(w.caller, w.talk, false)
	w.calls.OnPresence(w.callee, w.talk, false)
	if got := w.heard(); len(got) != 1 || got[0].Payload.(DMCallEnd).Reason != CallEnded {
		t.Fatalf("events = %+v", got)
	}
}

func TestSharingATalkOnlyTheCallerIsInRings(t *testing.T) {
	w := newCallWorld()
	t.Cleanup(w.calls.stop)
	w.calls.Members = func(uuid.UUID) []uuid.UUID { return []uuid.UUID{w.caller} }
	w.begin()
	if got := w.heard(); len(got) != 1 || got[0].Type != events.DMCallRing {
		t.Fatalf("events = %+v", got)
	}
}

func TestACallWithPeopleInItIsNotAnnouncedAsOverByTheNextOne(t *testing.T) {
	w := newCallWorld()
	t.Cleanup(w.calls.stop)
	w.begin()
	w.calls.OnPresence(w.caller, w.talk, true)
	w.calls.OnPresence(w.callee, w.talk, true)
	w.heard()
	other := uuid.New()
	w.calls.Begin(w.dm, other, w.caller, uuid.New(), "Talk 2")
	got := w.heard()
	if len(got) != 1 || got[0].Type != events.DMCallRing {
		t.Fatalf("only the new call is announced, the occupied Talk is not ended: %+v", got)
	}
	if call := w.calls.Active(w.dm); call == nil || call.TalkID != other {
		t.Fatalf("active = %+v", call)
	}
}
