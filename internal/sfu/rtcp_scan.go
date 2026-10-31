package sfu

import "github.com/pion/rtcp"

// RTCP packet types and feedback formats the SFU looks at.
const (
	rtcpTypeTransportFeedback = 205
	rtcpTypePayloadFeedback   = 206
	fmtNACK                   = 1
	fmtTransportCC            = 15
	fmtPLI                    = 1
	fmtFIR                    = 4
)

// scanRTCP calls visit for every packet of a compound RTCP batch with its
// header fields and bytes, reading headers only. A batch that is cut short
// ends the scan. The interceptors that look at feedback (the bandwidth
// estimator and the telemetry counters) each need a few packet kinds out of
// batches that mostly hold receiver reports; scanning headers and decoding
// only the kinds wanted keeps every batch from being parsed in full twice.
func scanRTCP(compound []byte, visit func(packetType byte, format uint8, raw []byte)) {
	for len(compound) >= 4 {
		size := 4 * (1 + (int(compound[2])<<8 | int(compound[3])))
		if size > len(compound) {
			return
		}
		visit(compound[1], compound[0]&0x1f, compound[:size])
		compound = compound[size:]
	}
}

// transportFeedbackOf decodes the transport-wide congestion control feedback
// packets of a batch.
func transportFeedbackOf(compound []byte) []*rtcp.TransportLayerCC {
	var out []*rtcp.TransportLayerCC
	scanRTCP(compound, func(typ byte, format uint8, raw []byte) {
		if typ != rtcpTypeTransportFeedback || format != fmtTransportCC {
			return
		}
		fb := &rtcp.TransportLayerCC{}
		if fb.Unmarshal(raw) == nil {
			out = append(out, fb)
		}
	})
	return out
}
