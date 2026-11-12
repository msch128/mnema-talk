package updaterd

import (
	"testing"
	"time"
)

// WaitIdle (shutdown) waits on a sync.WaitGroup while a handler that outlived
// http.Server.Shutdown can still start a swap through background. Nothing
// refuses a swap after WaitIdle returned, so it starts while the process exits.
// Known gap, an inverse assertion: it passes while the swap is accepted and
// turns red the day background refuses work after WaitIdle.
func TestKnownGapSwapStartsAfterWaitIdleReturned(t *testing.T) {
	s := &Server{}
	if !s.WaitIdle(time.Second) {
		t.Fatal("an idle updater must report idle at once")
	}
	ran := make(chan struct{})
	s.background(func() { close(ran) })
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("fixed: a swap after WaitIdle is refused; turn this into a regression test")
	}
	if !s.WaitIdle(time.Second) {
		t.Fatal("the updater must be idle again after the swap")
	}
}
