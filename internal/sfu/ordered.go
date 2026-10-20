package sfu

import "sync"

// newest delivers values to a callback in order, one at a time, on a goroutine
// of its own, and drops a value that a newer one replaced before it was
// delivered. The SFU announces changes from its forwarding goroutines, which
// must not wait for the WebSocket hub (the hub may hold its lock while it calls
// into the SFU), and a client must never end up with an older announcement
// than the latest one, which separate goroutines per announcement could not
// promise.
type newest[T any] struct {
	mu      sync.Mutex
	value   T
	pending bool
	running bool
}

// send queues v for fn. Values sent while fn is running are coalesced.
func (n *newest[T]) send(v T, fn func(T)) {
	n.mu.Lock()
	n.value, n.pending = v, true
	if n.running {
		n.mu.Unlock()
		return
	}
	n.running = true
	n.mu.Unlock()
	go func() {
		for {
			next, ok := n.drainNext()
			if !ok {
				return
			}
			fn(next)
		}
	}()
}

func (n *newest[T]) drainNext() (T, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.pending {
		n.running = false
		var zero T
		return zero, false
	}
	next := n.value
	n.pending = false
	return next, true
}
