package sfu

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/interceptor"
)

// panicOnRead panics on its second read: the first one shows the drain runs.
type panicOnRead struct{ reads atomic.Int32 }

func (r *panicOnRead) Read([]byte) (int, interceptor.Attributes, error) {
	if r.reads.Add(1) > 1 {
		panic("track read failed")
	}
	return 0, nil, nil
}

// TestPanicInADrainIsCountedAndStopsOnlyThatDrain fires the panic the
// sfu_drain site abandons. What it leaves: that track is no longer read (its
// reads stop at the panic), the panic is counted, and the process goes on.
func TestPanicInADrainIsCountedAndStopsOnlyThatDrain(t *testing.T) {
	before := metrics.GoroutinePanics.With("sfu_drain").Load()
	r := &panicOnRead{}
	drain(r)
	deadline := time.Now().Add(5 * time.Second)
	for metrics.GoroutinePanics.With("sfu_drain").Load() == before {
		if time.Now().After(deadline) {
			t.Fatal("the drain's panic was not counted")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if n := r.reads.Load(); n != 2 {
		t.Fatalf("reads after the panic: %d, want 2 (the drain stops at the panic)", n)
	}
}
