package sfu

import "time"

// bwSample is what is known about bandwidth at the time of a report, in bits
// per second. Zero means unknown.
type bwSample struct {
	// est is the estimate for the viewer's connection, cur the bitrate of the
	// layer it receives and next the one of the next better layer.
	est, cur, next int
}

// setBandwidth records the sample the next observe takes into account.
func (a *adapter) setBandwidth(est, cur, next int) {
	a.bw = bwSample{est: est, cur: cur, next: next}
}

// bandwidthTrouble counts the report in the streaks and reports whether the
// estimate has been below the current layer's bitrate for long enough.
func (a *adapter) bandwidthTrouble() bool {
	bw := a.bw
	a.bw = bwSample{}
	rising := a.lastEst > 0 && float64(bw.est) > float64(a.lastEst)*adaptRising
	a.lastEst = max(bw.est, 0)
	if bw.est <= 0 || bw.cur <= 0 {
		a.bwLow, a.bwHigh = 0, 0
		return false
	}
	if float64(bw.est) < float64(bw.cur)*adaptBWDown && !rising {
		a.bwLow++
	} else {
		a.bwLow = 0
	}
	if bw.next > 0 && float64(bw.est) > float64(bw.next)*adaptBWUp {
		a.bwHigh++
	} else {
		a.bwHigh = 0
	}
	return a.bwLow >= adaptBWSamples
}

// bandwidthAllowsUp reports whether a move up is not held back by a remembered
// bandwidth limit.
func (a *adapter) bandwidthAllowsUp(now time.Time) bool {
	return !now.Before(a.capUntil) || a.bwHigh >= adaptBWSamples
}
