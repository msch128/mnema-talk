package sfu

import (
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// A forwarded packet costs the garbage collector nothing: the copy that
// carries the viewer's own numbering lives in the track, not on the heap.
func TestForwardingAPacketToAViewerDoesNotAllocate(t *testing.T) {
	useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	vt, _ := rig.viewer(0)
	vt.sink = func(*rtp.Packet) error { return nil }
	pkt := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: 7, Timestamp: 90}, Payload: vp8Payload(true, false, 9, true, 5, 'a')}
	now := time.Now()
	allocs := testing.AllocsPerRun(200, func() {
		vt.mu.Lock()
		vt.sendPacketLocked(pkt, now)
		vt.mu.Unlock()
	})
	if allocs != 0 {
		t.Fatalf("%v allocations per forwarded packet, want 0", allocs)
	}
}

// The feedback a viewer sends every second or so is mostly receiver reports
// and keyframe requests; counting them decodes nothing.
func TestCountingFeedbackThatNeedsNoDecodingDoesNotAllocate(t *testing.T) {
	raw, err := rtcp.Marshal([]rtcp.Packet{
		&rtcp.ReceiverReport{SSRC: 1, Reports: []rtcp.ReceptionReport{{SSRC: 2}}},
		&rtcp.PictureLossIndication{SenderSSRC: 1, MediaSSRC: 2},
		&rtcp.FullIntraRequest{FIR: []rtcp.FIREntry{{SSRC: 2}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := metrics.SFUKeyframeRequests.Load()
	if allocs := testing.AllocsPerRun(200, func() { countFeedback(raw) }); allocs != 0 {
		t.Fatalf("%v allocations counting a batch of reports and keyframe requests, want 0", allocs)
	}
	if got := metrics.SFUKeyframeRequests.Load() - before; got%2 != 0 || got == 0 {
		t.Fatalf("counted %d keyframe requests", got)
	}
}

func TestCountFeedbackIgnoresAMalformedNackAndTruncatedBatches(t *testing.T) {
	before := metrics.SFUNackRequests.Load()
	// A NACK header whose body is too short to decode.
	countFeedback([]byte{0x81, rtcpTypeTransportFeedback, 0, 1, 0, 0, 0, 1})
	// A batch that claims more than it holds.
	countFeedback([]byte{0x81, rtcpTypeTransportFeedback, 0, 9, 0, 0, 0, 1})
	if got := metrics.SFUNackRequests.Load() - before; got != 0 {
		t.Fatalf("counted %d nack requests from malformed input", got)
	}
}
