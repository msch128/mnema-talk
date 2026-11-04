package sfu

import (
	"sync"
	"testing"
	"time"
)

func TestNewestDeliversInOrderAndCoalesces(t *testing.T) {
	var out newest[int]
	var mu sync.Mutex
	var got []int
	block := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	deliver := func(v int) {
		once.Do(func() { close(started) })
		<-block
		mu.Lock()
		got = append(got, v)
		mu.Unlock()
	}

	// The first value is being delivered when more arrive: the sender never
	// waits, and only the newest of the later ones is delivered after it.
	out.send(1, deliver)
	<-started
	for v := 2; v <= 9; v++ {
		out.send(v, deliver)
	}
	close(block)
	waitSFUCondition(t, "both deliveries", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) >= 2
	})
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != 1 || got[1] != 9 {
		t.Fatalf("delivered %v, want [1 9]", got)
	}
}

func TestNewestRestartsAfterGoingIdle(t *testing.T) {
	var out newest[string]
	got := make(chan string, 4)
	for _, v := range []string{"a", "b", "c"} {
		out.send(v, func(s string) { got <- s })
		select {
		case s := <-got:
			if s != v {
				t.Fatalf("got %q, want %q", s, v)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%q never delivered", v)
		}
		// Let the goroutine find nothing pending and stop, so the next send
		// has to start a new one.
		waitSFUCondition(t, "the delivery goroutine to stop", func() bool {
			out.mu.Lock()
			defer out.mu.Unlock()
			return !out.running
		})
	}
}
