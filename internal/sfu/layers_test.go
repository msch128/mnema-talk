package sfu

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// fakeClock replaces the clock of timestamp rewriting for one test.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func useFakeClock(t *testing.T) *fakeClock {
	t.Helper()
	c := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	prev := layerNow
	layerNow = func() time.Time {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.now
	}
	t.Cleanup(func() { layerNow = prev })
	return c
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// sunk collects what a viewer's track delivers.
type sunk struct {
	mu   sync.Mutex
	pkts []rtp.Packet
	err  error
}

func (s *sunk) write(p *rtp.Packet) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *p
	cp.Payload = append([]byte(nil), p.Payload...)
	s.pkts = append(s.pkts, cp)
	return s.err
}

func (s *sunk) take() []rtp.Packet {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.pkts
	s.pkts = nil
	return out
}

type layerRig struct {
	t      *testing.T
	group  *layerGroup
	infos  [maxLayers]*TrackInfo
	events chan string
	wanted chan []int
	qual   chan QualityState
}

func newLayerRig(t *testing.T, mime string, layers int) *layerRig {
	t.Helper()
	notifier := &mediaNotifier{}
	rig := &layerRig{t: t, events: make(chan string, 64), wanted: make(chan []int, 64), qual: make(chan QualityState, 64)}
	notifier.setWanted(func(_, _ uuid.UUID, l []int) { rig.wanted <- l })
	notifier.setQuality(func(_, _, _ uuid.UUID, st QualityState) { rig.qual <- st })
	codec := webrtc.RTPCodecCapability{MimeType: mime, ClockRate: videoClockRate}
	g := newLayerGroup("group", uuid.New(), uuid.New(), notifier, codec)
	t.Cleanup(g.close)
	rig.group = g
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	pub := &Peer{ID: g.publisher, PC: pc}
	for i := 0; i < layers; i++ {
		info := &TrackInfo{SenderID: g.publisher, Kind: webrtc.RTPCodecTypeVideo, Source: SourceScreen, publisher: pub, group: g, layer: i, ssrc: uint32(100 + i)}
		if !g.attach(info, codec) {
			t.Fatalf("layer %d refused", i)
		}
		rig.infos[i] = info
	}
	return rig
}

// viewer adds a viewer with the given choice whose deliveries land in the
// returned collector.
func (rig *layerRig) viewer(pref int) (*viewerTrack, *sunk) {
	rig.t.Helper()
	vt := rig.group.newViewer(uuid.New(), pref, "track", "stream")
	out := &sunk{}
	vt.sink = out.write
	rig.group.addViewer(vt)
	return vt, out
}

func (rig *layerRig) pkt(layer int, seq uint16, ts uint32, payload []byte) {
	rig.group.write(layer, &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: ts}, Payload: payload})
}

func (rig *layerRig) asked(layer int) bool {
	return rig.infos[layer].lastPLI.Swap(0) != 0
}

func seqs(pkts []rtp.Packet) []uint16 {
	out := make([]uint16, len(pkts))
	for i, p := range pkts {
		out[i] = p.SequenceNumber
	}
	return out
}

func TestViewerSwitchesLayersAtKeyframesOnly(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	vt, out := rig.viewer(-1)
	if !rig.asked(0) {
		t.Fatal("a new viewer did not ask for a keyframe of its layer")
	}

	// Nothing flows before a keyframe of the chosen layer.
	rig.pkt(1, 5000, 1, vp8Payload(true, true, 900, true, 1, 'b'))
	rig.pkt(0, 99, 1, vp8Payload(true, false, 9, true, 5, 'a'))
	if got := out.take(); len(got) != 0 {
		t.Fatalf("%d packets before a keyframe", len(got))
	}

	rig.pkt(0, 100, 1000, vp8Payload(true, true, 10, true, 5, 'a'))
	rig.pkt(0, 101, 4000, vp8Payload(true, false, 11, true, 5, 'a'))
	got := out.take()
	if len(got) != 2 || got[0].SequenceNumber != 100 || got[0].Timestamp != 1000 || got[1].SequenceNumber != 101 {
		t.Fatalf("first layer delivered as %v", seqs(got))
	}
	if d, _ := parseVP8(got[0].Payload); d.pic != 10 || d.tl0 != 5 {
		t.Fatalf("an unswitched stream must keep its picture IDs: %+v", d)
	}

	// The viewer asks for layer 2: the first layer goes on until its keyframe.
	vt.setPref(2)
	if !rig.asked(2) {
		t.Fatal("a switch did not ask the publisher for a keyframe of the new layer")
	}
	rig.pkt(2, 7000, 50, vp8Payload(true, false, 300, true, 2, 'c'))
	rig.pkt(0, 102, 7000, vp8Payload(true, false, 12, true, 5, 'a'))
	got = out.take()
	if len(got) != 1 || got[0].SequenceNumber != 102 {
		t.Fatalf("expected the old layer to continue, got %v", seqs(got))
	}

	clock.advance(100 * time.Millisecond)
	rig.pkt(2, 7001, 60, vp8Payload(true, true, 301, true, 3, 'c'))
	rig.pkt(2, 7002, 60, vp8Payload(false, true, 301, true, 3, 'c'))
	rig.pkt(0, 103, 10000, vp8Payload(true, false, 13, true, 5, 'a'))
	rig.pkt(2, 7003, 3060, vp8Payload(true, false, 302, true, 3, 'c'))
	got = out.take()
	if len(got) != 3 {
		t.Fatalf("after the switch got %v, want the new layer only", seqs(got))
	}
	// Sequence numbers continue, the timestamp moved on by the elapsed time.
	if s := seqs(got); s[0] != 103 || s[1] != 104 || s[2] != 105 {
		t.Fatalf("sequence numbers across the switch: %v", s)
	}
	if got[0].Timestamp != 7000+9000 || got[1].Timestamp != got[0].Timestamp || got[2].Timestamp != got[0].Timestamp+3000 {
		t.Fatalf("timestamps across the switch: %d %d %d", got[0].Timestamp, got[1].Timestamp, got[2].Timestamp)
	}
	for i, wantPic := range []uint16{13, 13, 14} {
		d, _ := parseVP8(got[i].Payload)
		if d.pic != wantPic {
			t.Errorf("packet %d: picture ID %d, want %d", i, d.pic, wantPic)
		}
	}
	if d, _ := parseVP8(got[0].Payload); d.tl0 != 6 || d.pic16 != true {
		t.Errorf("TL0PICIDX after the switch: %+v", d)
	}
	if st := vt.state(); st.Layer != 2 || st.Auto {
		t.Fatalf("state %+v", st)
	}

	// And back to the best layer: the numbers keep counting.
	vt.setPref(0)
	clock.advance(time.Second)
	rig.pkt(0, 900, 1_000_000, vp8Payload(true, true, 3, true, 77, 'a'))
	got = out.take()
	if len(got) != 1 || got[0].SequenceNumber != 106 || got[0].Timestamp != 7000+9000+3000+90000 {
		t.Fatalf("switch back delivered %v ts %d", seqs(got), got[0].Timestamp)
	}
	if d, _ := parseVP8(got[0].Payload); d.pic != 15 || d.tl0 != 7 {
		t.Fatalf("picture ID %d TL0 %d after switching back", d.pic, d.tl0)
	}
}

func TestSwitchKeepsPictureIDsWithinTheirWidth(t *testing.T) {
	useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 2)
	vt, out := rig.viewer(0)
	rig.pkt(0, 1, 1, vp8Payload(true, true, 0x7ffe, true, 255, 'a'))
	vt.setPref(1)
	rig.pkt(1, 9, 9, vp8Payload(true, true, 5, true, 0, 'b'))
	got := out.take()
	d0, _ := parseVP8(got[0].Payload)
	d1, _ := parseVP8(got[1].Payload)
	if d0.pic != 0x7ffe || d1.pic != 0x7fff || d1.tl0 != 0 {
		t.Fatalf("wrap: %x then %x (TL0 %d)", d0.pic, d1.pic, d1.tl0)
	}
	// A 7-bit stream wraps at 128.
	rig2 := newLayerRig(t, "video/VP8", 2)
	vt2, out2 := rig2.viewer(0)
	rig2.pkt(0, 1, 1, vp8Payload(true, true, 0x7f, false, 1, 'a'))
	vt2.setPref(1)
	rig2.pkt(1, 9, 9, vp8Payload(true, true, 40, false, 1, 'b'))
	rig2.pkt(1, 10, 9, vp8Payload(true, false, 41, false, 1, 'b'))
	got = out2.take()
	if d, _ := parseVP8(got[1].Payload); d.pic != 0 || d.pic16 {
		t.Fatalf("7-bit picture ID after the switch: %+v", d)
	}
	if d, _ := parseVP8(got[2].Payload); d.pic != 1 {
		t.Fatalf("7-bit picture ID after that: %+v", d)
	}
}

func TestSwitchWithoutPictureIDsOnlyRenumbers(t *testing.T) {
	useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 2)
	vt, out := rig.viewer(0)
	plain := func(tag byte) []byte { return []byte{0x10, tag, 0xaa} }
	rig.pkt(0, 10, 100, plain(0x00))
	vt.setPref(1)
	rig.pkt(1, 50, 5, plain(0x00))
	got := out.take()
	if len(got) != 2 || got[1].SequenceNumber != 11 || got[1].Payload[0] != 0x10 {
		t.Fatalf("got %v", seqs(got))
	}
}

func TestPendingSwitchAsksAgainOncePerSecond(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 2)
	vt, _ := rig.viewer(0)
	rig.pkt(0, 1, 1, vp8Payload(true, true, 1, true, 1, 'a'))
	rig.asked(0)
	vt.setPref(1)
	if !rig.asked(1) {
		t.Fatal("no keyframe request on a switch")
	}
	// The current layer's packets remind the publisher, at most once a second.
	rig.pkt(0, 2, 2, vp8Payload(true, false, 2, true, 1, 'a'))
	if rig.asked(1) {
		t.Fatal("asked again within a second")
	}
	clock.advance(1100 * time.Millisecond)
	rig.pkt(0, 3, 3, vp8Payload(true, false, 3, true, 1, 'a'))
	if !rig.asked(1) {
		t.Fatal("never asked again for a pending switch")
	}
	// The target layer's own non-keyframe packets remind it as well.
	clock.advance(1100 * time.Millisecond)
	rig.pkt(1, 4, 4, vp8Payload(true, false, 4, true, 1, 'b'))
	if !rig.asked(1) {
		t.Fatal("a delta packet of the target did not ask for a keyframe")
	}
}

func TestResolveNearestLiveLayer(t *testing.T) {
	rig := newLayerRig(t, "video/VP8", 3)
	g := rig.group
	g.declared.Store(3)
	for want, exp := range map[int]int{0: 0, 1: 1, 2: 2, 5: 2} {
		if got := g.resolve(want); got != exp {
			t.Errorf("all live: resolve(%d) = %d, want %d", want, got, exp)
		}
	}
	g.detach(rig.infos[2])
	if got := g.resolve(2); got != 1 {
		t.Errorf("layer 2 gone: resolve(2) = %d, want 1", got)
	}
	g.detach(rig.infos[1])
	if got := g.resolve(2); got != 0 {
		t.Errorf("only layer 0: resolve(2) = %d, want 0", got)
	}
	g.detach(rig.infos[0])
	g.attach(rig.infos[2], g.codec)
	if got := g.resolve(0); got != 2 {
		t.Errorf("only layer 2: resolve(0) = %d, want 2", got)
	}
	g.detach(rig.infos[2])
	if got := g.resolve(0); got != -1 {
		t.Errorf("nothing live: resolve(0) = %d", got)
	}
	// A sender that declared two layers has no third.
	g.declared.Store(2)
	g.attach(rig.infos[1], g.codec)
	g.attach(rig.infos[2], g.codec)
	if got := g.resolve(2); got != 2 && got != 1 {
		t.Errorf("declared 2: resolve(2) = %d", got)
	}
	if got := g.resolve(2); got != 1 {
		t.Errorf("declared 2: resolve(2) = %d, want 1", got)
	}
	if got := g.clampWant(2); got != 1 {
		t.Errorf("clampWant(2) with 2 declared = %d", got)
	}
	g.declared.Store(0)
	if got := g.clampWant(9); got != maxLayers-1 {
		t.Errorf("clampWant(9) = %d", got)
	}
}

func TestLayerGoneResetsCurrentLayer(t *testing.T) {
	useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 2)
	vt, out := rig.viewer(0)
	rig.pkt(0, 1, 1, vp8Payload(true, true, 1, true, 1, 'a'))
	rig.group.detach(rig.infos[0])
	if st := vt.state(); st.Layer != -1 {
		t.Fatalf("still on layer %d", st.Layer)
	}
	// The viewer moves to the layer that is left, at its next keyframe.
	if !rig.asked(1) {
		t.Fatal("no keyframe asked of the remaining layer")
	}
	rig.pkt(1, 5, 5, vp8Payload(true, false, 2, true, 1, 'b'))
	rig.pkt(1, 6, 6, vp8Payload(true, true, 3, true, 1, 'b'))
	got := out.take()
	if len(got) != 2 || got[1].SequenceNumber != 2 {
		t.Fatalf("got %v", seqs(got))
	}
	// Detaching something that is not the slot's info changes nothing.
	rig.group.detach(rig.infos[0])
	rig.group.detach(&TrackInfo{layer: 1})
	if rig.group.live.Load() != 1<<1 {
		t.Fatalf("live = %b", rig.group.live.Load())
	}
}

func TestAttachRefusals(t *testing.T) {
	rig := newLayerRig(t, "video/VP8", 1)
	g := rig.group
	other := webrtc.RTPCodecCapability{MimeType: "video/H264", ClockRate: videoClockRate}
	if g.attach(&TrackInfo{layer: 1}, other) {
		t.Error("a layer of another codec joined")
	}
	if g.attach(&TrackInfo{layer: 1}, webrtc.RTPCodecCapability{MimeType: "VIDEO/vp8", ClockRate: 8000}) {
		t.Error("a layer with another clock joined")
	}
	for _, l := range []int{-1, maxLayers} {
		if g.attach(&TrackInfo{layer: l}, g.codec) {
			t.Errorf("layer %d joined", l)
		}
	}
	// A new track for a layer replaces and ends the old one.
	old := rig.infos[0]
	repl := &TrackInfo{layer: 0, group: g}
	if !g.attach(repl, webrtc.RTPCodecCapability{MimeType: "video/vp8", ClockRate: videoClockRate}) || !old.superseded.Load() {
		t.Error("replacing a layer did not end the old track")
	}
	if g.representative() != repl {
		t.Error("representative is not the replacement")
	}
	g.supersede()
	if !repl.superseded.Load() || !g.superseded.Load() {
		t.Error("supersede did not reach the layers")
	}
	empty := newLayerGroup("e", uuid.New(), uuid.New(), nil, g.codec)
	if empty.representative() != nil {
		t.Error("an empty group has a representative")
	}
}

func TestWantedLayersFollowViewers(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	g := rig.group
	g.declared.Store(3)
	next := func() []int {
		t.Helper()
		select {
		case l := <-rig.wanted:
			return l
		case <-time.After(2 * time.Second):
			t.Fatal("no announcement")
			return nil
		}
	}
	quiet := func() {
		t.Helper()
		select {
		case l := <-rig.wanted:
			t.Fatalf("unexpected announcement %v", l)
		case <-time.After(50 * time.Millisecond):
		}
	}
	g.refreshWanted()
	if l := next(); len(l) != 1 || l[0] != 0 {
		t.Fatalf("with nobody watching the best layer stays: %v", l)
	}
	g.refreshWanted()
	quiet() // unchanged

	vt, _ := rig.viewer(2)
	// Layer 0 is held for a while, layer 2 is wanted now.
	if l := next(); len(l) != 2 || l[0] != 0 || l[1] != 2 {
		t.Fatalf("viewer on 2: %v", l)
	}
	clock.advance(layerHold + time.Second)
	g.refreshWanted()
	if l := next(); len(l) != 1 || l[0] != 2 {
		t.Fatalf("after the hold: %v", l)
	}
	vt.setPref(-1) // automatic starts at the best layer
	if l := next(); len(l) != 2 || l[0] != 0 || l[1] != 2 {
		t.Fatalf("viewer back to auto: %v", l)
	}
	g.removeViewer(vt)
	quiet() // layers are held, nothing changes yet
	clock.advance(layerHold + time.Second)
	g.refreshWanted()
	if l := next(); len(l) != 1 || l[0] != 0 {
		t.Fatalf("after the last viewer left: %v", l)
	}
}

func TestHeldLayerTimerRefreshes(t *testing.T) {
	rig := newLayerRig(t, "video/VP8", 2)
	g := rig.group
	g.declared.Store(2)
	vt, _ := rig.viewer(1)
	<-rig.wanted
	g.mu.Lock()
	g.hold[0] = time.Now().Add(30 * time.Millisecond)
	g.mu.Unlock()
	vt.setPref(1)
	g.refreshWanted()
	// The hold on layer 0 ends by itself, without any other event.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case l := <-rig.wanted:
			if len(l) == 1 && l[0] == 1 {
				return
			}
		case <-deadline:
			t.Fatal("the hold on an unused layer never ended")
		}
	}
}

func TestSupersededGroupStopsAnnouncing(t *testing.T) {
	rig := newLayerRig(t, "video/VP8", 1)
	g := rig.group
	g.refreshWanted()
	<-rig.wanted
	g.supersede()
	g.refreshWanted()
	select {
	case l := <-rig.wanted:
		t.Fatalf("a replaced share announced %v", l)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestQualityIsReportedToTheViewer(t *testing.T) {
	useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 2)
	vt, _ := rig.viewer(-1)
	rig.pkt(0, 1, 1, vp8Payload(true, true, 1, true, 1, 'a'))
	recv := func() QualityState {
		t.Helper()
		select {
		case st := <-rig.qual:
			return st
		case <-time.After(2 * time.Second):
			t.Fatal("no quality report")
			return QualityState{}
		}
	}
	if st := recv(); st.Layer != 0 || !st.Auto {
		t.Fatalf("first report %+v", st)
	}
	// More packets of the same layer report nothing.
	rig.pkt(0, 2, 2, vp8Payload(true, false, 2, true, 1, 'a'))
	vt.setPref(1) // a choice is confirmed even before the layer changes
	if st := recv(); st.Layer != 0 || st.Auto {
		t.Fatalf("choice report %+v", st)
	}
	rig.pkt(1, 9, 9, vp8Payload(true, true, 9, true, 1, 'b'))
	if st := recv(); st.Layer != 1 || st.Auto {
		t.Fatalf("switch report %+v", st)
	}
	select {
	case st := <-rig.qual:
		t.Fatalf("unexpected report %+v", st)
	case <-time.After(50 * time.Millisecond):
	}
	// Reports for a viewer that is gone stay silent.
	rig.group.removeViewer(vt)
	vt.report(true)
	select {
	case st := <-rig.qual:
		t.Fatalf("report for a closed track %+v", st)
	case <-time.After(50 * time.Millisecond):
	}
	// Without a notifier nothing breaks.
	g := newLayerGroup("n", uuid.New(), uuid.New(), nil, rig.group.codec)
	g.notifyQuality(uuid.New(), QualityState{})
	g.notifyWanted(1)
}

func TestH264SwitchNeedsParameterSets(t *testing.T) {
	useFakeClock(t)
	rig := newLayerRig(t, "video/H264", 2)
	vt, out := rig.viewer(0)
	sps := []byte{0x67, 1, 2}
	idr := []byte{0x65, 4}
	delta := []byte{0x41, 5}
	rig.pkt(0, 1, 1, delta)
	rig.pkt(0, 2, 2, idr) // an IDR slice alone starts a layer that never sends parameter sets
	if got := out.take(); len(got) != 1 || got[0].SequenceNumber != 2 {
		t.Fatalf("first IDR: %v", seqs(got))
	}
	vt.setPref(1)
	rig.pkt(1, 10, 10, sps)
	rig.pkt(1, 11, 10, idr)
	got := out.take()
	if len(got) != 2 || got[0].SequenceNumber != 3 || got[1].SequenceNumber != 4 {
		t.Fatalf("switch at the parameter sets: %v", seqs(got))
	}
	// Once a layer has sent parameter sets, a bare IDR is not trusted.
	vt.setPref(0)
	rig.pkt(0, 20, 20, sps)
	got = out.take()
	if len(got) != 1 {
		t.Fatalf("switch back: %v", seqs(got))
	}
	vt.setPref(1)
	rig.pkt(1, 30, 30, idr)
	if got = out.take(); len(got) != 0 {
		t.Fatalf("an IDR slice without its parameter sets started a layer: %v", seqs(got))
	}
	// A STAP-A with SPS is a start.
	rig.pkt(1, 31, 31, []byte{24, 0, 3, 0x67, 1, 2, 0, 2, 0x65, 4})
	if got = out.take(); len(got) != 1 {
		t.Fatalf("STAP-A start: %v", seqs(got))
	}
}

func TestUnreadableCodecIsForwardedAsOneLayer(t *testing.T) {
	useFakeClock(t)
	rig := newLayerRig(t, "video/VP9", 2)
	vt, out := rig.viewer(1)
	rig.pkt(1, 1, 1, []byte{1, 2, 3})
	if len(out.take()) != 0 {
		t.Fatal("a second layer of an unreadable codec was forwarded")
	}
	vt.setPref(0)
	rig.pkt(0, 2, 2, []byte{1, 2, 3})
	if got := out.take(); len(got) != 1 {
		t.Fatal("the best layer of an unreadable codec is not forwarded")
	}
}

func TestViewerTrackIgnoresClosedAndForwardsErrors(t *testing.T) {
	useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 1)
	vt, out := rig.viewer(0)
	out.err = io.ErrClosedPipe
	rig.pkt(0, 1, 1, vp8Payload(true, true, 1, true, 1, 'a'))
	out.err = errors.New("boom")
	rig.pkt(0, 2, 2, vp8Payload(true, false, 2, true, 1, 'a'))
	if len(out.take()) != 2 {
		t.Fatal("write errors must not stop the stream")
	}
	rig.group.removeViewer(vt)
	rig.pkt(0, 3, 3, vp8Payload(true, false, 3, true, 1, 'a'))
	// A writer that still holds the old list of viewers is ignored as well.
	vt.write(0, &rtp.Packet{Payload: vp8Payload(true, false, 3, true, 1, 'a')})
	if len(out.take()) != 0 {
		t.Fatal("a removed viewer still receives")
	}
	// A viewer replaced by a newer track of the same user stops receiving.
	first, firstOut := rig.viewer(0)
	second := rig.group.newViewer(first.viewer, 0, "t2", "s")
	secondOut := &sunk{}
	second.sink = secondOut.write
	rig.group.addViewer(second)
	rig.pkt(0, 4, 4, vp8Payload(true, true, 4, true, 1, 'a'))
	if len(firstOut.take()) != 0 || len(secondOut.take()) != 1 {
		t.Fatal("the replaced track still receives, or its replacement does not")
	}
	if rig.group.viewerFor(first.viewer) != second {
		t.Fatal("viewerFor does not return the newest track")
	}
	// Without viewers a packet goes nowhere.
	empty := newLayerGroup("e", uuid.New(), uuid.New(), nil, rig.group.codec)
	empty.write(0, &rtp.Packet{})
	if got := rig.group.viewerFor(uuid.New()); got != nil {
		t.Fatal("unknown viewer has a track")
	}
}

func TestViewerKeyframeRequestGoesToItsLayer(t *testing.T) {
	useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 2)
	vt, _ := rig.viewer(1)
	rig.asked(1)
	// Before the first keyframe the wished-for layer is asked.
	vt.requestKeyframe()
	if !rig.asked(1) {
		t.Fatal("the target layer was not asked")
	}
	rig.pkt(1, 1, 1, vp8Payload(true, true, 1, true, 1, 'b'))
	rig.asked(0)
	rig.asked(1)
	vt.requestKeyframe()
	if !rig.asked(1) || rig.asked(0) {
		t.Fatal("the viewer's own request must reach the layer it receives")
	}
	rig.group.requestKeyframe(-1)
	rig.group.requestKeyframe(maxLayers)
	rig.group.detach(rig.infos[0])
	rig.group.requestKeyframe(0)
}
