package sfu

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

// The bandwidth estimator's pacer keeps one writer per bound outgoing SSRC
// (and one per RTX alias). Pion binds a stream when a sender starts (a viewer
// subscribes, a publisher starts or resumes a track) and unbinds it when the
// sender stops; the pacer must forget the stream then, or a connection that
// keeps toggling subscriptions grows it, with the stopped senders it pins,
// until it closes.

func newTestBWE(t *testing.T) *bweInterceptor {
	t.Helper()
	ic, err := (bweFactory{}).NewInterceptor("")
	if err != nil {
		t.Fatalf("build bwe interceptor: %v", err)
	}
	b := ic.(*bweInterceptor)
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func pacerEntries(b *bweInterceptor) int {
	b.pacer.mu.RLock()
	defer b.pacer.mu.RUnlock()
	return len(b.pacer.writers)
}

func discardRTP(*rtp.Header, []byte, interceptor.Attributes) (int, error) { return 0, nil }

// pacerEntriesAfterChurn drives the real interceptor through bind/unbind
// cycles with distinct SSRCs and returns how many pacer entries survive, and
// how many the first bind added (so the test cannot pass by binding nothing).
func pacerEntriesAfterChurn(t *testing.T, cycles int) (entries, firstBind int) {
	t.Helper()
	b := newTestBWE(t)
	for i := 0; i < cycles; i++ {
		ssrc := uint32(1_000 + i*2)
		info := &interceptor.StreamInfo{SSRC: ssrc, SSRCRetransmission: ssrc + 1}
		b.BindLocalStream(info, interceptor.RTPWriterFunc(discardRTP))
		if i == 0 {
			firstBind = pacerEntries(b)
		}
		b.UnbindLocalStream(info)
	}
	return pacerEntries(b), firstBind
}

func TestPassPacerForgetsUnboundStreams(t *testing.T) {
	const cycles = 500
	entries, firstBind := pacerEntriesAfterChurn(t, cycles)
	if firstBind != 2 {
		t.Fatalf("a bound stream with RTX has %d pacer entries, want 2", firstBind)
	}
	if entries != 0 {
		t.Fatalf("%d pacer entries survive %d bind/unbind cycles, want 0", entries, cycles)
	}
}

// Unbinding one stream leaves the others, and a stream without RTX does not
// remove an entry for SSRC 0.
func TestPassPacerUnbindKeepsOtherStreams(t *testing.T) {
	b := newTestBWE(t)
	zero := &interceptor.StreamInfo{SSRC: 0}
	kept := &interceptor.StreamInfo{SSRC: 10, SSRCRetransmission: 11}
	gone := &interceptor.StreamInfo{SSRC: 20}
	for _, info := range []*interceptor.StreamInfo{zero, kept, gone} {
		b.BindLocalStream(info, interceptor.RTPWriterFunc(discardRTP))
	}
	b.UnbindLocalStream(gone)
	for _, ssrc := range []uint32{0, 10, 11} {
		if _, err := b.pacer.Write(&rtp.Header{SSRC: ssrc}, nil, nil); err != nil {
			t.Fatalf("stream %d after another was unbound: %v", ssrc, err)
		}
	}
	if _, err := b.pacer.Write(&rtp.Header{SSRC: 20}, nil, nil); err == nil {
		t.Fatal("a packet of the unbound stream was sent")
	}
}

// A packet paced while its stream is unbound is either sent to the stream's
// writer (its lookup came first) or refused with the unknown stream error (it
// came after), never lost in between; once UnbindLocalStream has returned,
// every packet of the stream and of its RTX alias is refused. Run with -race.
func TestPassPacerWriteRacingUnbind(t *testing.T) {
	const writers, perWriter = 4, 2_000
	b := newTestBWE(t)
	var sent atomic.Int64
	sink := interceptor.RTPWriterFunc(func(_ *rtp.Header, p []byte, _ interceptor.Attributes) (int, error) {
		sent.Add(1)
		return len(p), nil
	})
	info := &interceptor.StreamInfo{SSRC: 42, SSRCRetransmission: 43}
	out := b.BindLocalStream(info, sink)

	var unbound atomic.Bool
	var refused, lateSent atomic.Int64
	var done sync.WaitGroup
	for g := 0; g < writers; g++ {
		done.Add(1)
		go func(ssrc uint32) {
			defer done.Done()
			for i := 0; i < perWriter; i++ {
				after := unbound.Load()
				_, err := out.Write(&rtp.Header{SSRC: ssrc, SequenceNumber: uint16(i)}, []byte{1}, nil)
				switch {
				case err != nil:
					refused.Add(1)
				case after:
					lateSent.Add(1)
				}
			}
		}(info.SSRC + uint32(g%2))
	}
	// Let the writers get going so the unbind lands among them.
	for sent.Load() == 0 {
		runtime.Gosched()
	}
	b.UnbindLocalStream(info)
	unbound.Store(true)
	done.Wait()

	total := int64(writers * perWriter)
	if got := sent.Load() + refused.Load(); got != total {
		t.Fatalf("%d sent + %d refused = %d, want %d", sent.Load(), refused.Load(), got, total)
	}
	if lateSent.Load() != 0 {
		t.Fatalf("%d packets begun after the unbind returned were sent", lateSent.Load())
	}
	if n := pacerEntries(b); n != 0 {
		t.Fatalf("%d pacer entries after the unbind", n)
	}
	t.Logf("%d sent before the unbind, %d refused", sent.Load(), refused.Load())
}
