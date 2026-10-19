package sfu

import (
	"time"

	"github.com/pion/rtp"
)

// keyStart reports whether p starts a keyframe of layer idx, the only place
// where a viewer may join that layer.
func (g *layerGroup) keyStart(idx int, p *rtp.Packet) bool {
	switch g.kind {
	case codecVP8:
		d, ok := parseVP8(p.Payload)
		return ok && d.key
	case codecH264:
		sps, idr := h264Start(p.Payload)
		if sps {
			g.sawSPS[idx].Store(true)
			return true
		}
		return idr && !g.sawSPS[idx].Load()
	}
	// A codec the SFU cannot read is forwarded as a single layer.
	return idx == 0
}

// requestKeyframe asks the publisher for a keyframe of layer idx.
func (g *layerGroup) requestKeyframe(idx int) {
	if idx < 0 || idx >= maxLayers {
		return
	}
	g.mu.Lock()
	info := g.infos[idx]
	g.mu.Unlock()
	if info != nil {
		info.requestKeyframe()
	}
}

// resolve turns a wished-for layer into one that exists: the lowest quality
// at or above it, else the best one below. -1 when no layer is attached.
func (g *layerGroup) resolve(want int) int {
	live := g.live.Load()
	want = g.clampWant(want)
	for i := want; i >= 0; i-- {
		if live&(1<<i) != 0 {
			return i
		}
	}
	for i := want + 1; i < maxLayers; i++ {
		if live&(1<<i) != 0 {
			return i
		}
	}
	return -1
}

// floor is the worst layer a viewer may be given: the last declared one, else
// the lowest-quality layer that exists.
func (g *layerGroup) floor() int {
	if n := int(g.declared.Load()); n > 0 {
		return min(n, maxLayers) - 1
	}
	live := g.live.Load()
	for i := maxLayers - 1; i > 0; i-- {
		if live&(1<<i) != 0 {
			return i
		}
	}
	return 0
}

// clampWant is a wished-for layer limited to the declared ones.
func (g *layerGroup) clampWant(want int) int {
	if n := int(g.declared.Load()); n > 0 && want >= n {
		return n - 1
	}
	if want >= maxLayers {
		return maxLayers - 1
	}
	return want
}

// setDeclared records how many layers the sender announced.
func (g *layerGroup) setDeclared(n int) {
	g.declared.Store(int32(n))
	g.retarget()
	g.refreshWanted()
}

// retarget moves every viewer to the layer it should have now.
func (g *layerGroup) retarget() {
	g.mu.Lock()
	vts := g.viewerListLocked()
	g.mu.Unlock()
	for _, vt := range vts {
		vt.retarget()
	}
}

// need is the layers some viewer receives or is about to.
func (g *layerGroup) need() uint8 {
	g.mu.Lock()
	vts := g.viewerListLocked()
	g.mu.Unlock()
	var mask uint8
	for _, vt := range vts {
		if want := g.clampWant(int(vt.wantA.Load())); want >= 0 {
			mask |= 1 << want
		}
		if cur := vt.curA.Load(); cur >= 0 {
			mask |= 1 << cur
		}
	}
	if len(vts) == 0 {
		// Nobody watches: the sender keeps its best layer, as before layers.
		mask = 1
	}
	return mask
}

// refreshWanted tells the publisher which layers to send when that changed.
// A layer stays wanted for layerHold after the last viewer left it.
func (g *layerGroup) refreshWanted() {
	need := g.need()
	now := layerNow()
	g.mu.Lock()
	var mask uint8
	var next time.Time
	for i := range g.hold {
		switch {
		case need&(1<<i) != 0:
			g.hold[i] = now.Add(layerHold)
			mask |= 1 << i
		case g.hold[i].After(now):
			mask |= 1 << i
			if next.IsZero() || g.hold[i].Before(next) {
				next = g.hold[i]
			}
		}
	}
	changed := !g.started || mask != g.announced
	g.started = true
	g.announced = mask
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	if !next.IsZero() && !g.superseded.Load() {
		g.timer = time.AfterFunc(next.Sub(now)+time.Millisecond, g.refreshWanted)
	}
	g.mu.Unlock()
	if changed && !g.superseded.Load() {
		g.notifyWanted(mask)
	}
}

func (g *layerGroup) notifyWanted(mask uint8) {
	if g.notifier == nil {
		return
	}
	fn := g.notifier.wantedFn()
	if fn == nil {
		return
	}
	layers := make([]int, 0, maxLayers)
	for i := 0; i < maxLayers; i++ {
		if mask&(1<<i) != 0 {
			layers = append(layers, i)
		}
	}
	g.wantedOut.send(layers, func(layers []int) { fn(g.roomID, g.publisher, layers) })
}

func (g *layerGroup) notifyQuality(vt *viewerTrack, st QualityState) {
	if g.notifier == nil {
		return
	}
	if fn := g.notifier.qualityFn(); fn != nil {
		vt.qualityOut.send(st, func(st QualityState) { fn(g.roomID, vt.viewer, g.publisher, st) })
	}
}
