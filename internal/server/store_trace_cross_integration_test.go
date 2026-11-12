//go:build integration

package server

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestReadStateAnswersAndMessageEventsCross measures how often the answer of
// GET /api/read-state and the message events of the same moment reach a
// client in an order that leaves its badge wrong. The web client applies each
// message_create as it arrives (+1) and replaces its counts with an answer when
// the answer arrives (web/src/stores/chat/readState.ts), with no order between
// the two. Its count after the answer is the answer plus the events that come
// later; the server's count is all of them. So the badge is off by the answer
// minus the events that arrived before it. A writer posts while the observer
// asks; each request is one sample. (Events read between the answer and the
// counter read count as earlier: the share is a lower bound.)
//
// It logs the share and asserts nothing about it unless
// STORE_TRACE_CROSS_STRICT is set: a measurement for lane 5's H-L5-04.
// STORE_TRACE_CROSS=<n> sets the number of samples (default 30; keep the run
// under a minute of writing, the send rate limit allows about 500 messages).
func TestReadStateAnswersAndMessageEventsCross(t *testing.T) {
	samples := envInt("STORE_TRACE_CROSS", 30)
	a := newApp(t, true)
	owner := a.seedAdmin()
	a.createChannel(owner, "lobby", "text")
	ch := a.createChannel(owner, "busy", "text")
	observer := a.register(owner, "max")
	ws := observer.dialLiveWS(a.origin())

	var arrived atomic.Int64 // message_create events of the busy channel received so far
	go func() {
		for ev := range ws.events {
			if ev.Type == "message_create" && strings.Contains(string(ev.Payload), ch.String()) {
				arrived.Add(1)
			}
		}
	}()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if res := owner.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "busy " + strconv.Itoa(i)}); res.status != http.StatusCreated {
				t.Errorf("post: %d %s", res.status, res.body)
				return
			}
			time.Sleep(120 * time.Millisecond) // under the send rate limit for the whole run
		}
	})
	over, under := 0, 0
	for range samples {
		answer := observer.readState(t, ch).UnreadCount
		switch before := int(arrived.Load()); {
		case answer > before:
			over++ // the message is in the answer and its event is still to come: counted twice
		case answer < before:
			under++ // the event came first and the answer does not hold the message: dropped
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
	if arrived.Load() == 0 {
		t.Fatal("the writer's messages never reached the observer: the measurement measured nothing")
	}
	t.Logf("read-state answers that leave a wrong badge: %d of %d (%d count a message twice, %d drop one); %d messages", over+under, samples, over, under, arrived.Load())
	if os.Getenv("STORE_TRACE_CROSS_STRICT") != "" && over+under > 0 {
		t.Fatalf("%d of %d answers cross the message events", over+under, samples)
	}
}
