package linkpreview

import (
	"context"
	"sync"
)

type requesterKey struct{}

// WithRequester tags ctx with who asked for a fetch (a user ID), for the
// per-requester fetch limit. Untagged fetches only count against the
// server-wide limit.
func WithRequester(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requesterKey{}, id)
}

// acquire waits up to slotWait for the requester's slot, then for a
// server-wide one. The requester's slot is taken first, so a member waiting
// on their own limit never holds a server-wide slot meanwhile.
func (f *Fetcher) acquire(ctx context.Context) (func(), error) {
	ctx, cancel := context.WithTimeout(ctx, slotWait)
	defer cancel()
	id, _ := ctx.Value(requesterKey{}).(string)
	releaseRequester := func() {}
	if id != "" {
		r, err := f.requester.acquire(ctx, id)
		if err != nil {
			return nil, err
		}
		releaseRequester = r
	}
	select {
	case f.slots <- struct{}{}:
		return func() { <-f.slots; releaseRequester() }, nil
	case <-ctx.Done():
		releaseRequester()
		return nil, errBusy
	}
}

// requesterSlots holds a semaphore of maxFetchesPerRequester per requester
// with fetches in flight or waiting; idle requesters are dropped.
type requesterSlots struct {
	mu sync.Mutex
	m  map[string]*requesterSlot
}

type requesterSlot struct {
	sem  chan struct{}
	refs int
}

func (r *requesterSlots) acquire(ctx context.Context, id string) (func(), error) {
	r.mu.Lock()
	if r.m == nil {
		r.m = map[string]*requesterSlot{}
	}
	s := r.m[id]
	if s == nil {
		s = &requesterSlot{sem: make(chan struct{}, maxFetchesPerRequester)}
		r.m[id] = s
	}
	s.refs++
	r.mu.Unlock()
	done := func() {
		r.mu.Lock()
		if s.refs--; s.refs == 0 {
			delete(r.m, id)
		}
		r.mu.Unlock()
	}
	select {
	case s.sem <- struct{}{}:
		return func() { <-s.sem; done() }, nil
	case <-ctx.Done():
		done()
		return nil, errBusy
	}
}
