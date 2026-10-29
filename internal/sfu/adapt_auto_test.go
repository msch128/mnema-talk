package sfu

import (
	"testing"
	"time"
)

// ramp models the bandwidth estimate of a connection that only carries one
// layer: it follows 1.5 times what is delivered while the link keeps up, at
// the pace of a congestion controller (8 % a second upwards), and falls to
// 0.85 times what is delivered the moment the link is the limit.
type ramp struct {
	rates []int
	cap   int
	est   int
}

func (r *ramp) sample(layer int) (est, cur, next int) {
	cur = r.rates[layer]
	if layer > 0 {
		next = r.rates[layer-1]
	}
	delivered := min(cur, r.cap)
	target := delivered * 3 / 2
	if cur > r.cap {
		target = delivered * 85 / 100
	}
	if r.est < target {
		r.est = min(target, r.est*108/100)
	} else {
		r.est = target
	}
	return r.est, cur, next
}

// run feeds the adapter one clean report a second for secs seconds and
// returns the layers it moved to, with their times.
func runRamp(a *adapter, r *ramp, from time.Time, secs int) (end time.Time, moves []int) {
	end = from
	for range secs {
		end = end.Add(time.Second)
		a.setBandwidth(r.sample(a.layer))
		if layer, changed := a.observe(end, 0, 20*time.Millisecond); changed {
			moves = append(moves, layer)
		}
	}
	return end, moves
}

// A 4K share (layers of 12, 5.2 and 2.3 Mbit/s) on a link far faster than any
// of them: one burst of loss sends the viewer down, and it has to find its
// way back to the source. The estimate of a stream that is not limited by the
// link only ever reaches 1.5 times what is sent, so it is still catching up
// with the better layer when the first reports after a move up are judged.
func TestAutoReturnsToTheSourceOnAFastLinkAfterATrouble(t *testing.T) {
	r := &ramp{rates: []int{12_000_000, 5_200_000, 2_300_000}, cap: 1_000_000_000, est: bweInitialBitrate}
	a := newAdapter(epoch, 2)
	now, _ := runRamp(a, r, epoch, 10)
	for _, trouble := range []int{1, 2} {
		now = now.Add(adaptGrace + time.Second)
		if layer, changed := a.observe(now, 0.5, 0); !changed || layer != trouble {
			t.Fatalf("severe loss: layer %d changed %v, want %d", layer, changed, trouble)
		}
	}
	now, moves := runRamp(a, r, now, 600)
	if a.layer != 0 {
		t.Fatalf("on layer %d ten minutes after the trouble, moves %v", a.layer, moves)
	}
	if len(moves) != 2 {
		t.Fatalf("moves %v, want one step up per layer and no retreat", moves)
	}
}

// A link that carries the middle layer but not the source keeps the viewer
// there: it tries the source now and then, backs off, and does not flap.
func TestAutoDoesNotFlapAgainstALinkThatCarriesLessThanTheSource(t *testing.T) {
	r := &ramp{rates: []int{12_000_000, 5_200_000, 2_300_000}, cap: 8_000_000, est: bweInitialBitrate}
	a := newAdapter(epoch, 2)
	_, moves := runRamp(a, r, epoch, 1200)
	if a.layer != 1 {
		t.Fatalf("settled on layer %d of an 8 Mbit/s link, moves %v", a.layer, moves)
	}
	// One move down, then failed attempts with growing pauses: about seven in 20 minutes.
	if len(moves) > 16 {
		t.Fatalf("flapping: %d moves %v", len(moves), moves)
	}
}

func TestAutoStaysOnTheSourceOnAFastLink(t *testing.T) {
	r := &ramp{rates: []int{12_000_000, 5_200_000, 2_300_000}, cap: 1_000_000_000, est: bweInitialBitrate}
	a := newAdapter(epoch, 2)
	_, moves := runRamp(a, r, epoch, 600)
	if a.layer != 0 || len(moves) != 0 {
		t.Fatalf("layer %d moves %v", a.layer, moves)
	}
}

func TestAutoNeverGoesAboveWhatThePictureCanUse(t *testing.T) {
	r := &ramp{rates: []int{12_000_000, 5_200_000, 2_300_000}, cap: 1_000_000_000, est: bweInitialBitrate}
	a := newAdapter(epoch, 2)
	a.best = 2
	a.layer = a.top()
	_, moves := runRamp(a, r, epoch, 600)
	if a.layer != 2 || len(moves) != 0 {
		t.Fatalf("a small picture got layer %d, moves %v", a.layer, moves)
	}
}

func TestAutoClimbsToAnEnlargedPictureQuicklyAndStepsDownForASmallerOne(t *testing.T) {
	r := &ramp{rates: []int{12_000_000, 5_200_000, 2_300_000}, cap: 1_000_000_000, est: bweInitialBitrate}
	a := newAdapter(epoch, 2)
	a.best = 2
	a.layer = 2
	now, _ := runRamp(a, r, epoch, 60)

	// The tile is made fullscreen: the source is the best layer now.
	if a.setBest(now, 0) {
		t.Fatal("the layer changed before any report")
	}
	start := now
	var moves []int
	for a.layer != 0 && now.Sub(start) < 30*time.Second {
		var m []int
		now, m = runRamp(a, r, now, 1)
		moves = append(moves, m...)
	}
	if a.layer != 0 || now.Sub(start) > 15*time.Second {
		t.Fatalf("layer %d after %v, moves %v", a.layer, now.Sub(start), moves)
	}
	if len(moves) != 2 || moves[0] != 1 || moves[1] != 0 {
		t.Fatalf("moves %v, want one layer at a time", moves)
	}

	// Back to a small tile: down at once, and it stays there.
	if !a.setBest(now, 2) || a.layer != 2 {
		t.Fatalf("layer %d after the picture got small", a.layer)
	}
	_, moves = runRamp(a, r, now, 300)
	if a.layer != 2 || len(moves) != 0 {
		t.Fatalf("layer %d moves %v", a.layer, moves)
	}
}

func TestAutoGoesDownOnCongestionFast(t *testing.T) {
	r := &ramp{rates: []int{12_000_000, 5_200_000, 2_300_000}, cap: 1_000_000_000, est: bweInitialBitrate}
	a := newAdapter(epoch, 2)
	now, _ := runRamp(a, r, epoch, 30)
	r.cap = 3_000_000
	end, moves := runRamp(a, r, now, 60)
	if a.layer != 2 || len(moves) != 2 {
		t.Fatalf("layer %d moves %v on a 3 Mbit/s link", a.layer, moves)
	}
	if end.Sub(now) > 60*time.Second {
		t.Fatal("too slow")
	}
}

// The whole path through a viewer's track: the size of the picture limits
// automatic, an enlarged picture lets it climb, and the sender is asked for
// the layers it passes through.
func TestAutoViewerFollowsTheSizeOfItsPicture(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	rig.group.declared.Store(3)
	vt, _ := rig.viewer(-1)
	vt.setFit(2)
	if got := vt.wantA.Load(); got != 2 {
		t.Fatalf("a small picture wants layer %d", got)
	}
	rig.pkt(2, 100, 1000, vp8Payload(true, true, 10, true, 1, 'a'))
	if st := vt.state(); st.Layer != 2 || !st.Auto {
		t.Fatalf("starts on %+v", st)
	}
	// A long clean stretch does not take it above the picture.
	for range 120 {
		clock.advance(time.Second)
		vt.observeReport(0, 20*time.Millisecond)
	}
	if got := vt.wantA.Load(); got != 2 {
		t.Fatalf("a small picture climbed to layer %d", got)
	}

	// Fullscreen: layers 1 and 0 are asked of the sender, then received.
	vt.setFit(0)
	if got := vt.wantA.Load(); got != 2 {
		t.Fatalf("jumped to layer %d before a report", got)
	}
	clock.advance(time.Second)
	vt.observeReport(0, 20*time.Millisecond)
	if got := vt.wantA.Load(); got != 1 {
		t.Fatalf("wants layer %d on the first report of a large picture, want 1", got)
	}
	if !rig.asked(1) {
		t.Fatal("no keyframe asked of layer 1")
	}
	rig.pkt(1, 500, 5, vp8Payload(true, true, 50, true, 1, 'b'))
	for range int(adaptClimbFor/time.Second) + int(adaptGrace/time.Second) {
		clock.advance(time.Second)
		vt.observeReport(0, 20*time.Millisecond)
	}
	if got := vt.wantA.Load(); got != 0 {
		t.Fatalf("wants layer %d, want the source", got)
	}
	rig.pkt(0, 900, 9, vp8Payload(true, true, 90, true, 1, 'c'))
	if st := vt.state(); st.Layer != 0 || !st.Auto {
		t.Fatalf("at the top %+v", st)
	}

	// A smaller picture moves it down at once, trouble or not.
	vt.setFit(1)
	if got := vt.wantA.Load(); got != 1 {
		t.Fatalf("wants layer %d after the picture shrank", got)
	}
}

func TestAutoViewerWhoChoseALayerIgnoresTheSizeOfItsPicture(t *testing.T) {
	useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	rig.group.declared.Store(3)
	vt, _ := rig.viewer(0)
	vt.setFit(2)
	if got := vt.wantA.Load(); got != 0 {
		t.Fatalf("a chosen layer moved to %d", got)
	}
	// Going back to automatic starts at what the picture can use.
	vt.setPref(-1)
	if got := vt.wantA.Load(); got != 2 {
		t.Fatalf("automatic wants layer %d with a small picture", got)
	}
}

func TestAutoAfterAPickClimbsToWhatThePictureCanUseWithoutTheLongWait(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	rig.group.declared.Store(3)
	vt, _ := rig.viewer(2)
	vt.setFit(0)
	vt.setPref(-1)
	if got := vt.wantA.Load(); got != 2 {
		t.Fatalf("automatic wants layer %d, it carries on from the pick", got)
	}
	for range int(adaptGrace/time.Second) + int(adaptClimbFor/time.Second) {
		clock.advance(time.Second)
		vt.observeReport(0, 20*time.Millisecond)
	}
	if got := vt.wantA.Load(); got != 1 {
		t.Fatalf("wants layer %d after %v of quiet", got, adaptGrace+adaptClimbFor)
	}
}
