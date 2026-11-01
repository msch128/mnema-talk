package sfu

import "time"

// jumpToTop moves the viewer straight to the best layer its picture can use,
// without the gradual climb on clean reports. A camera viewer whose picture
// grew (a tile put on the stage) asked for it, and waiting for a clean stretch
// would leave it on the thumbnail. Trouble moves it down again as usual. It
// reports whether the layer changed.
func (a *adapter) jumpToTop(now time.Time) bool {
	to := a.top()
	if a.layer <= to {
		return false
	}
	a.layer = to
	a.climbing = false
	a.graceUntil = now.Add(adaptGrace)
	a.holdUntil = time.Time{}
	a.probeEnd = time.Time{}
	a.cleanSince = now
	a.bwLow, a.bwHigh = 0, 0
	a.resetPLIs()
	return true
}
