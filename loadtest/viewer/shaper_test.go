package main

import (
	"testing"
	"time"
)

func TestShaperQueuesThenDrops(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	// 800 kbit/s is 100 kB/s: a 1000 byte packet takes 10 ms.
	s := newShaper(800, 25*time.Millisecond)
	if w, ok := s.admit(now, 1000); !ok || w != 10*time.Millisecond {
		t.Fatalf("first packet: %v %v", w, ok)
	}
	// A second one arriving at once waits for the first.
	if w, ok := s.admit(now, 1000); !ok || w != 20*time.Millisecond {
		t.Fatalf("second packet: %v %v", w, ok)
	}
	if w, ok := s.admit(now, 1000); !ok || w != 30*time.Millisecond {
		t.Fatalf("third packet: %v %v", w, ok)
	}
	// The queue holds 25 ms: the next one is dropped.
	if _, ok := s.admit(now, 1000); ok {
		t.Fatal("a packet was queued beyond the queue limit")
	}
	// Later, the link is free again.
	if w, ok := s.admit(now.Add(time.Second), 1000); !ok || w != 10*time.Millisecond {
		t.Fatalf("after the queue drained: %v %v", w, ok)
	}
}
