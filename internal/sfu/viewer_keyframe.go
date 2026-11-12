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

// sentNumbers is what the stream of a layered viewer has carried: the newest
// sequence number and timestamp sent and, after a layer switch, the newest
// sequence number sent before it (base). A packet can arrive late, reordered
// on the way or repaired by a retransmission after newer ones, so the last
// packet sent is not always the newest. A switch continues after the newest:
// continuing after a late one would give the new layer's first packets (its
// keyframe) numbers the viewer already holds, which a receiver drops as
// duplicates (RFC 3550 A.1), and a timestamp older than a frame it has.
type sentNumbers struct {
	seq, base uint16
	ts        uint32
	any       bool
	// rebased: base applies. It retires once the stream has moved on far
	// enough that the 16-bit comparison with base would turn round.
	rebased bool
}

// seqNewer reports whether sequence number a comes after b (RFC 1982).
func seqNewer(a, b uint16) bool { return int16(a-b) > 0 }

// tsNewer reports whether RTP timestamp a comes after b.
func tsNewer(a, b uint32) bool { return int32(a-b) > 0 }

// note records a packet sent with sequence number seq and timestamp ts.
func (n *sentNumbers) note(seq uint16, ts uint32) {
	if !n.any || seqNewer(seq, n.seq) {
		n.seq = seq
	}
	if !n.any || tsNewer(ts, n.ts) {
		n.ts = ts
	}
	n.any = true
	if n.rebased && seq-n.base >= 1<<14 {
		n.rebased = false
	}
}

// rebase marks a layer switch: the new layer's packets start after seq, the
// newest number sent.
func (n *sentNumbers) rebase() {
	n.base, n.rebased = n.seq, true
}

// fresh reports whether a packet of the current layer that would go out as
// seq may be sent: one that the source sent before the keyframe the viewer
// was switched at would take a number the layer before already used.
func (n *sentNumbers) fresh(seq uint16) bool {
	return !n.rebased || seqNewer(seq, n.base)
}
