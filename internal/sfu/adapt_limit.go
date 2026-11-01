package sfu

import "time"

// setSqueeze holds the viewer n layers below the best its picture can use, for
// the voice upload limit (upload_limit.go). More squeeze moves it down at
// once; less lets it climb back the way an enlarged picture does. It reports
// whether the layer changed.
func (a *adapter) setSqueeze(now time.Time, n int) bool {
	n = max(n, 0)
	if n == a.squeeze {
		return false
	}
	old := a.top()
	a.squeeze = n
	return a.limitChanged(now, old)
}

// limitChanged applies a change of the best layer allowed from oldTop.
func (a *adapter) limitChanged(now time.Time, oldTop int) bool {
	top := a.top()
	if a.layer < top {
		a.layer = top
		a.graceUntil = now.Add(adaptGrace)
		a.climbing = false
		a.probeEnd = time.Time{}
		return true
	}
	if top < oldTop && a.layer <= oldTop {
		a.climbing = true
		a.holdUntil = time.Time{}
		a.cleanSince = now.Add(-adaptCleanFor)
	}
	return false
}
