package ws

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// A client that keeps draining a large greeting, only slowly, is served to
// the end: the greeting's wait measures a stall, not the greeting's duration.
func TestSlowButSteadilyDrainingClientReceivesTheWholeGreeting(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	c := testClient(h, auth.User{ID: uuid.New()})
	c.send = make(chan []byte, 8)

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-c.send:
			case <-stop:
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
	}()

	p := newPacerWait(c, 150*time.Millisecond)
	defer p.stop()
	start := time.Now()
	for i := 0; i < 60; i++ {
		if !p.send("ping", map[string]int{"n": i}) {
			t.Fatalf("greeting stopped at event %d after %v", i, time.Since(start))
		}
	}
	if time.Since(start) < 300*time.Millisecond {
		t.Fatal("the greeting did not outlast the stall budget; the test proves nothing")
	}
	if c.closing.Load() {
		t.Fatal("a draining client was dropped as too slow")
	}
}

// A client that drains nothing is still dropped once the budget passes.
func TestStuckClientIsDroppedFromTheGreeting(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	c := testClient(h, auth.User{ID: uuid.New()})
	c.send = make(chan []byte, 8)
	p := newPacerWait(c, 100*time.Millisecond)
	defer p.stop()
	for i := 0; i < 20; i++ {
		if !p.send("ping", i) {
			if !c.closing.Load() {
				t.Fatal("stopped without dropping the client")
			}
			return
		}
	}
	t.Fatal("a stuck client was never dropped")
}
