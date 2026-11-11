package sfu

import (
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// panickingRemote is a publisher track whose read hits a bug in the SFU.
type panickingRemote struct{}

func (panickingRemote) Read([]byte) (int, interceptor.Attributes, error) {
	panic("forwarding bug")
}

// steadyRemote is a publisher track that sends a packet every millisecond
// until the test ends, and counts the ones the SFU took.
type steadyRemote struct {
	taken atomic.Int64
	done  chan struct{}
}

func newSteadyRemote(t *testing.T) *steadyRemote {
	r := &steadyRemote{done: make(chan struct{})}
	t.Cleanup(func() { close(r.done) })
	return r
}

func (r *steadyRemote) Read(b []byte) (int, interceptor.Attributes, error) {
	select {
	case <-r.done:
		return 0, nil, io.EOF
	case <-time.After(time.Millisecond):
	}
	// Every packet is a VP8 keyframe, so a viewer starts at once.
	seq := uint16(r.taken.Add(1))
	raw, err := (&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, SSRC: 2}, Payload: vp8Payload(true, true, seq&0x7fff, true, 1, 'a')}).Marshal()
	if err != nil {
		return 0, nil, err
	}
	return copy(b, raw), nil, nil
}

// watch adds a viewer of publisher's screen share to the room, by hand: its
// track delivers to sink instead of a browser.
func watch(t *testing.T, r *Room, publisher, viewer uuid.UUID, sink func(*rtp.Packet) error) {
	t.Helper()
	r.mu.RLock()
	var gen uint64
	if peer := r.peers[viewer]; peer != nil {
		gen = peer.gen
	}
	r.mu.RUnlock()
	watchAs(t, r, publisher, viewer, gen, sink)
}

// watchAs is watch for the track of one particular connection of the viewer,
// the one numbered gen.
func watchAs(t *testing.T, r *Room, publisher, viewer uuid.UUID, gen uint64, sink func(*rtp.Packet) error) {
	t.Helper()
	r.mu.RLock()
	g := r.groups[publisher]
	r.mu.RUnlock()
	if g == nil {
		t.Fatal("the share has no layer group")
	}
	vt, err := g.newViewer(viewer, 0, 0, "track", "stream")
	if err != nil {
		t.Fatal(err)
	}
	vt.gen = gen
	vt.sink = sink
	if !g.addViewer(vt) {
		t.Fatal("the viewer was refused")
	}
}

// viewerCount counts the packets a viewer got.
type viewerCount struct{ n atomic.Int64 }

func (d *viewerCount) write(*rtp.Packet) error {
	d.n.Add(1)
	return nil
}

func waitPanicsCounted(t *testing.T, name string, want uint64) {
	t.Helper()
	waitSFUCondition(t, name+" to be counted", func() bool { return metrics.GoroutinePanics.With(name).Load() >= want })
}

// A forwarding loop that panics removes its own peer and tells the owner,
// like a media connection that failed. The other publisher of the room stays
// and its viewer keeps getting packets, and the panic is counted.
func TestPanickingForwardLoopRemovesOnlyItsPeer(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	broken, healthy := uuid.New(), uuid.New()
	noOffer := func(webrtc.SessionDescription) {}
	_, brokenPeer, err := s.Join(room, broken, "", noOffer, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, healthyPeer, err := s.Join(room, healthy, "", noOffer, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := s.Room(room)
	lost := make(chan struct{})
	brokenPeer.OnLost(func() { close(lost) })
	healthyPeer.OnLost(func() { t.Error("the healthy peer was dropped") })
	codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: videoClockRate}
	before := metrics.GoroutinePanics.With("sfu_forward").Load()

	steady := newSteadyRemote(t)
	r.receiveScreenLayer(healthyPeer, steady, "t", 21, codec, 0)
	var got viewerCount
	watch(t, r, healthy, uuid.New(), got.write)
	waitSFUCondition(t, "the healthy share to reach its viewer", func() bool { return got.n.Load() > 0 })
	r.receiveScreenLayer(brokenPeer, panickingRemote{}, "t", 22, codec, 0)

	select {
	case <-lost:
	case <-time.After(5 * time.Second):
		t.Fatal("the owner of the panicking peer was never told")
	}
	waitPanicsCounted(t, "sfu_forward", before+1)
	r.mu.RLock()
	_, brokenStays := r.peers[broken]
	healthyStays := r.peers[healthy] == healthyPeer
	r.mu.RUnlock()
	if brokenStays || !healthyStays {
		t.Fatalf("room after the panic: broken peer present=%t, healthy peer present=%t", brokenStays, healthyStays)
	}
	if r.sourceCount(broken, SourceScreen) != 0 || r.sourceCount(healthy, SourceScreen) != 1 {
		t.Fatalf("published screens after the panic: broken %d, healthy %d", r.sourceCount(broken, SourceScreen), r.sourceCount(healthy, SourceScreen))
	}
	after := got.n.Load()
	waitSFUCondition(t, "the healthy share to keep reaching its viewer", func() bool { return got.n.Load() >= after+20 })
}

// The track of one viewer runs on the publisher's forwarding loop. When it
// panics, that viewer is dropped: the publisher stays and the other viewers
// keep getting its packets.
func TestPanickingViewerTrackDropsThatViewerNotThePublisher(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	publisher, broken, fine := uuid.New(), uuid.New(), uuid.New()
	noOffer := func(webrtc.SessionDescription) {}
	peers := map[uuid.UUID]*Peer{}
	for _, id := range []uuid.UUID{publisher, broken, fine} {
		_, peer, err := s.Join(room, id, "", noOffer, nil)
		if err != nil {
			t.Fatal(err)
		}
		peers[id] = peer
	}
	r := s.Room(room)
	lost := make(chan struct{})
	peers[broken].OnLost(func() { close(lost) })
	peers[publisher].OnLost(func() { t.Error("the publisher was dropped for a viewer's panic") })
	peers[fine].OnLost(func() { t.Error("the healthy viewer was dropped") })
	before := metrics.GoroutinePanics.With("sfu_viewer_write").Load()

	codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: videoClockRate}
	r.receiveScreenLayer(peers[publisher], newSteadyRemote(t), "t", 31, codec, 0)
	var got viewerCount
	watch(t, r, publisher, fine, got.write)
	waitSFUCondition(t, "the share to reach the healthy viewer", func() bool { return got.n.Load() > 0 })
	watch(t, r, publisher, broken, func(*rtp.Packet) error { panic("viewer track bug") })

	select {
	case <-lost:
	case <-time.After(5 * time.Second):
		t.Fatal("the viewer whose track panicked was never dropped")
	}
	waitPanicsCounted(t, "sfu_viewer_write", before+1)
	r.mu.RLock()
	publisherStays, brokenStays := r.peers[publisher] == peers[publisher], r.peers[broken] != nil
	r.mu.RUnlock()
	if !publisherStays || brokenStays {
		t.Fatalf("room after the panic: publisher present=%t, broken viewer present=%t", publisherStays, brokenStays)
	}
	after := got.n.Load()
	waitSFUCondition(t, "the share to keep reaching the healthy viewer", func() bool { return got.n.Load() >= after+20 })
	if n := metrics.GoroutinePanics.With("sfu_viewer_write").Load() - before; n != 1 {
		t.Fatalf("the broken track was written to again: %d panics", n)
	}
}

// A panic can come out of a removal that had taken the peer out of the room
// already but not yet told its owner. The owner is told all the same: the
// member would otherwise stay seated in the Talk without media.
func TestPanicAfterThePeerLeftTheRoomStillTellsItsOwner(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	_, peer, err := s.Join(room, uuid.New(), "", func(webrtc.SessionDescription) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := s.Room(room)
	lost := make(chan struct{})
	peer.OnLost(func() { close(lost) })
	r.detachPeer(peer) // the removal got this far, then panicked

	r.dropOnPanic(peer)()

	select {
	case <-lost:
	case <-time.After(5 * time.Second):
		t.Fatal("the owner was never told")
	}
}

// NACKs come at the rate the client chooses. A worker that panics on them is
// restarted a few times only; then the connection's feedback ends and the
// peer is dropped, instead of panicking once per NACK for good.
func TestNackWorkerGivesUpAfterAFewPanicsAndThePeerIsDropped(t *testing.T) {
	i, err := nackResponderFactory{}.NewInterceptor("")
	if err != nil {
		t.Fatal(err)
	}
	n := i.(*nackResponder)
	t.Cleanup(func() { _ = n.Close() })
	n.afterResend = func() { panic("resend bug") }
	before := metrics.GoroutinePanics.With("sfu_nack").Load()
	reads := 0
	reader := n.BindRTCPReader(interceptor.RTCPReaderFunc(func([]byte, interceptor.Attributes) (int, interceptor.Attributes, error) {
		reads++
		return 0, nil, nil
	}))
	if _, _, err := reader.Read(nil, nil); err != nil {
		t.Fatalf("a working responder ended the feedback: %v", err)
	}

	for range 4 * nackWorkerPanics {
		n.enqueue(&rtcp.TransportLayerNack{})
		time.Sleep(2 * time.Millisecond)
	}
	waitSFUCondition(t, "the responder to give up", n.gaveUp.Load)
	if got := metrics.GoroutinePanics.With("sfu_nack").Load() - before; got != nackWorkerPanics {
		t.Fatalf("the worker panicked %d times, want it stopped after %d", got, nackWorkerPanics)
	}
	_, _, err = reader.Read(nil, nil)
	if !errors.Is(err, errNackGaveUp) || reads != 1 {
		t.Fatalf("feedback after giving up: err=%v, reads=%d", err, reads)
	}

	s := newTestSFU(t)
	room := uuid.New()
	_, peer, joinErr := s.Join(room, uuid.New(), "", func(webrtc.SessionDescription) {}, nil)
	if joinErr != nil {
		t.Fatal(joinErr)
	}
	lost := make(chan struct{})
	peer.OnLost(func() { close(lost) })
	s.Room(room).feedbackEnded(peer, err)
	select {
	case <-lost:
	case <-time.After(5 * time.Second):
		t.Fatal("the peer whose responder gave up was not dropped")
	}
}

// The recovery around every viewer's write is on the path of every packet:
// it must not allocate.
func TestLayeredWriteWithRecoveryDoesNotAllocate(t *testing.T) {
	rig := newLayerRig(t, "video/VP8", 1)
	vt, _ := rig.viewer(0)
	vt.sink = func(*rtp.Packet) error { return nil }
	pkt := &rtp.Packet{Header: rtp.Header{Version: 2}, Payload: vp8Payload(true, true, 1, true, 1, 'a')}
	seq := uint16(0)
	if allocs := testing.AllocsPerRun(1000, func() {
		seq++
		pkt.SequenceNumber = seq
		rig.group.write(0, pkt)
	}); allocs != 0 {
		t.Fatalf("forwarding one packet to one viewer allocates %.1f times", allocs)
	}
}

func BenchmarkLayeredWritePerViewer(b *testing.B) {
	g := newLayerGroup("group", uuid.New(), uuid.New(), nil, webrtc.RTPCodecCapability{MimeType: "video/VP8", ClockRate: videoClockRate})
	vt, err := g.newViewer(uuid.New(), 0, 0, "track", "stream")
	if err != nil {
		b.Fatal(err)
	}
	vt.sink = func(*rtp.Packet) error { return nil }
	pkt := &rtp.Packet{Header: rtp.Header{Version: 2}, Payload: vp8Payload(true, true, 1, true, 1, 'a')}
	b.Run("recovered", func(b *testing.B) {
		for b.Loop() {
			g.writeTo(vt, 0, pkt)
		}
	})
	b.Run("bare", func(b *testing.B) {
		for b.Loop() {
			vt.write(0, pkt)
		}
	})
}
