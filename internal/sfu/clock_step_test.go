package sfu

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// The SFU keeps three times as unix nanoseconds: a layer's last packet
// (layerGroup.lastPkt), a track's last keyframe request (TrackInfo.lastPLI)
// and the moment a track was stopped (TrackInfo.stoppedAt). UnixNano drops
// the monotonic reading of time.Now, so these comparisons follow the wall
// clock: when the host's clock is stepped by d, every later reading is off by
// d against the stored value. The tests below model a step by shifting the
// stored value by d, which gives the same difference now-last as shifting
// every later reading by -d. They record today's behaviour and turn red the
// day it is fixed (the inverse assertion of each is the wanted behaviour).

const clockStep = time.Hour

// stepBackStored makes a stored wall time look d later, as a backward step
// of the wall clock by d does; a negative d models a forward step.
func stepBackStored(v interface {
	Load() int64
	Store(int64)
}, d time.Duration) {
	v.Store(v.Load() + int64(d))
}

func pliTrack(t *testing.T) *TrackInfo {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	return &TrackInfo{SenderID: uuid.New(), Kind: webrtc.RTPCodecTypeVideo, Source: SourceCamera, publisher: &Peer{PC: pc}, ssrc: 7}
}

// acceptedKeyframes asks n times, keyframeMinInterval and a little apart,
// and counts the requests that reached the publisher (lastPLI moved).
func acceptedKeyframes(info *TrackInfo, n int) int {
	got := 0
	for range n {
		time.Sleep(keyframeMinInterval + 20*time.Millisecond)
		before := info.lastPLI.Load()
		info.requestKeyframe()
		if info.lastPLI.Load() != before {
			got++
		}
	}
	return got
}

// KNOWN DEFECT (lane 9, H-L9-01): after the wall clock steps back by d, every
// keyframe request of a track that asked within the d before the step is
// refused until the clock has caught up. New viewers and layer switches wait
// for a keyframe that the SFU never asks for. Wanted: n of n accepted.
func TestKnownDefectClockStepBackBlocksKeyframeRequests(t *testing.T) {
	steady, stepped := pliTrack(t), pliTrack(t)
	steady.requestKeyframe()
	stepped.requestKeyframe()
	if steady.lastPLI.Load() == 0 || stepped.lastPLI.Load() == 0 {
		t.Fatal("the first request was not taken")
	}
	stepBackStored(&stepped.lastPLI, clockStep)
	if got := acceptedKeyframes(steady, 3); got != 3 {
		t.Fatalf("without a step %d of 3 requests reached the publisher", got)
	}
	if got := acceptedKeyframes(stepped, 3); got != 0 {
		t.Fatalf("KNOWN DEFECT fixed? %d of 3 requests passed after a step back of %v", got, clockStep)
	}
}

// The refusal lasts as long as the step: a step of one second blocks for
// about one second plus keyframeMinInterval, then requests pass again.
func TestKnownDefectKeyframeBlockLastsAsLongAsTheStep(t *testing.T) {
	info := pliTrack(t)
	info.requestKeyframe()
	const d = time.Second
	stepBackStored(&info.lastPLI, d)
	start := time.Now()
	for info.lastPLI.Load() > time.Now().UnixNano()-int64(keyframeMinInterval) {
		before := info.lastPLI.Load()
		info.requestKeyframe()
		if info.lastPLI.Load() != before {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	before := info.lastPLI.Load()
	info.requestKeyframe()
	blocked := time.Since(start)
	if info.lastPLI.Load() == before {
		t.Fatal("the request after the block was refused")
	}
	if blocked < d {
		t.Fatalf("KNOWN DEFECT fixed? blocked for %v only after a step of %v", blocked, d)
	}
	t.Logf("a step back of %v refused keyframe requests for %v", d, blocked)
}

func stoppedRoom(t *testing.T) (*Room, *TrackInfo) {
	t.Helper()
	r := &Room{peers: map[uuid.UUID]*Peer{}, trackLocals: map[string]*TrackInfo{}, notifier: &mediaNotifier{}}
	info := &TrackInfo{SenderID: uuid.New(), Kind: webrtc.RTPCodecTypeVideo, Source: SourceCamera}
	r.addTrack("cam", info)
	r.mu.Lock()
	if !r.stopSourceTracksLocked(info.SenderID, SourceCamera) {
		t.Fatal("the camera was not stopped")
	}
	r.mu.Unlock()
	return r, info
}

func published(r *Room) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.trackLocals["cam"] != nil
}

// KNOWN DEFECT (lane 9, H-L9-01): a camera stopped before a backward step
// of d cannot be resumed on the same transceiver until the clock has caught
// up: its packets flow to the SFU and nobody sees them. Wanted: published.
func TestKnownDefectClockStepBackKeepsAResumedCameraHidden(t *testing.T) {
	steadyRoom, steady := stoppedRoom(t)
	stepRoom, stepped := stoppedRoom(t)
	stepBackStored(&stepped.stoppedAt, clockStep)
	time.Sleep(resumeHoldoff + 50*time.Millisecond)
	steadyRoom.resumeStoppedTrack("cam", steady)
	stepRoom.resumeStoppedTrack("cam", stepped)
	if !published(steadyRoom) {
		t.Fatal("without a step the camera did not resume after the holdoff")
	}
	if published(stepRoom) {
		t.Fatal("KNOWN DEFECT fixed? the camera resumed after a step back")
	}
}

// KNOWN DEFECT (lane 9, H-L9-01): a forward step of more than the holdoff
// voids it: a late packet of a camera that was just stopped publishes it
// again at once. Wanted: still stopped within resumeHoldoff.
func TestKnownDefectClockStepForwardVoidsTheResumeHoldoff(t *testing.T) {
	r, info := stoppedRoom(t)
	stepBackStored(&info.stoppedAt, -clockStep)
	r.resumeStoppedTrack("cam", info)
	if !published(r) {
		t.Fatal("KNOWN DEFECT fixed? a late packet after a forward step did not republish the camera")
	}
	steadyRoom, steady := stoppedRoom(t)
	steadyRoom.resumeStoppedTrack("cam", steady)
	if published(steadyRoom) {
		t.Fatal("without a step a late packet republished the camera")
	}
}

// set sets the fake layer clock, backward as well as forward.
func (c *fakeClock) set(at time.Time) {
	c.mu.Lock()
	c.now = at
	c.mu.Unlock()
}

// KNOWN DEFECT (lane 9, H-L9-01): a camera layer its sender switched off
// counts as flowing for the size of a backward step instead of one second.
// The case: the sender's browser stops its best layer under a bandwidth limit
// (the layer is wanted, so being asked for it again changes nothing). A new
// viewer whose picture fits that layer is put on it and sees nothing,
// although the thumbnail delivers a keyframe every minute. Wanted: the
// viewer is carried by the flowing layer at once, as without a step
// (TestCameraViewerOnASilentLayerIsCarriedByAFlowingOne).
func TestKnownDefectClockStepBackKeepsASilentLayerFlowing(t *testing.T) {
	if blind := blindAfterStep(t, 0); blind != 0 {
		t.Fatalf("without a step the viewer saw nothing for %v", blind)
	}
	blind := blindAfterStep(t, clockStep)
	if blind < clockStep-time.Minute {
		t.Fatalf("KNOWN DEFECT fixed? the viewer saw the flowing layer after %v", blind)
	}
	t.Logf("after a step back of %v a new viewer saw nothing for %v while another layer delivered", clockStep, blind)
}

// blindAfterStep is how long a new camera viewer whose picture fits the
// stopped best layer sees nothing, when the wall clock stepped back by step
// ten seconds after that layer's last packet. Measured in minutes.
func blindAfterStep(t *testing.T, step time.Duration) time.Duration {
	clock := useFakeClock(t)
	rig := cameraRig(t)
	rig.key(2, 200)
	rig.key(0, 100) // the best layer's last packet, then the browser stops it
	clock.advance(10 * time.Second)
	rig.delta(2, 201)
	clock.set(clock.now.Add(-step))

	_, out := rig.viewerFit(-1, 0)
	blind, seq := time.Duration(0), uint16(202)
	for ; blind < 2*clockStep; blind += time.Minute {
		rig.key(2, seq)
		rig.delta(2, seq+1)
		seq += 2
		if len(out.take()) > 0 {
			return blind
		}
		clock.advance(time.Minute)
	}
	return blind
}
