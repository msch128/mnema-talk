package sfu

import (
	"time"

	"github.com/msch128/mnema-talk/internal/safego"
)

func (vt *viewerTrack) needsKeyframe(now time.Time) bool {
	switch {
	case vt.closed, vt.eff < 0:
		return false
	case vt.eff == vt.cur, now.Sub(vt.lastKeyAsk) < keyframeRetry:
		return false
	default:
		return true
	}
}

// keyframeDueLocked reports whether a keyframe of the target layer should be
// asked for now (and records the ask).
func (vt *viewerTrack) keyframeDueLocked(now time.Time) bool {
	if !vt.needsKeyframe(now) {
		return false
	}
	vt.lastKeyAsk = now
	return true
}

// retryKeyframe asks again for the keyframe a switch is waiting for. The ask in
// write only happens while packets of the target layer arrive; a layer the
// sender has not (re)started, or that sends nothing because its encoder is
// starved, would otherwise be asked once, before it could answer. The viewer's
// receiver reports come regardless of the media, so they drive the retry.
func (vt *viewerTrack) retryKeyframe(now time.Time) {
	ask, eff := false, 0
	safego.Locked(&vt.mu, func() { ask, eff = vt.keyframeDueLocked(now), vt.eff })
	if ask {
		vt.group.requestKeyframe(eff)
	}
}

// viewerKeyframe passes on the viewer's own keyframe request; for an automatic
// viewer, repeated requests are a sign that it cannot decode what it gets.
func (vt *viewerTrack) viewerKeyframe() {
	now := layerNow()
	flood := false
	safego.Locked(&vt.mu, func() {
		if vt.ad != nil {
			flood = !vt.ad.keyframeRequested(now)
		}
	})
	if !flood {
		vt.requestKeyframe()
	}
}

// requestKeyframe passes the viewer's own keyframe request to the layer it
// receives (or is about to).
func (vt *viewerTrack) requestKeyframe() {
	vt.mu.Lock()
	idx := vt.cur
	if idx < 0 {
		idx = vt.eff
	}
	vt.mu.Unlock()
	vt.group.requestKeyframe(idx)
}
