package chat

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mnema-xyz/talk/internal/chat/core"
)

// countingLoads is a member list whose reads are counted and, while gate is
// set, wait for it: each read returns a list of as many members as reads so far.
func countingLoads(gate chan struct{}) (*memberLoads, *atomic.Int32) {
	var reads atomic.Int32
	l := &memberLoads{load: func(context.Context) ([]core.Member, error) {
		n := reads.Add(1)
		if gate != nil {
			<-gate
		}
		return make([]core.Member, n), nil
	}}
	return l, &reads
}

// After a restart every client asks for the member list at once: they share
// reads instead of reading the whole list once each.
func TestMemberLoadsShareConcurrentReads(t *testing.T) {
	gate := make(chan struct{})
	l, reads := countingLoads(gate)
	first := make(chan int, 1)
	go func() {
		got, _ := l.get(context.Background(), nil)
		first <- len(got)
	}()
	waitFor(t, func() bool { return reads.Load() == 1 })

	// 200 requests arrive while the first read runs: they all join one next read.
	var wg sync.WaitGroup
	lens := make([]int, 200)
	for i := range lens {
		wg.Go(func() {
			got, err := l.get(context.Background(), nil)
			if err != nil {
				t.Error(err)
			}
			lens[i] = len(got)
		})
	}
	waitFor(t, func() bool { l.mu.Lock(); defer l.mu.Unlock(); return l.waiting != nil })
	time.Sleep(20 * time.Millisecond) // let the rest join
	close(gate)
	wg.Wait()

	if n := <-first; n != 1 {
		t.Fatalf("first request got read %d, want 1", n)
	}
	if got := reads.Load(); got != 2 {
		t.Fatalf("%d reads for 201 requests, want 2", got)
	}
	// Nobody got the read that began before it arrived.
	for i, n := range lens {
		if n != 2 {
			t.Fatalf("request %d got read %d, want 2", i, n)
		}
	}
}

// A request alone reads at once and a later one reads again: nothing is cached.
func TestMemberLoadsReadAgainLater(t *testing.T) {
	l, reads := countingLoads(nil)
	for want := 1; want <= 3; want++ {
		got, err := l.get(context.Background(), nil)
		if err != nil || len(got) != want {
			t.Fatalf("read %d: got %d members, err %v", want, len(got), err)
		}
	}
	if reads.Load() != 3 {
		t.Fatalf("%d reads, want 3", reads.Load())
	}
}

// A read that panics fails its requests only; the next request reads again.
func TestMemberLoadsRecoverFromAPanickingRead(t *testing.T) {
	var calls atomic.Int32
	l := &memberLoads{load: func(context.Context) ([]core.Member, error) {
		if calls.Add(1) == 1 {
			panic("boom")
		}
		return []core.Member{{}}, nil
	}}
	if _, err := l.get(context.Background(), nil); !errors.Is(err, errMemberLoad) {
		t.Fatalf("panicking read: err %v, want errMemberLoad", err)
	}
	got, err := l.get(context.Background(), nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("read after the panic: %d members, err %v", len(got), err)
	}
}

// A caller that gives up returns at once; the read still answers the others.
func TestMemberLoadsCallerGivesUp(t *testing.T) {
	gate := make(chan struct{})
	l, _ := countingLoads(gate)
	ctx, cancel := context.WithCancel(context.Background())
	gaveUp := make(chan error, 1)
	go func() { _, err := l.get(ctx, nil); gaveUp <- err }()
	other := make(chan int, 1)
	go func() { got, _ := l.get(context.Background(), nil); other <- len(got) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	if err := <-gaveUp; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller: err %v", err)
	}
	close(gate)
	if n := <-other; n == 0 {
		t.Fatal("the other caller got no members")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}
