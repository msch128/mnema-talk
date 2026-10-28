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
	for _, typ := range []string{"webrtc_answer", "webrtc_candidate", "media_handshake"} {
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

// A handshake fans out to the whole room, so it has its own message and byte
// budget and cannot eat the signaling budget.
func TestEventBudgetMetersHandshakes(t *testing.T) {
	var b eventBudget
	now := time.Now()
	allowed := 0
	for range maxHandshakesPerSec + 20 {
		if b.allowEvent("media_handshake", 1<<10, now) {
			allowed++
		}
	}
	if allowed != maxHandshakesPerSec {
		t.Fatalf("allowed %d handshakes, want %d", allowed, maxHandshakesPerSec)
	}
	for range maxSignalingPerSec {
		if !b.allowEvent("webrtc_candidate", 100, now) {
			t.Fatal("handshake flood consumed the signaling budget")
		}
	}
	later := now.Add(time.Second)
	if !b.allowEvent("media_handshake", 1<<10, later) {
		t.Fatal("handshake budget did not reset after a second")
	}
}

func TestEventBudgetCapsHandshakeBytes(t *testing.T) {
	var b eventBudget
	now := time.Now()
	// Welcome and commit, each at the largest wire size (32 KiB raw is about
	// 44 KiB of base64), plus a key package: a legitimate join in a big room.
	const wire = 45 << 10
	for i := range 3 {
		if !b.allowEvent("media_handshake", wire, now) {
			t.Fatalf("legitimate handshake %d dropped", i)
		}
	}
	allowed := 3
	for range maxHandshakesPerSec {
		if b.allowEvent("media_handshake", wire, now) {
			allowed++
		}
	}
	if max := maxHandshakeBytesPerSec / wire; allowed != max {
		t.Fatalf("allowed %d large handshakes, want %d", allowed, max)
	}
	if b.allowEvent("media_handshake", maxMessageBytes+1, now.Add(time.Second)) {
		t.Fatal("oversized handshake was allowed")
	}
}

// The per-type caps of payloadCaps are what the budget enforces, for
// signaling and handshake events as for ordinary ones.
func TestEventBudgetEnforcesPayloadCaps(t *testing.T) {
	now := time.Now()
	for _, typ := range []string{"webrtc_answer", "webrtc_candidate", "media_handshake", "webrtc_diag", "typing"} {
		limit := payloadCap(typ)
		var b eventBudget
		if !b.allowEvent(typ, limit, now) {
			t.Fatalf("%s at its cap was dropped", typ)
		}
		if b.allowEvent(typ, limit+1, now) {
			t.Fatalf("%s over its cap was allowed", typ)
		}
	}
}
