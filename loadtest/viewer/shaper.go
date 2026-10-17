package main

import (
	"sync"
	"time"
)

// shaper models a link of a fixed rate with a drop-tail queue, for BW_KBPS:
// every packet leaves after the ones before it, taking size/rate, and a
// packet that would wait longer than the queue holds is dropped. A link that
// is too slow for a layer then shows what a real one does: the queue fills,
// the delay rises and packets are lost only once the queue is full.
type shaper struct {
	mu       sync.Mutex
	bytesPS  float64
	maxQueue time.Duration
	free     time.Time // when the link is free for the next packet
}

func newShaper(kbps int, maxQueue time.Duration) *shaper {
	return &shaper{bytesPS: float64(kbps) * 1000 / 8, maxQueue: maxQueue}
}

// admit takes a packet of n bytes arriving at now. It returns how long the
// packet is held on the link, or ok=false when the queue is full.
func (s *shaper) admit(now time.Time, n int) (wait time.Duration, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := now
	if s.free.After(now) {
		start = s.free
	}
	if start.Sub(now) > s.maxQueue {
		return 0, false
	}
	s.free = start.Add(time.Duration(float64(n) / s.bytesPS * float64(time.Second)))
	return s.free.Sub(now), true
}
