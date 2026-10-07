package ws

import (
	"testing"
	"time"
)

// Ordinary events past their budget must not starve signaling: a dropped
// answer or candidate leaves a media connection half negotiated.
func TestEventBudgetKeepsSignalingApart(t *testing.T) {
	var b eventBudget
	now := time.Now()
	for i := range maxEventsPerSec {
		if !b.allowFrame(now) || !b.allowEvent("voice_speaking", 10, now) {
			t.Fatalf("event %d within budget was dropped", i)
		}
	}
	if b.allowFrame(now) && b.allowEvent("typing", 10, now) {
		t.Fatal("ordinary event over budget was allowed")
	}
	for _, typ := range []string{"webrtc_answer", "webrtc_candidate"} {
		if !b.allowFrame(now) || !b.allowEvent(typ, 100, now) {
			t.Fatalf("%s dropped after ordinary events used their budget", typ)
		}
	}
}

// A flood of candidates is capped like any other event: signaling has its
// own budget, not an unlimited one.
func TestEventBudgetCapsSignalingAndFrames(t *testing.T) {
	var b eventBudget
	now := time.Now()
	allowed := 0
	for range maxSignalingPerSec + 10 {
		if b.allowEvent("webrtc_candidate", 100, now) {
			allowed++
		}
	}
	if allowed != maxSignalingPerSec {
		t.Fatalf("allowed %d signaling events, want %d", allowed, maxSignalingPerSec)
	}
	frames := 0
	for range maxFramesPerSec + 10 {
		if b.allowFrame(now) {
			frames++
		}
	}
	if frames != maxFramesPerSec {
		t.Fatalf("allowed %d frames, want %d", frames, maxFramesPerSec)
	}
	// A new second starts a new budget.
	later := now.Add(time.Second)
	if !b.allowFrame(later) || !b.allowEvent("webrtc_candidate", 100, later) || !b.allowEvent("typing", 10, later) {
		t.Fatal("budget did not reset after a second")
	}
}

func TestEventBudgetDropsOversizedCandidates(t *testing.T) {
	var b eventBudget
	now := time.Now()
	if b.allowEvent("webrtc_candidate", 4<<10, now) {
		t.Fatal("oversized candidate was allowed")
	}
	if !b.allowEvent("webrtc_answer", 8<<10, now) {
		t.Fatal("answer of a normal SDP size was dropped")
	}
}
