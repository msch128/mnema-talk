package sfu

import (
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/safego"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func (g *layerGroup) viewerListLocked() []*viewerTrack {
	out := make([]*viewerTrack, 0, len(g.viewers))
	for _, vt := range g.viewers {
		out = append(out, vt)
	}
	return out
}

func (g *layerGroup) publishViewersLocked() {
	list := g.viewerListLocked()
	g.snapshot.Store(&list)
}

// newViewer makes the outgoing track one viewer gets for the group. pref is
// the viewer's choice, -1 for automatic, and fit the best layer its picture
// can use (see setFit). It receives nothing until addViewer.
func (g *layerGroup) newViewer(viewer uuid.UUID, pref, fit int, trackID, streamID string) (*viewerTrack, error) {
	local, err := webrtc.NewTrackLocalStaticRTP(g.codec, trackID, streamID)
	if err != nil {
		return nil, err
	}
	vt := &viewerTrack{TrackLocalStaticRTP: local, group: g, since: layerNow(), viewer: viewer, pref: pref, fit: fit, cur: -1, eff: -1, sink: local.WriteRTP}
	if pref < 0 {
		vt.ad = newAdapter(layerNow(), g.floor())
		vt.ad.best = fit
		vt.ad.layer = vt.ad.top()
	}
	vt.want = vt.chosenLayer()
	vt.curA.Store(-1)
	vt.wantA.Store(int32(vt.want))
	return vt, nil
}

// addViewer starts delivering to vt and asks for the keyframe it waits for.
// It refuses, and closes, a track that is closed already or that belongs to an
// older peer of the user than the one registered: a late round of a replaced
// connection must not take the live viewer's place. It reports whether vt
// was registered.
func (g *layerGroup) addViewer(vt *viewerTrack) bool {
	vt.mu.Lock()
	dead := vt.closed
	vt.mu.Unlock()
	g.mu.Lock()
	old := g.viewers[vt.viewer]
	if dead || (old != nil && old != vt && old.gen > vt.gen) {
		g.mu.Unlock()
		vt.close()
		return false
	}
	g.viewers[vt.viewer] = vt
	g.publishViewersLocked()
	g.mu.Unlock()
	if old != nil && old != vt {
		old.close()
	}
	vt.retarget()
	g.refreshWanted()
	return true
}

// removeViewer stops delivering to vt.
func (g *layerGroup) removeViewer(vt *viewerTrack) {
	g.mu.Lock()
	if g.viewers[vt.viewer] == vt {
		delete(g.viewers, vt.viewer)
		g.publishViewersLocked()
	}
	g.mu.Unlock()
	vt.close()
	g.refreshWanted()
}

// viewerTracks is a snapshot of the group's viewers' outgoing tracks.
func (g *layerGroup) viewerTracks() []*viewerTrack {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.viewerListLocked()
}

// viewerFor is viewer's outgoing track of the group, nil if it has none.
func (g *layerGroup) viewerFor(viewer uuid.UUID) *viewerTrack {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.viewers[viewer]
}

// write hands one packet of layer idx to every viewer.
func (g *layerGroup) write(idx int, p *rtp.Packet) {
	now := layerNow().UnixNano()
	if was := g.lastPkt[idx].Swap(now); now-was >= int64(time.Second) && g.source == SourceCamera {
		// The layer was silent (its sender switched it off, or it just began: its
		// first packet comes after the layer was attached) and delivers now:
		// viewers waiting for it on another layer move over.
		g.retarget()
	}
	g.sent[idx].Add(uint64(rtpHeaderSize + len(p.Payload)))
	list := g.snapshot.Load()
	if list == nil {
		return
	}
	for _, vt := range *list {
		g.writeTo(vt, idx, p)
	}
}

// writeTo hands the packet to one viewer. Its track, and behind it that
// viewer's connection (NACK history, pacer, bandwidth estimate), run on the
// publisher's forwarding goroutine: a panic there is the viewer's, so the
// viewer goes and the publisher keeps sending to everyone else.
func (g *layerGroup) writeTo(vt *viewerTrack, idx int, p *rtp.Packet) {
	defer g.viewerWriteDone(vt)
	vt.write(idx, p)
}

// viewerWriteDone is deferred by writeTo. It costs a few nanoseconds per
// packet and viewer and allocates nothing unless a write panicked.
func (g *layerGroup) viewerWriteDone(vt *viewerTrack) {
	rec := recover()
	if rec == nil {
		return
	}
	safego.Recovered("sfu_viewer_write", rec, func() {
		// Out of the group first: the next packet must not reach it again.
		g.removeViewer(vt)
		if g.dropViewer != nil {
			g.dropViewer(vt.viewer)
		}
	})
}

// rateEvery is the shortest time between two measurements of the layers'
// bitrates, whatever the number of viewers asking.
const rateEvery = 900 * time.Millisecond

// rtpHeaderSize is the size counted for an RTP header (no extensions).
const rtpHeaderSize = 12

// sampleRates measures the layers' bitrates from the bytes received since the
// last measurement, smoothing over about three of them. It is cheap and
// lock free: of many viewers asking at once, one measures.
func (g *layerGroup) sampleRates(now time.Time) {
	if !g.rateMu.TryLock() {
		return
	}
	defer g.rateMu.Unlock()
	at, t := g.rateAt, now.UnixNano()
	if at != 0 && t-at < int64(rateEvery) {
		return
	}
	g.rateAt = t
	secs := float64(t-at) / float64(time.Second)
	for i := range g.sent {
		total := g.sent[i].Load()
		delta := total - g.seen[i]
		g.seen[i] = total
		if at == 0 {
			continue
		}
		if delta == 0 {
			g.rate[i].Store(0)
			continue
		}
		bps := int64(float64(delta) * 8 / secs)
		if old := g.rate[i].Load(); old > 0 {
			bps = (2*old + bps) / 3
		}
		bps = max(bps, 1)
		g.rate[i].Store(bps)
		g.last[i].Store(bps)
	}
}

// layerWriter feeds one layer's packets into its group. It is the "local
// track" of that layer's forwarding loop.
type layerWriter struct {
	group *layerGroup
	layer int
}

func (w layerWriter) WriteRTP(p *rtp.Packet) error {
	w.group.write(w.layer, p)
	return nil
}

// syncChoice brings a freshly registered track to the viewer's current choice,
// which may have changed while the track was made. It reports whether it had
// to change anything.
func (vt *viewerTrack) syncChoice(pref, fit int) bool {
	vt.mu.Lock()
	prefDiffers, fitDiffers := vt.pref != pref, vt.fit != max(fit, 0)
	vt.mu.Unlock()
	if prefDiffers {
		vt.setPref(pref)
	}
	if fitDiffers {
		vt.setFit(fit)
	}
	return prefDiffers || fitDiffers
}
