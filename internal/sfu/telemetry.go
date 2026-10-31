package sfu

import (
	"math/bits"

	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// telemetryFactory counts what leaves for subscribers (including cached
// retransmissions, since it sits beneath the NACK responder) and the
// feedback subscribers send back. It only reads packet headers.
type telemetryFactory struct{}

func (telemetryFactory) NewInterceptor(string) (interceptor.Interceptor, error) {
	return &telemetryInterceptor{}, nil
}

type telemetryInterceptor struct{ interceptor.NoOp }

func (*telemetryInterceptor) BindLocalStream(_ *interceptor.StreamInfo, w interceptor.RTPWriter) interceptor.RTPWriter {
	return interceptor.RTPWriterFunc(func(h *rtp.Header, payload []byte, a interceptor.Attributes) (int, error) {
		n, err := w.Write(h, payload, a)
		if err == nil {
			metrics.SFUEgressPackets.Inc()
			metrics.SFUEgressBytes.Add(uint64(n))
		}
		return n, err
	})
}

func (*telemetryInterceptor) BindRTCPReader(r interceptor.RTCPReader) interceptor.RTCPReader {
	return interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		n, attr, err := r.Read(b, a)
		if err == nil && n > 0 {
			countFeedback(b[:n])
		}
		return n, attr, err
	})
}

// countFeedback tallies NACK, PLI and FIR feedback in an RTCP batch. A PLI or
// FIR is counted from its header alone and only a NACK is decoded (for the
// packets it asks for); the receiver reports and transport feedback that make
// up most batches are not parsed here.
func countFeedback(raw []byte) {
	scanRTCP(raw, func(typ byte, format uint8, pkt []byte) {
		switch {
		case typ == rtcpTypeTransportFeedback && format == fmtNACK:
			countNack(pkt)
		case typ == rtcpTypePayloadFeedback && (format == fmtPLI || format == fmtFIR):
			metrics.SFUKeyframeRequests.Inc()
		}
	})
}

func countNack(raw []byte) {
	var nack rtcp.TransportLayerNack
	if nack.Unmarshal(raw) != nil {
		return
	}
	metrics.SFUNackRequests.Inc()
	for _, pair := range nack.Nacks {
		metrics.SFUNackPackets.Add(uint64(bits.OnesCount16(uint16(pair.LostPackets))) + 1)
	}
}
