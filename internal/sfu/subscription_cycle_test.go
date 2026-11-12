package sfu

import (
	"os"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// Subscription cycle (H-L8-09): one viewer switches its subscription to one
// publisher's screen share on and off, through Room.Subscribe, the entry
// point of the WebSocket handler's webrtc_subscribe. Each switch is one
// event; the viewer's media connection stays up. Nothing of a stopped sender
// may be retained: the heap in use (after two forced collections, every 100
// switches) grows by at most subPerSwitchBound per switch, the goroutine
// count and the offer stay flat. What does stay, Pion's SRTP state of each
// sender's SSRCs, is bounded by maxSentTracks (TestSenderCapClosesTheConnection).
//
// Smoke runs 2000 switches; SFU_SUB_SWITCHES=n (or SFU_CYCLE_DEEP=1 for 20000)
// runs more with the same assertions. SFU_SUB_HEAPPROFILE=path writes heap
// profiles after the warm-up (path.base), after half the switches
// (path.half) and at the end (path).

func subSwitches(smoke int) int {
	if v := os.Getenv("SFU_SUB_SWITCHES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	if os.Getenv("SFU_CYCLE_DEEP") == "1" {
		return 20000
	}
	return smoke
}

// subPerSwitchBound is the heap growth per switch the cycle may show. A
// stopped sender kept with its writer chain costs about 7 KB (3.5 KB a
// switch); Pion's SRTP state of each sender on both ends of the viewer's
// connection (about 160 bytes on each) and the collector's noise stay below.
const subPerSwitchBound = 1536

func writeHeapProfile(path string) {
	if f, err := os.Create(path); err == nil {
		runtime.GC()
		_ = pprof.WriteHeapProfile(f)
		_ = f.Close()
	}
}

// subscriptionRoom is a Talk of a publisher sharing a screen and a viewer,
// both connected.
func subscriptionRoom(t *testing.T) (r *Room, viewer, publisher *client) {
	t.Helper()
	s := newTestSFU(t)
	room := uuid.New()
	publisher = newClient(t)
	track := publisher.publishVideo("cycle-screen")
	publisher = joinMember(t, s, room, publisher)
	fillSendHistory(track)
	viewer = joinMember(t, s, room, newClient(t))
	return s.Room(room), viewer, publisher
}

// drainClient empties what the test client keeps of the tracks it receives,
// so the harness neither blocks Pion's track goroutines nor holds packets.
func drainClient(c *client) {
	for {
		select {
		case <-c.tracks:
		case <-c.received:
		default:
			return
		}
	}
}

// subscriptionCycle runs the cycle and returns the heap growth per switch
// (over the checkpoints after the first), the goroutine growth
// and the size of the viewer's offer before and after.
func subscriptionCycle(t *testing.T, switches int) (perSwitch float64, goGrowth, sdpBefore, sdpAfter int) {
	t.Helper()
	profile := os.Getenv("SFU_SUB_HEAPPROFILE")
	if profile != "" {
		runtime.MemProfileRate = 1024
	}
	r, viewer, publisher := subscriptionRoom(t)
	for range 100 { // warm-up: Pion's pools and lazy goroutines
		toggleScreen(t, r, viewer, publisher)
		drainClient(viewer)
	}
	time.Sleep(300 * time.Millisecond)
	drainClient(viewer)
	sdpBefore = offerSize(viewer.peer)
	goBase := runtime.NumGoroutine()
	if profile != "" {
		writeHeapProfile(profile + ".base")
	}
	var xs, ys []float64
	for done := 0; done < switches; done += 2 {
		if done%100 == 0 {
			drainClient(viewer)
			xs, ys = append(xs, float64(done)), append(ys, float64(heapAfterGC()))
		}
		if profile != "" && done == switches/2 {
			writeHeapProfile(profile + ".half")
		}
		toggleScreen(t, r, viewer, publisher)
	}
	drainClient(viewer)
	xs, ys = append(xs, float64(switches)), append(ys, float64(heapAfterGC()))
	if profile != "" {
		writeHeapProfile(profile)
	}
	// The last offer, which removes the track, may still be on its way.
	time.Sleep(300 * time.Millisecond)
	sdpAfter = offerSize(viewer.peer)
	goGrowth = runtime.NumGoroutine() - goBase
	t.Logf("heap in use every 100 switches: %v", ys)
	return slope(xs[1:], ys[1:]), goGrowth, sdpBefore, sdpAfter
}

// fillSendHistory writes one history's worth of packets to the publisher's
// track at once. The test client's own NACK responder keeps the last 1024
// packets it sent, which at the helper's 50 packets a second would fill
// during the cycle and look like growth of the SFU.
func fillSendHistory(track *webrtc.TrackLocalStaticRTP) {
	for i := range 1100 {
		_ = track.WriteRTP(&rtp.Packet{
			Header:  rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: uint16(30000 + i), Timestamp: uint32(i) * 3000},
			Payload: []byte{0x10, 0x00, 0x00, 0x9d, 0x01, 0x2a},
		})
	}
}

// slope is the median of the slopes between every two checkpoints
// (Theil-Sen): a checkpoint that caught a renegotiation in flight moves it
// little.
func slope(xs, ys []float64) float64 {
	var slopes []float64
	for i := range xs {
		for j := i + 1; j < len(xs); j++ {
			slopes = append(slopes, (ys[j]-ys[i])/(xs[j]-xs[i]))
		}
	}
	slices.Sort(slopes)
	return slopes[len(slopes)/2]
}

// offerSize is the size of the SFU's last offer to peer.
func offerSize(p *Peer) int {
	if d := p.PC.CurrentLocalDescription(); d != nil {
		return len(d.SDP)
	}
	return 0
}

// TestSubscriptionSwitchesRetainNothing is the defender's resource test of
// H-L8-09.
func TestSubscriptionSwitchesRetainNothing(t *testing.T) {
	switches := subSwitches(2000)
	perSwitch, goGrowth, sdpBefore, sdpAfter := subscriptionCycle(t, switches)
	t.Logf("%d switches: heap %+.0f bytes per switch, goroutines %+d, offer %d -> %d bytes",
		switches, perSwitch, goGrowth, sdpBefore, sdpAfter)
	if goGrowth > 8 {
		t.Errorf("goroutines grew by %d over %d switches", goGrowth, switches)
	}
	if sdpAfter > sdpBefore+256 {
		t.Errorf("the viewer's offer grew from %d to %d bytes over %d switches", sdpBefore, sdpAfter, switches)
	}
	if perSwitch > subPerSwitchBound {
		t.Errorf("each switch retains %.0f bytes (bound %d)", perSwitch, subPerSwitchBound)
	}
}

// TestSenderCapClosesTheConnection: what Pion keeps per sender cannot be
// dropped, so a connection that made more than its cap of senders is closed.
// The cap is lowered for the test; up to it the viewer stays connected.
func TestSenderCapClosesTheConnection(t *testing.T) {
	r, viewer, publisher := subscriptionRoom(t)
	peer := viewer.peer
	const more = 30
	peer.rtcp.mu.Lock()
	peer.rtcp.senderCap = peer.rtcp.senders + more
	peer.rtcp.mu.Unlock()
	for range more { // one sender each
		toggleScreen(t, r, viewer, publisher)
		drainClient(viewer)
	}
	if state := peer.PC.ConnectionState(); state != webrtc.PeerConnectionStateConnected {
		t.Fatalf("the viewer's connection is %s after %d senders, its cap", state, more)
	}
	if err := r.Subscribe(viewer.id, publisher.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	waitSFUCondition(t, "the connection closed past the cap", func() bool {
		return peer.PC.ConnectionState() == webrtc.PeerConnectionStateClosed && r.GetPeer(viewer.id) == nil
	})
}
