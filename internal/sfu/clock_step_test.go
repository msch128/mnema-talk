package sfu

import (
	"testing"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// The SFU stores the times it compares later as steadyNanos: a layer's last
// packet (layerGroup.lastPkt), a track's last keyframe request
// (TrackInfo.lastPLI), the moment a track was stopped (TrackInfo.stoppedAt),
// a resend (nackSlot.resentAt) and a bitrate measurement (layerGroup.rateAt).
// They used to be unix nanoseconds, which follow the wall clock (lane 9,
// H-L9-01): a step of the host's clock by d moved every deadline by d. The
// tests below step the wall clock the way the host does: every later reading
// of the SFU's clock keeps its monotonic reading and has its wall time moved
// by d (wallStepped). A time without a monotonic reading cannot model this.

const clockStep = time.Hour

// timeLayout is the layout of time.Time (src/time/time.go): with
// hasMonotonic set in wall, bits 30 to 62 of wall are the seconds since
// 1885 and ext is the monotonic reading.
type timeLayout struct {
	wall uint64
	ext  int64
	loc  *time.Location
}

const hasMonotonic = 1 << 63

// wallStepped is at as time.Now reads it after the host's clock was stepped
// by d (whole seconds): the same monotonic reading, the wall time moved by d.
// Go has no API for such a reading, so the wall field is moved in place;
// checkWallStep proves the layout before a test relies on it.
func wallStepped(at time.Time, d time.Duration) time.Time {
	v := (*timeLayout)(unsafe.Pointer(&at))
	if v.wall&hasMonotonic != 0 {
		v.wall = uint64(int64(v.wall) + int64(d/time.Second)<<30)
	}
	return at
}

func checkWallStep(t *testing.T) {
	t.Helper()
	at := time.Now()
	got := wallStepped(at, -clockStep)
	if got.Sub(at) != 0 || at.Round(0).Sub(got.Round(0)) != clockStep {
		t.Fatalf("wallStepped does not model a wall clock step: monotonic moved by %v, wall by %v",
			got.Sub(at), at.Round(0).Sub(got.Round(0)))
	}
}

// stepWallClock makes the SFU's clock read the real time with its wall
// clock stepped by d (negative: back) until the test ends.
func stepWallClock(t *testing.T, d time.Duration) {
	t.Helper()
	checkWallStep(t)
	prev := layerClock.Load()
	now := func() time.Time { return wallStepped(time.Now(), d) }
	layerClock.Store(&now)
	t.Cleanup(func() { layerClock.Store(prev) })
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

// After the wall clock steps back, keyframe requests keyframeMinInterval
// apart still reach the publisher (H-L9-01: none did for the size of the step).
func TestClockStepBackKeepsKeyframeRequests(t *testing.T) {
	info := pliTrack(t)
	info.requestKeyframe()
	if info.lastPLI.Load() == 0 {
		t.Fatal("the first request was not taken")
	}
	stepWallClock(t, -clockStep)
	if got := acceptedKeyframes(info, 3); got != 3 {
		t.Fatalf("%d of 3 requests passed after a step back of %v", got, clockStep)
	}
}

// A forward step does not void the rate limit: a request right after one is
// refused, whatever the wall clock says.
func TestClockStepForwardKeepsTheKeyframeRateLimit(t *testing.T) {
	info := pliTrack(t)
	info.requestKeyframe()
	stepWallClock(t, clockStep)
	before := info.lastPLI.Load()
	info.requestKeyframe()
	if info.lastPLI.Load() != before {
		t.Fatal("a request right after another passed after a forward step")
	}
}

// A step back of a second delays no request: the next one after
// keyframeMinInterval passes (it used to wait for the step plus the interval).
func TestKeyframeRateLimitIgnoresASmallStepBack(t *testing.T) {
	info := pliTrack(t)
	info.requestKeyframe()
	stepWallClock(t, -time.Second)
	if got := acceptedKeyframes(info, 1); got != 1 {
		t.Fatal("a step back of 1s refused the next keyframe request")
	}
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

// A camera stopped before a backward step resumes on the same transceiver
// after the holdoff (H-L9-01: it stayed hidden for the size of the step).
func TestClockStepBackResumesAStoppedCamera(t *testing.T) {
	r, info := stoppedRoom(t)
	stepWallClock(t, -clockStep)
	time.Sleep(resumeHoldoff + 50*time.Millisecond)
	r.resumeStoppedTrack("cam", info)
	if !published(r) {
		t.Fatal("the camera did not resume after a step back")
	}
}

// A forward step keeps the holdoff: a late packet of a camera stopped a
// moment ago does not publish it again (H-L9-01: it did).
func TestClockStepForwardKeepsTheResumeHoldoff(t *testing.T) {
	r, info := stoppedRoom(t)
	stepWallClock(t, clockStep)
	r.resumeStoppedTrack("cam", info)
	if published(r) {
		t.Fatal("a late packet after a forward step republished the camera")
	}
}

// set sets the fake layer clock, backward as well as forward.
func (c *fakeClock) set(at time.Time) {
	c.mu.Lock()
	c.now = at
	c.mu.Unlock()
}

// A camera layer its sender switched off counts as flowing for one second,
// whatever the wall clock does. The case: the sender's browser stops its best
// layer under a bandwidth limit (the layer is wanted, so being asked for it
// again changes nothing). A new viewer whose picture fits that layer is
// carried by the flowing thumbnail at once, as without a step
// (TestCameraViewerOnASilentLayerIsCarriedByAFlowingOne). H-L9-01: it saw
// nothing for the size of the step.
func TestClockStepBackKeepsASilentLayerSilent(t *testing.T) {
	checkWallStep(t)
	for _, step := range []time.Duration{0, clockStep} {
		if blind := blindAfterStep(t, step); blind != 0 {
			t.Fatalf("after a step back of %v a new viewer saw nothing for %v", step, blind)
		}
	}
}

// A packet resent before a backward step may be resent again once the
// interval has passed (it was refused for the size of the step).
func TestClockStepBackKeepsResends(t *testing.T) {
	checkWallStep(t)
	h := newHistory(1 << 20)
	at := time.Now()
	addPacket(h, 9, 100, at)
	if _, _, ok := h.find(9, at, time.Second); !ok {
		t.Fatal("the first resend was refused")
	}
	if _, _, ok := h.find(9, wallStepped(at.Add(2*time.Second), -clockStep), time.Second); !ok {
		t.Fatal("a resend after the interval was refused after a step back")
	}
}

// The layers' bitrates are measured every rateEvery across a backward step
// (no measurement was taken for the size of the step).
func TestClockStepBackKeepsMeasuringBitrates(t *testing.T) {
	checkWallStep(t)
	g := newLayerGroup("g", uuid.New(), uuid.New(), &mediaNotifier{}, webrtc.RTPCodecCapability{MimeType: "video/VP8"})
	at := time.Now()
	g.sampleRates(at)
	first := g.rateAt
	g.sampleRates(wallStepped(at.Add(rateEvery), -clockStep))
	if g.rateAt == first {
		t.Fatal("no measurement after rateEvery and a step back")
	}
}

// blindAfterStep is how long a new camera viewer whose picture fits the
// stopped best layer sees nothing, when the wall clock stepped back by step
// ten seconds after that layer's last packet. Measured in minutes. The fake
// clock starts from a real reading, so it carries a monotonic reading.
func blindAfterStep(t *testing.T, step time.Duration) time.Duration {
	clock := useFakeClock(t)
	clock.set(time.Now())
	rig := cameraRig(t)
	rig.key(2, 200)
	rig.key(0, 100) // the best layer's last packet, then the browser stops it
	clock.advance(10 * time.Second)
	rig.delta(2, 201)
	clock.set(wallStepped(clock.now, -step))

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
