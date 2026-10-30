package sfu

import (
	"time"

	"github.com/pion/rtcp"
)

// receiverReportOf extracts what the adapter needs from a viewer's receiver
// report for the stream with the given SSRC: the loss fraction and the round
// trip time (zero when it cannot be told). ok is false when the report says
// nothing about that stream.
func receiverReportOf(rr *rtcp.ReceiverReport, ssrc uint32, now time.Time) (loss float64, rtt time.Duration, ok bool) {
	for _, r := range rr.Reports {
		if r.SSRC != ssrc {
			continue
		}
		loss = float64(r.FractionLost) / 256
		if r.LastSenderReport != 0 {
			// Both are in 1/65536 seconds, on the 32 middle bits of the NTP time.
			elapsed := ntpMiddle32(now) - r.LastSenderReport - r.Delay
			// A wrapped or implausible value (a clock step, a stale report) is no measurement.
			if secs := time.Duration(elapsed) * time.Second / 65536; elapsed < 1<<31 && secs < 10*time.Second {
				rtt = secs
			}
		}
		return loss, rtt, true
	}
	return 0, 0, false
}

// ntpMiddle32 is the middle 32 bits of the NTP timestamp of t, the unit of
// the LSR and DLSR fields of a report block.
func ntpMiddle32(t time.Time) uint32 {
	const ntpEpochOffset = 2208988800 // seconds between 1900 and 1970
	secs := uint64(t.Unix()) + ntpEpochOffset
	frac := uint64(t.Nanosecond()) << 32 / 1_000_000_000
	return uint32((secs<<32 | frac) >> 16)
}
