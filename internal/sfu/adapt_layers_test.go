package sfu

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtcp"
)

func TestAutomaticViewerFollowsItsReports(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	rig.group.declared.Store(3)
	vt, out := rig.viewer(-1)
	rig.pkt(0, 100, 1000, vp8Payload(true, true, 10, true, 1, 'a'))
	if st := vt.state(); st.Layer != 0 || !st.Auto {
		t.Fatalf("starts on %+v", st)
	}

	// The keyframe burst of the start is not held against the link.
	clock.advance(time.Second)
	vt.observeReport(0.9, 0)
	if vt.wantA.Load() != 0 {
		t.Fatal("moved down during the start")
	}

	// Trouble moves it down one layer, once the publisher has been asked for it.
	clock.advance(10 * time.Second)
	rig.asked(1)
	vt.observeReport(0.5, 0)
	if got := vt.wantA.Load(); got != 1 {
		t.Fatalf("wants layer %d after severe loss", got)
	}
	if !rig.asked(1) {
		t.Fatal("no keyframe asked of the layer the viewer is moving to")
	}
	rig.pkt(1, 500, 5, vp8Payload(true, true, 50, true, 1, 'b'))
	if st := vt.state(); st.Layer != 1 || !st.Auto {
		t.Fatalf("after the move %+v", st)
	}
	if got := out.take(); len(got) != 2 || got[1].SequenceNumber != 101 {
		t.Fatalf("stream across the move: %v", seqs(got))
	}

	// And once more, to the last layer, but no further.
	clock.advance(10 * time.Second)
	vt.observeReport(0.5, 0)
	rig.pkt(2, 900, 9, vp8Payload(true, true, 90, true, 1, 'c'))
	if st := vt.state(); st.Layer != 2 {
		t.Fatalf("second move: %+v", st)
	}
	clock.advance(10 * time.Second)
	vt.observeReport(0.5, 0)
	if vt.wantA.Load() != 2 {
		t.Fatal("moved below the worst layer")
	}

	// A long clean stretch brings it back up, one layer.
	for range adaptCleanFor/time.Second + 1 {
		clock.advance(time.Second)
		vt.observeReport(0, 20*time.Millisecond)
	}
	if got := vt.wantA.Load(); got != 1 {
		t.Fatalf("wants layer %d after a long clean stretch, want 1", got)
	}
	rig.pkt(1, 501, 6, vp8Payload(true, true, 51, true, 2, 'b'))
	if st := vt.state(); st.Layer != 1 || !st.Auto {
		t.Fatalf("after moving up %+v", st)
	}
}

func TestPublisherLearnsWhatAutomaticViewersNeed(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	rig.group.declared.Store(3)
	vt, _ := rig.viewer(-1)
	if l := <-rig.wanted; len(l) != 1 || l[0] != 0 {
		t.Fatalf("first announcement %v", l)
	}
	// Within the hold on layer 0.
	clock.advance(5 * time.Second)
	vt.observeReport(0.5, 0)
	// Layer 0 is held for a while; layer 1 is wanted now.
	if l := <-rig.wanted; len(l) != 2 || l[0] != 0 || l[1] != 1 {
		t.Fatalf("after a move down: %v", l)
	}
}

func TestChoiceOfAViewerIsNotAdapted(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	rig.group.declared.Store(3)
	vt, _ := rig.viewer(0)
	clock.advance(10 * time.Second)
	vt.observeReport(0.9, time.Second)
	for range 5 {
		vt.viewerKeyframe()
	}
	vt.observeReport(0.9, time.Second)
	if vt.wantA.Load() != 0 || vt.ad != nil {
		t.Fatal("a chosen layer was adapted")
	}
	// A closed track takes no report.
	auto, _ := rig.viewer(-1)
	rig.group.removeViewer(auto)
	clock.advance(10 * time.Second)
	auto.observeReport(0.9, 0)
	if auto.wantA.Load() != 0 {
		t.Fatal("a closed track adapted")
	}
}

func TestSwitchingToAutomaticContinuesFromTheChosenLayer(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	rig.group.declared.Store(3)
	vt, _ := rig.viewer(2)
	vt.setPref(-1)
	if vt.ad == nil || vt.ad.layer != 2 || vt.wantA.Load() != 2 {
		t.Fatalf("automatic starts on layer %d, want the chosen 2", vt.ad.layer)
	}
	// Choosing automatic again keeps its state.
	state := vt.ad
	vt.setPref(-1)
	if vt.ad != state {
		t.Fatal("automatic restarted")
	}
	// Choosing a layer ends adaptation.
	vt.setPref(1)
	if vt.ad != nil || vt.wantA.Load() != 1 {
		t.Fatal("a chosen layer is still adapted")
	}
	// And the worst layer allowed follows what the share declares.
	vt.setPref(-1)
	clock.advance(time.Second)
	rig.group.setDeclared(2)
	if vt.ad.floor != 1 || vt.wantA.Load() != 1 {
		t.Fatalf("floor %d layer %d after the share dropped a layer", vt.ad.floor, vt.wantA.Load())
	}
}

func TestViewerKeyframeRequestsAreEvidenceOfTrouble(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 2)
	rig.group.declared.Store(2)
	vt, _ := rig.viewer(-1)
	clock.advance(10 * time.Second)
	for range adaptPLIs {
		vt.viewerKeyframe()
	}
	vt.observeReport(0, 0)
	if vt.wantA.Load() != 1 {
		t.Fatal("repeated keyframe requests did not move the viewer down")
	}
}

func TestFloorOfAShare(t *testing.T) {
	rig := newLayerRig(t, "video/VP8", 3)
	g := rig.group
	g.declared.Store(2)
	if g.floor() != 1 {
		t.Fatalf("declared 2: floor %d", g.floor())
	}
	g.declared.Store(9)
	if g.floor() != maxLayers-1 {
		t.Fatalf("declared too many: floor %d", g.floor())
	}
	g.declared.Store(0)
	if g.floor() != 2 {
		t.Fatalf("three layers live: floor %d", g.floor())
	}
	g.detach(rig.infos[2])
	g.detach(rig.infos[1])
	if g.floor() != 0 {
		t.Fatalf("one layer live: floor %d", g.floor())
	}
}

// A viewer's receiver report, as its browser sends it, moves it down; the stream
// the browser sees stays one continuous stream.
func TestReceiverReportsOfARealViewerMoveItDown(t *testing.T) {
	clock := useFakeClock(t)
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	pub.publishLayers(25)
	pub.join(s, room)
	viewer := newClient(t)
	viewer.join(s, room)
	r := s.Room(room)
	waitSources(t, r, pub.id, SourceScreen, 3)
	if err := r.SetScreenLayers(pub.id, []Layer{{1920, 1080, 30}, {1280, 720, 30}, {640, 360, 30}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Subscribe(viewer.id, pub.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	track := waitTrack(t, viewer)
	checkOneStream(t, waitLayer(t, viewer, 0))

	report := func(loss uint8) {
		t.Helper()
		err := viewer.pc.WriteRTCP([]rtcp.Packet{&rtcp.ReceiverReport{
			SSRC:    uint32(track.SSRC()) + 1,
			Reports: []rtcp.ReceptionReport{{SSRC: uint32(track.SSRC()), FractionLost: loss}},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	for want := 1; want <= 2; want++ {
		clock.advance(10 * time.Second)
		report(200)
		checkOneStream(t, waitLayer(t, viewer, want))
		st, ok := r.ScreenQuality(viewer.id, pub.id)
		if !ok || st.Layer != want || !st.Auto {
			t.Fatalf("after loss %d: %+v ok=%v", want, st, ok)
		}
	}
}
