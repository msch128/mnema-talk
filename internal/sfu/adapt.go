package sfu

import "time"

// A viewer who left the choice to the server ("auto") gets the best layer its
// connection carries. The SFU judges that from what the viewer reports about
// the stream it receives: the loss fraction and the round trip time in its
// RTCP receiver reports (every second or so), and how often it asks for a new
// keyframe. Nothing is estimated from the media, which the SFU cannot read.
//
// It starts on the best layer the viewer's picture can use (see setBest: the
// viewer says how large it shows the share) and never goes above it. Trouble moves it down one layer at a time; it
// moves up again, one layer at a time, only after a long stretch without
// trouble, and a move up that goes wrong makes the next one wait twice as
// long, so a link that can carry the lower layer but not the higher one does
// not flap. Nothing here applies to a viewer who chose a layer.
//
// A link that is too slow for a layer but loses nothing (the sender's queue
// absorbs it) shows in none of those reports for a long time, so the viewer's
// bandwidth estimate (bwe.go, from the transport-wide feedback the browser
// sends) is compared with the bitrate the layers have at the SFU: an estimate
// that stays below the current layer's bitrate times adaptBWDown moves the
// viewer down, and a viewer that was moved down this way goes up again only
// when the estimate is above the better layer's bitrate times adaptBWUp, or
// after adaptCapHold, when it may try once more as usual.
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
	adaptBackoffMax = 5 * time.Minute
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
	// adaptBWDown: an estimate below the current layer's bitrate times this
	// counts as trouble once it lasts adaptBWSamples reports. A delivery that
	// keeps up makes the estimate about 1.5 times the layer's bitrate, so
	// this only fires when the link carries clearly less than the layer.
	adaptBWDown = 1.2
	// adaptBWUp: the estimate must stay above the better layer's bitrate
	// times this for adaptBWSamples reports before a viewer that hit the
	// bandwidth limit goes up.
	adaptBWUp      = 1.5
	adaptBWSamples = 4
	// adaptCapHold is how long a bandwidth limit that moved a viewer down is
	// remembered. The estimate cannot rise above 1.5 times what is being sent,
	// so it never proves that a better layer fits; after this time the
	// viewer tries it once more (a failed try backs off as any other).
	adaptCapHold = time.Minute
	// adaptRising: an estimate that grew by more than this since the last
	// report is still ramping up, which is not a limit of the link. The
	// estimate of an app-limited stream stays close to 1.5 times what is
	// sent, so after a move up it takes seconds to catch up with the better
	// layer; judging it before would send every attempt back down.
	adaptRising = 1.02
	// adaptClimbFor is how long a viewer that may use a better layer than it
	// has (it enlarged the picture, or made it fullscreen) waits between
	// steps while it has had no trouble.
	adaptClimbFor = 4 * time.Second
)

// adapter decides the layer of one automatic viewer from its reports. It has no
// locking and no clock: the caller serializes it and passes the time.
type adapter struct {
	// layer is the layer chosen now and floor the worst one allowed (the
	// lowest-quality layer the share has).
	layer, floor int
	// best is the best layer the viewer's picture can use (0 is the best the
	// share has): a small tile does not need the source, so the layer never
	// goes above it. climbing is set while the viewer is below a best that
	// has just been raised and has had no trouble since.
	best     int
	climbing bool

	graceUntil time.Time
	// holdUntil is when a move up may be tried at the earliest.
	holdUntil time.Time
	probeEnd  time.Time
	backoff   time.Duration
	// cleanSince is when the last trouble ended.
	cleanSince time.Time
	highStreak int

	// bw is the latest bandwidth sample, consumed by observe.
	bw bwSample
	// bwLow and bwHigh count consecutive reports with the estimate below the
	// layer's bitrate times adaptBWDown / above the better layer's times adaptBWUp.
	bwLow, bwHigh int
	// lastEst is the estimate of the previous report.
	lastEst int
	// capUntil: until then a bandwidth limit that moved the viewer down is remembered.
	capUntil time.Time
	// narrowed is set when the last move down was caused by the estimate.
	narrowed bool

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
	a.layer = max(a.layer, a.top())
}

// top is the best layer allowed: the viewer's best, but never worse than the floor.
func (a *adapter) top() int {
	return min(a.best, a.floor)
}

// setBest limits the layer to what the viewer's picture can use. A viewer
// that sat on the old limit climbs on from there without waiting for a clean
// stretch (it is not held back by trouble); one that is pushed below the new
// limit moves down at once. It reports whether the layer changed.
func (a *adapter) setBest(now time.Time, best int) bool {
	best = max(best, 0)
	if best == a.best {
		return false
	}
	raised := best < a.best
	atLimit := a.layer <= a.top()
	a.best = best
	if a.layer < a.top() {
		a.layer = a.top()
		a.graceUntil = now.Add(adaptGrace)
		a.climbing = false
		a.probeEnd = time.Time{}
		return true
	}
	if raised && atLimit {
		a.climbing = true
		a.holdUntil = time.Time{}
		a.cleanSince = now.Add(-adaptCleanFor)
	}
	return false
}

// keyframeRequested records a keyframe request of the viewer in constant time.
// Requests closer together than adaptPLIGap are not counted one by one: they
// are a flood, which is trouble by itself. Requests during the grace period
// after a layer change are not recorded: the viewer is waiting for the new
// layer's first keyframe, which is no sign of trouble.
func (a *adapter) keyframeRequested(now time.Time) (counted bool) {
	if now.Before(a.graceUntil) {
		return true
	}
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
	to = max(min(to, a.floor), a.top())
	if to == a.layer {
		return false
	}
	a.layer = to
	a.climbing = false
	a.graceUntil = now.Add(adaptGrace)
	a.holdUntil = now.Add(a.backoff)
	a.cleanSince = now
	a.probeEnd = time.Time{}
	a.highStreak = 0
	a.bwLow, a.bwHigh = 0, 0
	a.resetPLIs()
	return true
}

func (a *adapter) resetPLIs() {
	a.plis = [adaptPLIs]time.Time{}
	a.nPLI = 0
	a.flooded = false
}

type adaptSignals struct {
	loss   float64
	rise   bool
	narrow bool
	recent int
}

func (a *adapter) updateLossStreak(loss float64) (severe bool) {
	if loss >= adaptLossHigh {
		a.highStreak++
	} else {
		a.highStreak = 0
	}
	return loss >= adaptLossSevere
}

func (a *adapter) hasTrouble(s adaptSignals) bool {
	severe := a.updateLossStreak(s.loss)
	switch {
	case severe, a.highStreak >= 2:
		return true
	case s.rise, a.flooded:
		return true
	case s.recent >= adaptPLIs, s.narrow:
		return true
	default:
		return false
	}
}

// observe takes one report: the loss fraction (0..1) and the round trip time
// (zero when the report did not carry it). It returns the layer to receive
// and whether that changed.
func (a *adapter) observe(now time.Time, loss float64, rtt time.Duration) (int, bool) {
	rise := a.recordRTT(rtt)
	if now.Before(a.graceUntil) {
		a.bw = bwSample{}
		a.narrowed = false
		a.flooded = false
		return a.layer, false
	}
	narrow := a.bandwidthTrouble()
	a.narrowed = false
	sig := adaptSignals{
		loss:   loss,
		rise:   rise,
		narrow: narrow,
		recent: a.recentPLIs(now),
	}
	trouble := a.hasTrouble(sig)
	a.flooded = false
	if trouble {
		return a.layer, a.stepDown(now, narrow)
	}
	if loss >= adaptLossClear {
		// Not trouble, but not clean either: the wait for the next try restarts.
		a.cleanSince = now
		return a.layer, false
	}
	return a.layer, a.tryStepUp(now)
}

// stepDown moves the viewer to the next lower layer after trouble, unless it
// is on the lowest one allowed. It reports whether the layer changed.
func (a *adapter) stepDown(now time.Time, narrow bool) bool {
	a.cleanSince = now
	if a.layer >= a.floor {
		// Nowhere lower to go.
		a.highStreak = 0
		return false
	}
	failedProbe := now.Before(a.probeEnd)
	a.climbing = false
	a.layer++
	a.graceUntil = now.Add(adaptGrace)
	a.highStreak = 0
	a.bwLow, a.bwHigh = 0, 0
	a.resetPLIs()
	if failedProbe {
		a.backoff = min(a.backoff*2, adaptBackoffMax)
	}
	if narrow {
		// A limit that was met again and again is remembered for longer.
		a.capUntil = now.Add(max(adaptCapHold, a.backoff))
		a.narrowed = true
	}
	a.holdUntil = now.Add(a.backoff)
	a.probeEnd = time.Time{}
	return true
}

func (a *adapter) canStepUp(now time.Time) bool {
	if a.layer <= a.top() || now.Before(a.holdUntil) {
		return false
	}
	if now.Sub(a.cleanSince) < a.cleanWait() {
		return false
	}
	return a.bandwidthAllowsUp(now)
}

// cleanWait is how long a viewer has to be clean before it tries a better layer.
func (a *adapter) cleanWait() time.Duration {
	if a.climbing {
		return adaptClimbFor
	}
	return adaptCleanFor
}

// tryStepUp moves the viewer to the next better layer once it has been clean
// long enough and nothing holds it back. It reports whether the layer changed.
func (a *adapter) tryStepUp(now time.Time) bool {
	if !a.probeEnd.IsZero() && !now.Before(a.probeEnd) {
		// The last move up held: the next attempt may come as soon as usual.
		a.backoff = adaptCleanFor
		a.probeEnd = time.Time{}
	}
	if a.canStepUp(now) {
		a.layer--
		a.bwLow, a.bwHigh = 0, 0
		a.graceUntil = now.Add(adaptGrace)
		a.probeEnd = now.Add(adaptProbe)
		a.holdUntil = now.Add(a.backoff)
		if a.climbing {
			a.holdUntil = now.Add(adaptClimbFor)
			a.climbing = a.layer > a.top()
		}
		a.cleanSince = now
		return true
	}
	return false
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
