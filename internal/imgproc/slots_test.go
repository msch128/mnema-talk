package imgproc

import (
	"context"
	"errors"
	"testing"
	"time"
)

func fillSlots(t *testing.T) {
	t.Helper()
	n := cap(slots)
	for i := 0; i < n; i++ {
		slots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < n; i++ {
			<-slots
		}
	})
}

func TestRedrawGivesUpWhenEverySlotIsTaken(t *testing.T) {
	old := maxWait
	maxWait = 20 * time.Millisecond
	t.Cleanup(func() { maxWait = old })
	fillSlots(t)
	if _, err := ProcessAvatar(nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want busy", err)
	}
}

func TestGateHandsTheSlotBack(t *testing.T) {
	fillSlots(t)
	<-slots
	release, err := Gate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	release()
	slots <- struct{}{}
}

func TestGateRefusesWhenTheQueueIsAlreadyFull(t *testing.T) {
	oldWait, oldMax := maxWait, maxQueue
	maxWait = time.Hour
	maxQueue = 0
	t.Cleanup(func() {
		maxWait = oldWait
		maxQueue = oldMax
	})
	fillSlots(t)
	release, err := Gate(t.Context())
	if release != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("release set = %v, err = %v", release != nil, err)
	}
	if waiting.Load() != 0 {
		t.Fatalf("queue counter left at %d", waiting.Load())
	}
}

func TestGateTakesASlotThatFreesWhileItWaits(t *testing.T) {
	old := maxWait
	maxWait = time.Second
	t.Cleanup(func() { maxWait = old })
	n := cap(slots)
	for i := 0; i < n; i++ {
		slots <- struct{}{}
	}
	t.Cleanup(func() {
		for len(slots) > 0 {
			<-slots
		}
	})
	got := make(chan error, 1)
	var release func()
	go func() {
		var err error
		release, err = Gate(t.Context())
		got <- err
	}()
	deadline := time.Now().Add(time.Second)
	for waiting.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("gate did not wait for a slot")
		}
		time.Sleep(time.Millisecond)
	}
	<-slots
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("gate did not take the freed slot")
	}
	release()
}

func TestGateStopsWhenTheCallerGivesUp(t *testing.T) {
	old := maxWait
	maxWait = time.Hour
	t.Cleanup(func() { maxWait = old })
	fillSlots(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	release, err := Gate(ctx)
	if release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("release set = %v, err = %v", release != nil, err)
	}
}
