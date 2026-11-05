package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fillSlots occupies every hashing slot until the test ends.
func fillSlots(t *testing.T) {
	t.Helper()
	n := cap(bcryptSlots)
	for i := 0; i < n; i++ {
		bcryptSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < n; i++ {
			<-bcryptSlots
		}
	})
}

func TestSaturatedHashingQueueFailsFastInsteadOfBlocking(t *testing.T) {
	fillSlots(t)
	old := bcryptMaxWait
	bcryptMaxWait = 50 * time.Millisecond
	t.Cleanup(func() { bcryptMaxWait = old })

	start := time.Now()
	if err := BurnPasswordCheck(context.Background(), "x"); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("got %v, want ErrServerBusy", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("waited %v, want about the 50ms limit", time.Since(start))
	}
}

func TestQueuedHashingGivesUpWhenClientDisconnects(t *testing.T) {
	fillSlots(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := checkPasswordCtx(ctx, "x", "hash"); done <- err }()
	// Cancel only once the request is really queued behind the full slots.
	for deadline := time.Now().Add(time.Second); bcryptWaiting.Load() == 0; {
		if time.Now().After(deadline) {
			t.Fatal("the request never queued for a hashing slot")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("a cancelled request kept waiting for a hashing slot")
	}
	if n := bcryptWaiting.Load(); n != 0 {
		t.Fatalf("waiting counter leaked after the cancel: %d", n)
	}
}

func TestHashingQueueDepthIsBounded(t *testing.T) {
	fillSlots(t)
	old := bcryptMaxQueue
	bcryptMaxQueue = 0
	t.Cleanup(func() { bcryptMaxQueue = old })
	if _, err := HashPasswordCtx(context.Background(), "x"); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("got %v, want ErrServerBusy at once", err)
	}
	if bcryptWaiting.Load() != 0 {
		t.Fatalf("waiting counter leaked: %d", bcryptWaiting.Load())
	}
}
