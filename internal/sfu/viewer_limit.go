package sfu

import "time"

// uploadView is what the upload governor needs to know of one viewer on
// automatic: what it costs now, what one more step down saves and what one
// step back up costs, all in bits per second.
type uploadView struct {
	vt       *viewerTrack
	rate     float64
	down, up float64
	squeeze  int
}

// layerBits is the bitrate of layer i: the measured one, else the last that
// was measured (a layer the sender switched off keeps its size).
func (g *layerGroup) layerBits(i int) float64 {
	if i < 0 || i >= maxLayers {
		return 0
	}
	if r := g.rate[i].Load(); r > 0 {
		return float64(r)
	}
	return float64(g.last[i].Load())
}

// uploadView describes the viewer for the governor; false for a viewer the
// governor cannot move: one that chose its layer, or that has no picture yet
// or no known bitrate.
func (vt *viewerTrack) uploadView(now time.Time) (uploadView, bool) {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	if vt.ad == nil || vt.closed || vt.cur < 0 {
		return uploadView{}, false
	}
	g, ad := vt.group, vt.ad
	g.sampleRates(now)
	rate := g.layerBits(vt.cur)
	if rate <= 0 {
		return uploadView{}, false
	}
	v := uploadView{vt: vt, rate: rate, squeeze: ad.squeeze}
	if next := min(ad.best+ad.squeeze+1, ad.floor); next > ad.top() && next > vt.cur {
		to := g.layerBits(next)
		if to <= 0 {
			to = rate / 2
		}
		v.down = max(rate-to, 0)
	}
	if ad.squeeze > 0 {
		if relaxed := min(ad.best+ad.squeeze-1, ad.floor); vt.cur > relaxed {
			v.up = max(g.layerBits(max(relaxed, vt.cur-1))-rate, 0)
		}
	}
	return v, true
}

// setSqueeze holds the viewer n layers below the best its picture can use
// (see adapter.setSqueeze).
func (vt *viewerTrack) setSqueeze(n int) {
	vt.mu.Lock()
	changed := false
	if vt.ad != nil && !vt.closed {
		vt.ad.setFloor(vt.group.floor())
		changed = vt.ad.setSqueeze(layerNow(), n)
		vt.want = vt.ad.layer
		vt.wantA.Store(int32(vt.want))
	}
	vt.mu.Unlock()
	if changed {
		vt.retarget()
	}
	vt.group.refreshWanted()
}
