package ws

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/metrics"
)

func frameOf(eventType string, payloadBytes int) []byte {
	pad := strings.Repeat("x", payloadBytes)
	return []byte(`{"type":"` + eventType + `","payload":{"t":1,"pad":"` + pad + `"}}`)
}

func TestPeekEventTypeReadsOnlyAFirstTypeMember(t *testing.T) {
	for frame, want := range map[string]string{
		`{"type":"typing","payload":{}}`:        "typing",
		`{ "type" : "ping" , "payload":1}`:      "ping",
		`{"payload":{"a":1},"type":"typing"}`:   "",
		`{"type":5}`:                            "",
		`["type","ping"]`:                       "",
		`not json`:                              "",
		``:                                      "",
		`{"type":"` + strings.Repeat("a", 5000): "",
	} {
		got, ok := peekEventType([]byte(frame))
		if got != want || ok != (want != "") {
			t.Errorf("peekEventType(%.40q) = %q, %v", frame, got, ok)
		}
	}
}

func TestPayloadCapsPerEventType(t *testing.T) {
	if payloadCap("typing") != smallEventPayload || payloadCap("voice_speaking") != smallEventPayload {
		t.Fatal("ordinary events must stay small")
	}
	for _, typ := range []string{"webrtc_answer", "media_handshake"} {
		if payloadCap(typ) != maxMessageBytes {
			t.Fatalf("%s cap = %d", typ, payloadCap(typ))
		}
	}
	if payloadCap("webrtc_candidate") != 2<<10 || payloadCap("webrtc_diag") != 4<<10 {
		t.Fatal("candidate and diagnostic caps changed")
	}
}

func TestEventBudgetDropsOversizedOrdinaryPayloads(t *testing.T) {
	var b eventBudget
	now := time.Now()
	if !b.allowEvent("typing", smallEventPayload, now) {
		t.Fatal("a payload at the cap was refused")
	}
	if b.allowEvent("typing", smallEventPayload+1, now) {
		t.Fatal("a payload over the cap was admitted")
	}
}

// Oversized frames of ordinary events are dropped before they are decoded or
// reach a handler; frames of the large event types and sane ones still pass.
func TestDispatchDropsOversizedFramesBeforeDecoding(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	c := testClient(h, auth.User{ID: uuid.New()})
	drainTypes(t, c)
	var budget eventBudget
	dropped := metrics.WSDropped.With("oversize_event").Load()
	for range 40 {
		c.dispatchFrame(&budget, websocket.TextMessage, frameOf("typing", 60<<10))
		c.dispatchFrame(&budget, websocket.TextMessage, frameOf("ping", 60<<10))
	}
	if got := metrics.WSDropped.With("oversize_event").Load() - dropped; got != 80 {
		t.Fatalf("oversize drops = %d, want 80", got)
	}
	if len(c.send) != 0 {
		t.Fatal("an oversized ping was still answered")
	}
	c.dispatchFrame(&budget, websocket.TextMessage, frameOf("ping", 100))
	if len(c.send) != 1 {
		t.Fatal("a normal ping was not answered")
	}
	if frameTooLarge(frameOf("webrtc_answer", 50<<10)) || !frameTooLarge(frameOf("webrtc_candidate", 5<<10)) {
		t.Fatal("answers may be large, candidates may not")
	}
	// A frame with the type last is judged after decoding, by the budget.
	late, _ := json.Marshal(map[string]any{"payload": map[string]string{"pad": strings.Repeat("x", 8<<10)}, "type": "ping"})
	c.dispatchFrame(&budget, websocket.TextMessage, late)
	if len(c.send) != 1 {
		t.Fatal("an oversized ping with its type last was answered")
	}
}

func TestUserBudgetIsSharedAcrossConnections(t *testing.T) {
	var u userBudgets
	id, other := uuid.New(), uuid.New()
	now := time.Now()
	allowed := 0
	for range 3 * maxEventsPerSec {
		if u.allowEvent(id, "typing", 10, now) {
			allowed++
		}
	}
	if allowed != maxUserEventsPerSec {
		t.Fatalf("an account got %d events in a second, want %d", allowed, maxUserEventsPerSec)
	}
	if !u.allowEvent(other, "typing", 10, now) {
		t.Fatal("another account was limited")
	}
	if !u.allowEvent(id, "typing", 10, now.Add(time.Second)) {
		t.Fatal("the window did not roll")
	}
	for range 3 * maxFramesPerSec {
		u.allowFrame(id, now.Add(2*time.Second))
	}
	if u.allowFrame(id, now.Add(2*time.Second)) {
		t.Fatal("frame cap not applied")
	}
	u.forget(id)
	if !u.allowFrame(id, now.Add(2*time.Second)) {
		t.Fatal("forgotten account kept its window")
	}
}

func TestUserHandshakeBudgetCoversAllSocketsOfTheAccount(t *testing.T) {
	var u userBudgets
	id := uuid.New()
	now := time.Now()
	allowed := 0
	// Two sockets would each get maxHandshakesPerSec; together they share it.
	for range 2 * maxHandshakesPerSec {
		if u.allowEvent(id, handshakeEvent, 1<<10, now) {
			allowed++
		}
	}
	if allowed != maxHandshakesPerSec {
		t.Fatalf("account relayed %d handshakes in a second, want %d", allowed, maxHandshakesPerSec)
	}
	if u.allowEvent(id, handshakeEvent, maxHandshakeBytesPerSec, now.Add(time.Second)) != true {
		t.Fatal("one big handshake at the byte budget must pass")
	}
	if u.allowEvent(id, handshakeEvent, 1, now.Add(time.Second)) {
		t.Fatal("bytes beyond the shared budget were admitted")
	}
	if !u.allowEvent(id, "webrtc_answer", 10, now) {
		t.Fatal("signaling is metered per connection only")
	}
}

func TestUserBudgetSweepsIdleAccounts(t *testing.T) {
	var u userBudgets
	now := time.Now()
	for range 50 {
		u.allowFrame(uuid.New(), now)
	}
	u.allowFrame(uuid.New(), now.Add(2*userBudgetSweepEvery))
	if len(u.m) != 1 {
		t.Fatalf("%d windows kept after the sweep, want 1", len(u.m))
	}
}
