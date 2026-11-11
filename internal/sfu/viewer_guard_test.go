package sfu

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/safego"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// sharedTrack stands for the one outgoing track all listeners of a microphone
// are bound to: like Pion's, it writes each packet to every listener's chain
// on the caller's goroutine.
type sharedTrack []interceptor.RTPWriter

func (s sharedTrack) WriteRTP(p *rtp.Packet) error {
	for _, w := range s {
		_, _ = w.Write(&p.Header, p.Payload, nil)
	}
	return nil
}

// listen is the chain of one listener's connection as the SFU guards it,
// ending in transport instead of the network.
func listen(t *testing.T, s *SFU, listener *Peer, transport interceptor.RTPWriterFunc) interceptor.RTPWriter {
	t.Helper()
	guard, err := viewerGuardFactory{drop: s.dropConnection}.NewInterceptor(listener.connID)
	if err != nil {
		t.Fatal(err)
	}
	return guard.BindLocalStream(&interceptor.StreamInfo{}, transport)
}

func joinAll(t *testing.T, s *SFU, room uuid.UUID, users ...uuid.UUID) map[uuid.UUID]*Peer {
	t.Helper()
	peers := map[uuid.UUID]*Peer{}
	for _, id := range users {
		_, peer, err := s.Join(room, id, func(webrtc.SessionDescription) {}, nil)
		if err != nil {
			t.Fatal(err)
		}
		peers[id] = peer
	}
	return peers
}

// A microphone is one track for all its listeners, and every listener's
// connection handles each packet on the speaker's forwarding loop. When one
// listener's connection panics there, that listener is dropped: the speaker
// stays and the others keep hearing them.
func TestPanicInOneListenersConnectionDropsThatListenerNotTheSpeaker(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	speaker, broken, fine := uuid.New(), uuid.New(), uuid.New()
	peers := joinAll(t, s, room, speaker, broken, fine)
	r := s.Room(room)
	lost := make(chan struct{})
	peers[broken].OnLost(func() { close(lost) })
	peers[speaker].OnLost(func() { t.Error("the speaker was dropped for a listener's panic") })
	peers[fine].OnLost(func() { t.Error("the healthy listener was dropped") })
	before := metrics.GoroutinePanics.With("sfu_viewer_write").Load()

	var got viewerCount
	track := sharedTrack{
		listen(t, s, peers[broken], func(*rtp.Header, []byte, interceptor.Attributes) (int, error) {
			panic("listener connection bug")
		}),
		listen(t, s, peers[fine], func(*rtp.Header, []byte, interceptor.Attributes) (int, error) {
			got.n.Add(1)
			return 0, nil
		}),
	}
	info := &TrackInfo{SenderID: speaker, Kind: webrtc.RTPCodecTypeAudio, Source: SourceAudio, publisher: peers[speaker]}
	safego.Go("sfu_forward", r.dropOnPanic(peers[speaker]), func() { r.forward(newSteadyRemote(t), track, "mic", info) })

	select {
	case <-lost:
	case <-time.After(5 * time.Second):
		t.Fatal("the listener whose connection panicked was never dropped")
	}
	r.mu.RLock()
	speakerStays, brokenStays := r.peers[speaker] == peers[speaker], r.peers[broken] != nil
	r.mu.RUnlock()
	if !speakerStays || brokenStays {
		t.Fatalf("room after the panic: speaker present=%t, broken listener present=%t", speakerStays, brokenStays)
	}
	after := got.n.Load()
	waitSFUCondition(t, "the microphone to keep reaching the healthy listener", func() bool { return got.n.Load() >= after+20 })
	if n := metrics.GoroutinePanics.With("sfu_viewer_write").Load() - before; n != 1 {
		t.Fatalf("the broken connection was written to again: %d panics", n)
	}
}

// A connection whose chain panicked reports a closed stream from then on,
// which the forwarding loops take as a viewer that just left.
func TestGuardedConnectionReportsAClosedStreamAfterItsPanic(t *testing.T) {
	dropped := make(chan string, 1)
	guard, _ := viewerGuardFactory{drop: func(id string) { dropped <- id }}.NewInterceptor("conn")
	calls := 0
	w := guard.BindLocalStream(&interceptor.StreamInfo{}, interceptor.RTPWriterFunc(func(*rtp.Header, []byte, interceptor.Attributes) (int, error) {
		calls++
		panic("connection bug")
	}))

	for range 2 {
		if _, err := w.Write(&rtp.Header{}, nil, nil); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("write after a panic returned %v, want a closed stream", err)
		}
	}
	if calls != 1 {
		t.Fatalf("the chain that panicked was entered %d times, want once", calls)
	}
	select {
	case id := <-dropped:
		if id != "conn" {
			t.Fatalf("connection %q dropped, want the one that panicked", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was never dropped")
	}
}

// The guard is the first writer a packet passes on every connection, so a
// panic anywhere beneath it (retransmission history, counters, the
// transport-cc stamp, the bandwidth estimate, the transport) is caught.
func TestEveryConnectionsChainStartsWithTheViewerGuard(t *testing.T) {
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		t.Fatal(err)
	}
	dropped := make(chan string, 1)
	registry, err := buildInterceptors(mediaEngine, &peerLoad{}, func(id string) { dropped <- id })
	if err != nil {
		t.Fatal(err)
	}
	chain, err := registry.Build("conn")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = chain.Close() })
	info := &interceptor.StreamInfo{SSRC: 7, ClockRate: videoClockRate, MimeType: webrtc.MimeTypeVP8}
	w := chain.BindLocalStream(info, interceptor.RTPWriterFunc(func(*rtp.Header, []byte, interceptor.Attributes) (int, error) {
		panic("transport bug")
	}))

	if _, ok := w.(*guardedWriter); !ok {
		t.Fatalf("the chain starts with %T, want the viewer guard", w)
	}
	if _, err := w.Write(&rtp.Header{Version: 2, SSRC: 7}, []byte{1}, interceptor.Attributes{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("a panic at the bottom of the chain came back as %v, want a closed stream", err)
	}
	select {
	case id := <-dropped:
		if id != "conn" {
			t.Fatalf("connection %q dropped, want the one that panicked", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was never dropped")
	}
}

// Pion names a connection after the nanosecond it was made in, so two can
// share a name. When the chain of one of them panics, both are dropped and
// join again: the one that panicked must not stay behind unheard.
func TestConnectionsThatShareAnIDAreBothDroppedForAPanic(t *testing.T) {
	s := newTestSFU(t)
	first, second, other := uuid.New(), uuid.New(), uuid.New()
	peers := joinAll(t, s, uuid.New(), first, other)
	peers[second] = joinAll(t, s, uuid.New(), second)[second]
	peers[second].connID = peers[first].connID
	lost := make(chan uuid.UUID, 2)
	for _, id := range []uuid.UUID{first, second} {
		peers[id].OnLost(func() { lost <- id })
	}
	peers[other].OnLost(func() { t.Error("a connection with another ID was dropped") })

	s.dropConnection(peers[first].connID)

	got := map[uuid.UUID]bool{}
	for range 2 {
		select {
		case id := <-lost:
			got[id] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("dropped %d of the 2 connections that share the ID", len(got))
		}
	}
}

// The track of a viewer's earlier connection can still panic after the
// viewer joined again. The new connection is healthy and stays.
func TestPanickingTrackOfAReplacedConnectionLeavesTheNewOne(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	publisher, viewer := uuid.New(), uuid.New()
	peers := joinAll(t, s, room, publisher, viewer)
	earlier := peers[viewer]
	rejoined := joinAll(t, s, room, viewer)[viewer]
	r := s.Room(room)
	rejoined.OnLost(func() { t.Error("the viewer's new connection was dropped") })
	before := metrics.GoroutinePanics.With("sfu_viewer_write").Load()

	codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: videoClockRate}
	remote := newSteadyRemote(t)
	r.receiveScreenLayer(peers[publisher], remote, "t", 31, codec, 0)
	watchAs(t, r, publisher, viewer, earlier.gen, func(*rtp.Packet) error { panic("viewer track bug") })

	waitPanicsCounted(t, "sfu_viewer_write", before+1)
	// Long enough for a drop to have happened: the share keeps arriving.
	taken := remote.taken.Load()
	waitSFUCondition(t, "the share to go on", func() bool { return remote.taken.Load() >= taken+50 })
	r.mu.RLock()
	stays := r.peers[viewer] == rejoined
	r.mu.RUnlock()
	if !stays {
		t.Fatal("the viewer's new connection left the room")
	}
}

// The guard runs once per packet and listener.
func TestGuardedWriteDoesNotAllocate(t *testing.T) {
	w := guardedTestWriter()
	h, payload := &rtp.Header{Version: 2}, []byte{1, 2, 3}
	if allocs := testing.AllocsPerRun(1000, func() {
		_, _ = w.Write(h, payload, nil)
	}); allocs != 0 {
		t.Fatalf("guarding one packet for one listener allocates %.1f times", allocs)
	}
}

// discardWriter is a connection that takes every packet.
type discardWriter struct{}

func (discardWriter) Write(_ *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
	return len(payload), nil
}

func guardedTestWriter() interceptor.RTPWriter {
	guard, _ := viewerGuardFactory{drop: func(string) {}}.NewInterceptor("conn")
	return guard.BindLocalStream(&interceptor.StreamInfo{}, discardWriter{})
}

func BenchmarkSharedWritePerListener(b *testing.B) {
	h, payload := &rtp.Header{Version: 2}, []byte{1, 2, 3}
	var guarded, bare interceptor.RTPWriter = guardedTestWriter(), discardWriter{}
	b.Run("guarded", func(b *testing.B) {
		for b.Loop() {
			_, _ = guarded.Write(h, payload, nil)
		}
	})
	b.Run("bare", func(b *testing.B) {
		for b.Loop() {
			_, _ = bare.Write(h, payload, nil)
		}
	})
}
