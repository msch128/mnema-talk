package mnema

// What a hostile or broken server can send must not exhaust the bot's memory,
// grow its goroutines without bound or pause every route for weeks.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestAFrameAboveTheReadLimitEndsTheConnectionUndelivered(t *testing.T) {
	g := newFakeGateway(t, 41250)
	c := g.client(t, Options{Intents: IntentGuildMessages})
	got := make(chan int, 1)
	c.OnDispatch(func(_ context.Context, name string, data json.RawMessage) {
		if name == "BIG" {
			got <- len(data)
		}
	})
	runClient(t, c)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s1")
	big := `{"op":0,"t":"BIG","s":2,"d":"` + strings.Repeat("x", MaxGatewayFrame) + `"}`
	// The client may close while the frame is still being written.
	_ = fc.ws.WriteMessage(websocket.TextMessage, []byte(big))
	// The client drops the connection and comes back on a new one.
	g.next(t)
	select {
	case n := <-got:
		t.Fatalf("a %d-byte dispatch was delivered", n)
	default:
	}
}

func TestADispatchFloodWithASlowHandlerKeepsGoroutinesBounded(t *testing.T) {
	g := newFakeGateway(t, 41250)
	c := g.client(t, Options{Intents: IntentGuildMessages, HandlerConcurrency: 1})
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	c.OnMessageCreate(func(ctx context.Context, _ *Message) {
		select {
		case <-block:
		case <-ctx.Done():
		}
	})
	runClient(t, c)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s1")
	before := runtime.NumGoroutine()
	// Written from a goroutine so that a client that stops reading cannot
	// block the test itself.
	go func() {
		for i := int64(0); i < 2000; i++ {
			if fc.ws.WriteJSON(map[string]any{"op": 0, "t": "MESSAGE_CREATE", "s": i + 2, "d": map[string]any{"id": "m", "channel_id": "c", "content": "x"}}) != nil {
				return
			}
		}
	}()
	time.Sleep(500 * time.Millisecond)
	if grown := runtime.NumGoroutine() - before; grown > dispatchQueueFactor+10 {
		t.Fatalf("goroutines grew by %d with one blocked handler", grown)
	}
}

func TestARetryAfterBeyondTheCapFailsAtOnceAndPausesNothing(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n == 1 {
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"message": "You are being rate limited.", "retry_after": 3e6, "global": true})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": "u"})
	}))
	t.Cleanup(srv.Close)
	r, err := NewREST(Options{BaseURL: srv.URL, Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = r.Me(ctx)
	var re *RESTError
	if !errors.As(err, &re) || re.Status != http.StatusTooManyRequests || re.RetryAfter < MaxRetryAfter {
		t.Fatalf("first request: %v", err)
	}
	if _, err := r.Channels(ctx); err != nil {
		t.Fatalf("an unrelated later request: %v", err)
	}
	if n != 2 {
		t.Fatalf("requests seen: %d, want 2", n)
	}
}

func TestRetryAfterSecondsThatDoNotFitADurationAreCapped(t *testing.T) {
	if got := seconds(1e300); got != time.Duration(1<<63-1) {
		t.Fatalf("seconds(1e300) = %v", got)
	}
	if got := seconds(-1); got != 0 {
		t.Fatalf("seconds(-1) = %v", got)
	}
}

func TestAFloodTheHandlersCannotKeepUpWithIsShedAndTheConnectionStays(t *testing.T) {
	g := newFakeGateway(t, 200)
	c := g.client(t, Options{Intents: IntentGuildMessages, HandlerConcurrency: 1})
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	c.OnMessageCreate(func(ctx context.Context, _ *Message) {
		select {
		case <-block:
		case <-ctx.Done():
		}
	})
	runClient(t, c)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s1")
	// Far more events than the queue holds, then heartbeats answered for five
	// intervals: a client that stopped reading would miss the ACKs and drop
	// the connection as a zombie.
	for i := int64(0); i < 5000; i++ {
		fc.dispatch(t, i+2, "MESSAGE_CREATE", map[string]any{"id": "m", "channel_id": "c", "content": "x"})
	}
	deadline := time.After(time.Second)
	for done := false; !done; {
		select {
		case f, ok := <-fc.frames:
			if !ok {
				t.Fatal("the client closed the connection")
			}
			if f.Op == opHeartbeat {
				fc.write(t, opHeartbeatACK, nil)
			}
		case <-deadline:
			done = true
		}
	}
	select {
	case <-g.conns:
		t.Fatal("the client reconnected: the flood stalled its read loop")
	default:
	}
}
