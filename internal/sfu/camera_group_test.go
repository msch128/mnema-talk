package sfu

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtp"
)

// cameraRig is a layer group of a camera: three layers attached, its sender's
// wanted-layer announcements on rig.wanted.
func cameraRig(t *testing.T) *layerRig {
	t.Helper()
	rig := newLayerRig(t, "video/VP8", 3)
	rig.group.source = SourceCamera
	rig.group.notifier.setWantedCamera(func(_, _ uuid.UUID, l []int) { rig.wanted <- l })
	return rig
}

// key sends a keyframe packet of layer idx.
func (rig *layerRig) key(idx int, seq uint16) {
	rig.pkt(idx, seq, uint32(seq)*3000, vp8Payload(true, true, seq&0x7fff, true, 1, byte(idx)))
}

// delta sends an ordinary frame of layer idx.
func (rig *layerRig) delta(idx int, seq uint16) {
	rig.pkt(idx, seq, uint32(seq)*3000, vp8Payload(true, false, seq&0x7fff, true, 1, byte(idx)))
}

func (rig *layerRig) viewerFit(pref, fit int) (*viewerTrack, *sunk) {
	rig.t.Helper()
	vt, err := rig.group.newViewer(uuid.New(), pref, fit, "track", "stream")
	if err != nil {
		rig.t.Fatal(err)
	}
	out := &sunk{}
	vt.sink = out.write
	rig.group.addViewer(vt)
	return vt, out
}

// checkContinuous asserts that what a viewer got is one stream: consecutive sequence numbers.
func checkContinuous(t *testing.T, pkts []rtp.Packet) {
	t.Helper()
	for i := 1; i < len(pkts); i++ {
		if pkts[i].SequenceNumber != pkts[i-1].SequenceNumber+1 {
			t.Fatalf("packet %d: sequence %d after %d", i, pkts[i].SequenceNumber, pkts[i-1].SequenceNumber)
		}
	}
}

func lastLayerOf(pkts []rtp.Packet) int {
	if len(pkts) == 0 {
		return -1
	}
	return int(pkts[len(pkts)-1].Payload[len(pkts[len(pkts)-1].Payload)-1])
}

// A layer its sender switched off stays attached but silent. A viewer whose
// picture needs that layer is carried by the nearest one that delivers instead
// of looking at nothing, and moves over when the layer is back.
func TestCameraViewerOnASilentLayerIsCarriedByAFlowingOne(t *testing.T) {
	clock := useFakeClock(t)
	rig := cameraRig(t)
	rig.key(0, 100)
	rig.key(2, 200)
	clock.advance(10 * time.Second)
	rig.delta(0, 101) // the thumbnail's sender switched it off; the best layer carries on

	vt, out := rig.viewerFit(-1, 2)
	if !rig.asked(0) || rig.asked(2) {
		t.Fatal("a viewer on a silent thumbnail did not ask for the layer that delivers")
	}
	rig.key(0, 102)
	rig.delta(0, 103)
	if got := out.take(); len(got) != 2 || lastLayerOf(got) != 0 {
		t.Fatalf("the viewer saw %v, want the best layer's frames", seqs(got))
	}

	// The thumbnail is switched on again and delivers: the viewer moves over at its keyframe.
	clock.advance(2 * time.Second)
	rig.delta(2, 201)
	if !rig.asked(2) {
		t.Fatal("the layer coming back did not ask the publisher for its keyframe")
	}
	rig.delta(0, 104)
	if got := out.take(); lastLayerOf(got) != 0 {
		t.Fatalf("the viewer left its layer before a keyframe of the new one: %v", seqs(got))
	}
	rig.key(2, 202)
	rig.delta(0, 105)
	rig.delta(2, 203)
	got := out.take()
	if lastLayerOf(got) != 2 {
		t.Fatalf("after the keyframe the viewer sees layer %d", lastLayerOf(got))
	}
	checkContinuous(t, got)
	if st := vt.state(); st.Layer != 2 {
		t.Fatalf("state %+v", st)
	}
}

// A screen share's viewer chose its layer (or the adapter did) and waits for it.
func TestScreenViewerWaitsForItsSilentLayer(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	rig.key(0, 100)
	rig.key(2, 200)
	clock.advance(10 * time.Second)
	rig.delta(0, 101)
	rig.viewerFit(-1, 2)
	if !rig.asked(2) {
		t.Fatal("a screen viewer did not wait for the layer its picture fits")
	}
}

// A picture that shrinks moves to a smaller layer at its keyframe and one that
// grows moves back; quick changes in a row end on the last one without a break.
func TestCameraViewerFollowsItsPictureSizeAtKeyframes(t *testing.T) {
	clock := useFakeClock(t)
	rig := cameraRig(t)
	for i := 0; i < 3; i++ {
		rig.key(i, uint16(100*(i+1)))
	}
	vt, out := rig.viewerFit(-1, 0)
	rig.asked(0)
	rig.key(0, 101)
	out.take()

	clock.advance(2 * time.Second)
	vt.setFit(2)
	if !rig.asked(2) {
		t.Fatal("a shrinking picture did not ask for a keyframe of the thumbnail")
	}
	rig.delta(2, 301)
	rig.delta(0, 102)
	if got := out.take(); lastLayerOf(got) != 0 {
		t.Fatalf("moved before the thumbnail's keyframe: %v", seqs(got))
	}
	rig.key(2, 302)
	rig.delta(2, 303)
	got := out.take()
	if lastLayerOf(got) != 2 {
		t.Fatalf("the viewer sees layer %d after the keyframe", lastLayerOf(got))
	}
	checkContinuous(t, got)

	// The picture grows: the viewer climbs one layer at a time on its clean reports, at the pace of
	// an enlarged picture (not after a long clean stretch), and moves at the keyframe of each.
	clock.advance(time.Second)
	vt.setFit(0)
	for i := 0; i < 6 && vt.wantA.Load() != 0; i++ {
		clock.advance(time.Second)
		vt.observeReport(0, 0)
	}
	if vt.wantA.Load() != 0 {
		t.Fatalf("a growing picture still wants layer %d", vt.wantA.Load())
	}
	rig.key(0, 103)
	rig.delta(0, 104)
	if st := vt.state(); st.Layer != 0 {
		t.Fatalf("state %+v after the best layer's keyframe", st)
	}
	out.take()

	// Flapping: the last size wins and the stream stays one stream.
	clock.advance(2 * time.Second)
	vt.setFit(2)
	vt.setFit(0)
	vt.setFit(2)
	rig.asked(2)
	rig.key(2, 305)
	rig.delta(2, 306)
	got = out.take()
	checkContinuous(t, got)
	if st := vt.state(); st.Layer != 2 {
		t.Fatalf("state %+v after the last change", st)
	}
}

// The adapter works for a camera as for a screen: severe loss moves an Auto
// viewer down, which the sender hears as a wanted lower layer; a camera has no
// picker, so its viewer is never told a quality.
func TestCameraAutoViewerMovesDownUnderLossAndIsNotToldItsQuality(t *testing.T) {
	clock := useFakeClock(t)
	rig := cameraRig(t)
	for i := 0; i < 3; i++ {
		rig.key(i, uint16(100*(i+1)))
	}
	vt, _ := rig.viewerFit(-1, 0)
	rig.key(0, 101)
	// Reports right after a layer change are ignored; this one comes later.
	clock.advance(adaptGrace + time.Second)
	drainWanted(rig)

	vt.observeReport(0.5, 0)
	if got := int(vt.wantA.Load()); got != 1 {
		t.Fatalf("severe loss left the viewer wanting layer %d, want 1", got)
	}
	var announced []int
	select {
	case announced = <-rig.wanted:
	case <-time.After(time.Second):
		t.Fatal("the sender was not told the lower layer is wanted")
	}
	found := false
	for _, l := range announced {
		found = found || l == 1
	}
	if !found {
		t.Fatalf("wanted layers %v lack layer 1", announced)
	}
	select {
	case st := <-rig.qual:
		t.Fatalf("a camera viewer was told its quality: %+v", st)
	default:
	}
}

func drainWanted(rig *layerRig) {
	for {
		select {
		case <-rig.wanted:
		default:
			return
		}
	}
}
