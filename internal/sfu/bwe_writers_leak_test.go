package sfu

import (
	"testing"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

// Oracle for inventory I6 row 26 (lane 8, growth): bweInterceptor's passPacer
// keeps a `writers` entry per bound SSRC (plus one per RTX alias) and never
// deletes it. The sibling interceptor nackResponder implements
// UnbindLocalStream to release its per-stream state (nack_responder.go:134);
// bweInterceptor embeds interceptor.NoOp, so its UnbindLocalStream does nothing
// and the pacer's map only grows -- for the whole life of the peer connection,
// across every subscription change, layer switch and renegotiation that binds a
// fresh outgoing SSRC.
//
// pacerEntriesAfterChurn drives the real interceptor through N bind/unbind
// cycles with distinct SSRCs (what a long call's subscription churn produces)
// and returns how many pacer entries survive the unbinds. It counts the
// perturbation (binds performed) so the test cannot pass by doing nothing.

func pacerEntriesAfterChurn(t *testing.T, cycles int) (entries, bound int) {
	t.Helper()
	ic, err := (bweFactory{}).NewInterceptor("")
	if err != nil {
		t.Fatalf("build bwe interceptor: %v", err)
	}
	b := ic.(*bweInterceptor)
	defer func() { _ = b.Close() }()

	sink := interceptor.RTPWriterFunc(func(*rtp.Header, []byte, interceptor.Attributes) (int, error) { return 0, nil })
	for i := 0; i < cycles; i++ {
		ssrc := uint32(1_000 + i*2)
		rtx := ssrc + 1
		info := &interceptor.StreamInfo{SSRC: ssrc, SSRCRetransmission: rtx}
		b.BindLocalStream(info, sink)
		bound++
		// pion calls UnbindLocalStream when the local track is removed (a viewer
		// unsubscribes, a publisher drops a layer). nackResponder cleans up here;
		// bweInterceptor inherits the NoOp and leaks.
		b.UnbindLocalStream(info)
	}
	b.pacer.mu.RLock()
	entries = len(b.pacer.writers)
	b.pacer.mu.RUnlock()
	return entries, bound
}

// TestPassPacerWritersLeakOnUnbind is an inverse-assertion known failure
// (AGENT-RULES "A known failure ... for Go write the inverse assertion in a
// helper and say so"): it asserts the LEAK IS PRESENT (entries do not return to
// baseline after the unbinds), so it passes today and turns RED the day
// bweInterceptor gains an UnbindLocalStream that deletes from passPacer.writers.
// Flip the comparison to `== 0` when that fix lands.
func TestPassPacerWritersLeakOnUnbind(t *testing.T) {
	const cycles = 500
	entries, bound := pacerEntriesAfterChurn(t, cycles)
	t.Logf("after %d bind/unbind cycles: %d pacer entries survive (expected 0 if cleaned up; each cycle also binds an RTX alias)", bound, entries)
	if entries == 0 {
		t.Fatalf("passPacer.writers returned to baseline after %d unbinds: the I6 row 26 leak is fixed -- invert this assertion to a size guard", cycles)
	}
	// The leak grows with churn: at least one entry per cycle survives (the base
	// SSRC; the RTX alias doubles it). This documents the unbounded-over-a-call
	// growth, not merely one stale entry.
	if entries < cycles {
		t.Fatalf("expected the leak to grow with churn (>= %d survivors), got %d", cycles, entries)
	}
}
