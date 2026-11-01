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

// resolveFlowing is resolve for a viewer that should see something now: a
// layer its sender switched off (nobody needed it for a while) is still
// attached but silent, and a viewer resolved to it would see nothing until
// the sender, told it is needed, starts it again. Until that layer delivers,
// the nearest one that does carries the viewer, and the viewer moves over as
// soon as the wished-for layer's packets are back (see write). When nothing
// flows at all the wished-for layer is kept. A screen share's viewer chose
// its layer (or the adapter did), and waits for it as before; a camera's
// viewer only has a picture size, so it should never be left looking at
// nothing.
func (g *layerGroup) resolveFlowing(want int, now time.Time) int {
	layer := g.resolve(want)
	if g.source != SourceCamera || layer < 0 || g.recent(layer, now) {
		return layer
	}
	if alt := g.flowing(layer, now); alt >= 0 {
		return alt
	}
	return layer
}

// recent reports whether layer i delivered a packet within the last second.
func (g *layerGroup) recent(i int, now time.Time) bool {
	return g.live.Load()&(1<<i) != 0 && now.UnixNano()-g.lastPkt[i].Load() < int64(time.Second)
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

// evalLayerHold updates or reads the expiration time for layer i.
func (g *layerGroup) evalLayerHold(i int, now time.Time, need uint8) (uint8, time.Time) {
	if need&(1<<i) != 0 {
		g.hold[i] = now.Add(layerHold)
		return 1 << i, time.Time{}
	}
	if g.hold[i].After(now) {
		return 1 << i, g.hold[i]
	}
	return 0, time.Time{}
}

// computeHoldMaskLocked determines which layers remain wanted and when the earliest expires.
func (g *layerGroup) computeHoldMaskLocked(now time.Time, need uint8) (uint8, time.Time) {
	var mask uint8
	var next time.Time
	for i := range g.hold {
		m, exp := g.evalLayerHold(i, now, need)
		mask |= m
		if !exp.IsZero() && (next.IsZero() || exp.Before(next)) {
			next = exp
		}
	}
	return mask, next
}

// scheduleHoldTimerLocked stops any existing timer and schedules a refresh for the next expiration.
func (g *layerGroup) scheduleHoldTimerLocked(next time.Time, now time.Time) {
	g.stopTimerLocked()
	if !next.IsZero() && !g.superseded.Load() {
		g.timer = time.AfterFunc(next.Sub(now)+time.Millisecond, g.refreshWanted)
	}
}

// refreshWanted tells the publisher which layers to send when that changed.
// A layer stays wanted for layerHold after the last viewer left it.
func (g *layerGroup) refreshWanted() {
	need := g.need()
	now := layerNow()
	g.mu.Lock()
	mask, next := g.computeHoldMaskLocked(now, need)
	changed := !g.started || mask != g.announced
	g.started = true
	g.announced = mask
	g.scheduleHoldTimerLocked(next, now)
	g.mu.Unlock()
	if changed && !g.superseded.Load() {
		g.notifyWanted(mask)
	}
}

func (g *layerGroup) notifyWanted(mask uint8) {
	if g.notifier == nil {
		return
	}
	fn := g.notifier.wantedFnFor(g.source)
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
	// Viewers pick the layer of a screen share; a camera has no picker.
	if g.notifier == nil || g.source != SourceScreen {
		return
	}
	if fn := g.notifier.qualityFn(); fn != nil {
		vt.qualityOut.send(st, func(st QualityState) { fn(g.roomID, vt.viewer, g.publisher, st) })
	}
}
