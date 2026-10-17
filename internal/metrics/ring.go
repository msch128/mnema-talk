package metrics

import "sync"

// Ring keeps the newest cap values; older ones are overwritten, so memory
// stays bounded no matter how long the process runs.
type Ring[T any] struct {
	mu   sync.Mutex
	buf  []T
	next int
	full bool
}

// NewRing makes a ring holding up to capacity values (at least 1).
func NewRing[T any](capacity int) *Ring[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring[T]{buf: make([]T, capacity)}
}

// Push appends v, dropping the oldest value when full.
func (r *Ring[T]) Push(v T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = v
	r.next++
	if r.next == len(r.buf) {
		r.next, r.full = 0, true
	}
}

// Snapshot returns the values oldest first.
func (r *Ring[T]) Snapshot() []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return append([]T(nil), r.buf[:r.next]...)
	}
	out := make([]T, 0, len(r.buf))
	out = append(out, r.buf[r.next:]...)
	return append(out, r.buf[:r.next]...)
}
