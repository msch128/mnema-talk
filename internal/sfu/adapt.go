package sfu

import (
	"time"

	"github.com/pion/rtcp"
)

// A viewer who left the choice to the server ("auto") gets the best layer its
// connection carries. The SFU judges that from what the viewer reports about
// the stream it receives: the loss fraction and the round trip time in its
// RTCP receiver reports (every second or so), and how often it asks for a new
// keyframe. Nothing is estimated from the media, which the SFU cannot read.
//
// It starts on the best layer. Trouble moves it down one layer at a time; it
// moves up again, one layer at a time, only after a long stretch without
// trouble, and a move up that goes wrong makes the next one wait twice as
// long, so a link that can carry the lower layer but not the higher one does
// not flap. Nothing here applies to a viewer who chose a layer.
const (
	// adaptGrace is how long after a layer change the reports are ignored: a
	// keyframe is several times the size of an ordinary frame and loses packets
	// on its own.
	adaptGrace = 3 * time.Second
	// adaptLossHigh is the loss fraction that counts as trouble when it lasts
	// two reports; adaptLossSevere is trouble at once.
	adaptLossHigh   = 0.05
	adaptLossSevere = 0.15
	// adaptLossClear is the loss fraction below which a report is clean.
	adaptLossClear = 0.01
	// adaptRTTRise is how far the round trip time may rise above its lowest
	// recent value before the queues on the path count as filling up.
	adaptRTTRise = 250 * time.Millisecond
	// adaptRTTSamples is how many reports the lowest round trip time is taken from.
	adaptRTTSamples = 60
	// adaptMinRTTSamples is how many reports the round trip time needs before it is judged.
	adaptMinRTTSamples = 5
	// adaptCleanFor is how long a viewer has to be clean before it tries the
	// next better layer.
	adaptCleanFor = 12 * time.Second
	// adaptProbe is how long a move up is watched; trouble within it is a
	// failed attempt.
	adaptProbe = 8 * time.Second
	// adaptBackoffMax caps the wait between attempts after failures.
	adaptBackoffMax = 2 * time.Minute
	// adaptPLIs keyframe requests within adaptPLIWindow count as trouble
	// (the viewer cannot decode what it gets).
	adaptPLIs      = 3
	adaptPLIWindow = 5 * time.Second
	// adaptPLIGap is the closest two keyframe requests are counted apart; a
	// viewer asking faster is flooding, which counts as one trouble signal.
	adaptPLIGap = 100 * time.Millisecond
	// adaptStall is how long nothing may reach a viewer, while another layer
	// delivers, before the viewer is moved to that layer.
	adaptStall = 2 * time.Second
)

// adapter decides the layer of one automatic viewer from its reports. It has no
// locking and no clock: the caller serializes it and passes the time.
type adapter struct {
	// layer is the layer chosen now and floor the worst one allowed (the
	// lowest-quality layer the share has).
	layer, floor int

	graceUntil time.Time
	// holdUntil is when a move up may be tried at the earliest.
	holdUntil time.Time
	probeEnd  time.Time
	backoff   time.Duration
	// cleanSince is when the last trouble ended.
	cleanSince time.Time
	highStreak int

	rtts [adaptRTTSamples]time.Duration
	nRTT int
	// plis is a ring of the last adaptPLIs counted keyframe requests, so
	// recording one is constant work however fast they arrive.
	plis    [adaptPLIs]time.Time
	nPLI    int
	flooded bool
}

func newAdapter(now time.Time, floor int) *adapter {
	return &adapter{floor: floor, cleanSince: now, graceUntil: now.Add(adaptGrace), backoff: adaptCleanFor}
}

// setFloor changes the worst layer allowed; the layer follows if it is below it.
func (a *adapter) setFloor(floor int) {
	a.floor = floor
	if a.layer > floor {
		a.layer = floor
	}
}

// keyframeRequested records a keyframe request of the viewer in constant time.
// Requests closer together than adaptPLIGap are not counted one by one: they
// are a flood, which is trouble by itself.
func (a *adapter) keyframeRequested(now time.Time) (counted bool) {
	if a.nPLI > 0 {
		last := a.plis[(a.nPLI-1)%adaptPLIs]
		if now.Sub(last) < adaptPLIGap {
			a.flooded = true
			return false
		}
	}
	a.plis[a.nPLI%adaptPLIs] = now
	a.nPLI++
	return true
}

// recentPLIs is how many counted keyframe requests are within the window.
func (a *adapter) recentPLIs(now time.Time) int {
	cut := now.Add(-adaptPLIWindow)
	recent := 0
	for _, t := range a.plis {
		if t.After(cut) {
			recent++
		}
	}
	return recent
}

// stalledTo moves the viewer to layer to after nothing reached it for
// adaptStall although that layer delivers. It reports whether the layer changed.
func (a *adapter) stalledTo(now time.Time, to int) bool {
	to = min(to, a.floor)
	if to == a.layer {
		return false
	}
	a.layer = to
	a.graceUntil = now.Add(adaptGrace)
	a.holdUntil = now.Add(a.backoff)
	a.cleanSince = now
	a.probeEnd = time.Time{}
	a.highStreak = 0
	a.resetPLIs()
	return true
}

func (a *adapter) resetPLIs() {
	a.plis = [adaptPLIs]time.Time{}
	a.nPLI = 0
	a.flooded = false
}

// observe takes one report: the loss fraction (0..1) and the round trip time
// (zero when the report did not carry it). It returns the layer to receive
// and whether that changed.
func (a *adapter) observe(now time.Time, loss float64, rtt time.Duration) (int, bool) {
	rise := a.recordRTT(rtt)
	recent := a.recentPLIs(now)
	flooded := a.flooded
	a.flooded = false
	if now.Before(a.graceUntil) {
		return a.layer, false
	}
	severe := loss >= adaptLossSevere
	if loss >= adaptLossHigh {
		a.highStreak++
	} else {
		a.highStreak = 0
	}
	trouble := severe || a.highStreak >= 2 || rise || flooded || recent >= adaptPLIs
	if trouble {
		a.cleanSince = now
		if a.layer >= a.floor {
			// Nowhere lower to go.
			a.highStreak = 0
			return a.layer, false
		}
		failedProbe := now.Before(a.probeEnd)
		a.layer++
		a.graceUntil = now.Add(adaptGrace)
		a.highStreak = 0
		a.resetPLIs()
		if failedProbe {
			a.backoff = min(a.backoff*2, adaptBackoffMax)
		}
		a.holdUntil = now.Add(a.backoff)
		a.probeEnd = time.Time{}
		return a.layer, true
	}
	if loss >= adaptLossClear {
		// Not trouble, but not clean either: the wait for the next try restarts.
		a.cleanSince = now
		return a.layer, false
	}
	if !a.probeEnd.IsZero() && !now.Before(a.probeEnd) {
		// The last move up held: the next attempt may come as soon as usual.
		a.backoff = adaptCleanFor
		a.probeEnd = time.Time{}
	}
	if a.layer > 0 && !now.Before(a.holdUntil) && now.Sub(a.cleanSince) >= adaptCleanFor {
		a.layer--
		a.graceUntil = now.Add(adaptGrace)
		a.probeEnd = now.Add(adaptProbe)
		a.holdUntil = now.Add(a.backoff)
		a.cleanSince = now
		return a.layer, true
	}
	return a.layer, false
}

// recordRTT adds a sample and reports whether the round trip time rose far
// above its lowest recent value.
func (a *adapter) recordRTT(rtt time.Duration) bool {
	if rtt <= 0 {
		return false
	}
	a.rtts[a.nRTT%adaptRTTSamples] = rtt
	a.nRTT++
	if a.nRTT < adaptMinRTTSamples {
		return false
	}
	count := min(a.nRTT, adaptRTTSamples)
	lowest := a.rtts[0]
	for _, v := range a.rtts[:count] {
		lowest = min(lowest, v)
	}
	return rtt-lowest > adaptRTTRise
}

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
