package sfu

import (
	"testing"

	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

func TestTelemetryCountsFeedbackAndEgress(t *testing.T) {
	icp, err := telemetryFactory{}.NewInterceptor("")
	if err != nil {
		t.Fatal(err)
	}

	nacks, pairs := metrics.SFUNackRequests.Load(), metrics.SFUNackPackets.Load()
	keys := metrics.SFUKeyframeRequests.Load()
	raw, err := rtcp.Marshal([]rtcp.Packet{
		// Pair 1 loses 4 packets (base + bitmap 0b111), pair 2 one.
		&rtcp.TransportLayerNack{Nacks: []rtcp.NackPair{{PacketID: 10, LostPackets: 0b111}, {PacketID: 40}}},
		&rtcp.PictureLossIndication{},
		&rtcp.FullIntraRequest{FIR: []rtcp.FIREntry{{SSRC: 1}}},
		&rtcp.ReceiverReport{},
	})
	if err != nil {
		t.Fatal(err)
	}
	reader := icp.BindRTCPReader(interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		return copy(b, raw), a, nil
	}))
	if _, _, err := reader.Read(make([]byte, 1500), nil); err != nil {
		t.Fatal(err)
	}
	if got := metrics.SFUNackRequests.Load() - nacks; got != 1 {
		t.Fatalf("nack requests = %d", got)
	}
	if got := metrics.SFUNackPackets.Load() - pairs; got != 5 {
		t.Fatalf("nack packets = %d, want 5", got)
	}
	if got := metrics.SFUKeyframeRequests.Load() - keys; got != 2 {
		t.Fatalf("keyframe requests = %d", got)
	}

	// Garbage and empty reads count nothing and pass through.
	countFeedback([]byte{1, 2, 3})
	empty := icp.BindRTCPReader(interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		return 0, a, nil
	}))
	if _, _, err := empty.Read(make([]byte, 8), nil); err != nil {
		t.Fatal(err)
	}

	pkts, bytes := metrics.SFUEgressPackets.Load(), metrics.SFUEgressBytes.Load()
	fail := false
	writer := icp.BindLocalStream(&interceptor.StreamInfo{}, interceptor.RTPWriterFunc(func(h *rtp.Header, p []byte, _ interceptor.Attributes) (int, error) {
		if fail {
			return 0, errTelemetryTest
		}
		return 12 + len(p), nil
	}))
	if _, err := writer.Write(&rtp.Header{}, make([]byte, 100), nil); err != nil {
		t.Fatal(err)
	}
	fail = true
	if _, err := writer.Write(&rtp.Header{}, make([]byte, 100), nil); err == nil {
		t.Fatal("write error swallowed")
	}
	if metrics.SFUEgressPackets.Load()-pkts != 1 || metrics.SFUEgressBytes.Load()-bytes != 112 {
		t.Fatal("egress counts wrong: only the successful write may count")
	}
}

type telemetryTestError struct{}

func (telemetryTestError) Error() string { return "write failed" }

var errTelemetryTest error = telemetryTestError{}
