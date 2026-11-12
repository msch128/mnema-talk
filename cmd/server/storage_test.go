package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
)

func TestLazyStoreRetriesUntilStorageIsUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	mem := media.NewMemoryStore()
	l := startLazyStore(ctx, func(context.Context) (media.Store, error) {
		if attempts.Add(1) < 3 {
			return nil, errors.New("connection refused")
		}
		return mem, nil
	}, time.Millisecond, 4*time.Millisecond)

	if l.Ready() == nil && attempts.Load() < 3 {
		t.Fatal("ready before init succeeded")
	}
	// Until ready, calls fail with 503 instead of a generic 500.
	if err := l.Upload(ctx, "k", bytes.NewReader(nil), "text/plain", 0); err != nil {
		if ae, ok := httpx.AsAPIError(err); !ok || ae.Status != http.StatusServiceUnavailable {
			t.Fatalf("not ready: err = %v", err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for l.Ready() != nil {
		if time.Now().After(deadline) {
			t.Fatalf("store never became ready: %v", l.Ready())
		}
		time.Sleep(time.Millisecond)
	}
	if err := l.Upload(ctx, "k", bytes.NewReader([]byte("x")), "text/plain", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := l.GetObjectFrom(ctx, "k", 0); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", attempts.Load())
	}
}

func TestLazyStoreStopsRetryingWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var attempts atomic.Int32
	l := startLazyStore(ctx, func(context.Context) (media.Store, error) {
		attempts.Add(1)
		return nil, errors.New("down")
	}, time.Millisecond, time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)
	n := attempts.Load()
	time.Sleep(20 * time.Millisecond)
	if attempts.Load() != n {
		t.Fatal("still retrying after the context ended")
	}
	if l.Ready() == nil {
		t.Fatal("ready without a store")
	}
}

func waitReady(t *testing.T, l *lazyStore) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for l.Ready() != nil {
		if time.Now().After(deadline) {
			t.Fatalf("store never became ready: %v", l.Ready())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLazyStoreDoesNotBlockOnAHangingFirstAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	defer close(release)
	done := make(chan *lazyStore, 1)
	go func() {
		done <- startLazyStore(ctx, func(context.Context) (media.Store, error) {
			<-release
			return nil, errors.New("blackholed")
		}, time.Millisecond, time.Millisecond)
	}()
	select {
	case l := <-done:
		if l.Ready() == nil {
			t.Fatal("ready before init succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("startLazyStore waited for the first init attempt")
	}
}

// probedStore is a store whose storage can be switched off after the start.
type probedStore struct {
	*media.MemoryStore
	down   atomic.Bool
	probes atomic.Int32
}

func (p *probedStore) Probe(context.Context) error {
	p.probes.Add(1)
	if p.down.Load() {
		return errors.New("connection refused")
	}
	return nil
}

func TestLazyStoreReportsStorageThatDiesAfterTheStartAndItsReturn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &probedStore{MemoryStore: media.NewMemoryStore()}
	l := startLazyStore(ctx, func(context.Context) (media.Store, error) { return store, nil }, time.Millisecond, time.Millisecond)
	waitReady(t, l)

	store.down.Store(true)
	// The first answer is cached: many callers, one request to storage.
	for range 5 {
		if err := l.Ready(); err != nil {
			t.Fatalf("a cached healthy answer was dropped early: %v", err)
		}
	}
	if n := store.probes.Load(); n != 1 {
		t.Fatalf("probes = %d, want 1", n)
	}

	l.probeMu.Lock()
	l.probedAt = time.Now().Add(-probeTTL)
	l.probeMu.Unlock()
	if l.Ready() == nil {
		t.Fatal("storage that died after a healthy start is still reported as ready")
	}
	if l.Ready() == nil || store.probes.Load() != 2 {
		t.Fatalf("the outage was not cached: probes = %d", store.probes.Load())
	}

	store.down.Store(false)
	l.probeMu.Lock()
	l.probedAt = time.Now().Add(-probeTTL)
	l.probeMu.Unlock()
	if err := l.Ready(); err != nil {
		t.Fatalf("storage is back but still reported down: %v", err)
	}
}
