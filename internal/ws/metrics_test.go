package ws

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/metrics"
)

func TestBudgetCountsDropsAndAcceptedEvents(t *testing.T) {
	frames := metrics.WSDropped.With("flood_frame").Load()
	events := metrics.WSDropped.With("flood_event").Load()
	limited := metrics.MLSHandshakes.With("rate_limited").Load()
	received := metrics.WSEventsReceived.Load()

	var b eventBudget
	now := time.Now()
	for range maxFramesPerSec + 3 {
		b.allowFrame(now)
	}
	if got := metrics.WSDropped.With("flood_frame").Load() - frames; got != 3 {
		t.Fatalf("flood_frame = %d, want 3", got)
	}

	b = eventBudget{}
	for range maxEventsPerSec + 2 {
		b.allowEvent("typing", 10, now)
	}
	if got := metrics.WSDropped.With("flood_event").Load() - events; got != 2 {
		t.Fatalf("flood_event = %d, want 2", got)
	}
	if got := metrics.WSEventsReceived.Load() - received; got != maxEventsPerSec {
		t.Fatalf("received = %d, want %d", got, maxEventsPerSec)
	}

	b = eventBudget{}
	for range maxHandshakesPerSec + 1 {
		b.allowEvent(handshakeEvent, 10, now)
	}
	if got := metrics.MLSHandshakes.With("rate_limited").Load() - limited; got != 1 {
		t.Fatalf("rate_limited handshakes = %d, want 1", got)
	}
}

func TestDeliverCountsSentAndSlowClients(t *testing.T) {
	sent := metrics.WSEventsSent.Load()
	slow := metrics.WSDropped.With("slow_client").Load()
	c := newClient(nil, nil, auth.User{}, 0)
	c.send = make(chan []byte, 1)
	c.deliver([]byte("a"))
	c.deliver([]byte("b"))
	if metrics.WSEventsSent.Load()-sent != 1 || metrics.WSDropped.With("slow_client").Load()-slow != 1 {
		t.Fatal("delivery counters wrong")
	}
}

func TestMediaHandshakeCounters(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	sender := newClient(h, nil, auth.User{ID: uuid.New()}, 0)
	room := uuid.New()
	sender.setVoice(&room)
	relayed := metrics.MLSHandshakes.With("relayed").Load()
	rejected := metrics.MLSHandshakes.With("rejected").Load()
	bytes := metrics.MLSHandshakeBytes.Load()

	wire := base64.StdEncoding.EncodeToString([]byte("hello"))
	good, _ := json.Marshal(map[string]any{"channel_id": room, "wire": wire})
	other, _ := json.Marshal(map[string]any{"channel_id": uuid.New(), "wire": wire})
	badBase64, _ := json.Marshal(map[string]any{"channel_id": room, "wire": "!!!"})
	for _, p := range []string{string(good), string(other), string(badBase64), "{"} {
		sender.handleMediaHandshake(json.RawMessage(p))
	}
	if got := metrics.MLSHandshakes.With("relayed").Load() - relayed; got != 1 {
		t.Fatalf("relayed = %d, want 1", got)
	}
	if got := metrics.MLSHandshakes.With("rejected").Load() - rejected; got != 3 {
		t.Fatalf("rejected = %d, want 3", got)
	}
	if got := metrics.MLSHandshakeBytes.Load() - bytes; got != 5 {
		t.Fatalf("handshake bytes = %d, want 5", got)
	}
}
