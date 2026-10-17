package sfu

import (
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

// feedBW gives the adapter one clean report a second for n seconds, each with
// the same bandwidth sample, and returns the time reached and the layers it
// moved to.
func feedBW(a *adapter, from time.Time, n int, est, cur, next int) (end time.Time, changes []int) {
	end = from
	for range n {
		end = end.Add(time.Second)
		a.setBandwidth(est, cur, next)
		if layer, changed := a.observe(end, 0, 20*time.Millisecond); changed {
			changes = append(changes, layer)
		}
	}
	return end, changes
}

func TestAdapterMovesDownWhenTheEstimateStaysBelowTheLayer(t *testing.T) {
	a := newAdapter(epoch, 2)
	// The first seconds after the start are not judged.
	now, ch := feedBW(a, epoch, 2, 500_000, 2_000_000, 0)
	if len(ch) != 0 {
		t.Fatalf("moved during the grace period: %v", ch)
	}
	// Below the layer's bitrate times 1.2 for adaptBWSamples reports.
	now, ch = feedBW(a, now, adaptBWSamples-1, 2_300_000, 2_000_000, 0)
	if len(ch) != 0 || a.layer != 0 {
		t.Fatalf("moved after %d reports: %v", adaptBWSamples-1, ch)
	}
	now, ch = feedBW(a, now, 1, 2_300_000, 2_000_000, 0)
	if len(ch) != 1 || ch[0] != 1 || !a.narrowed {
		t.Fatalf("changes %v narrowed %v", ch, a.narrowed)
	}
	// An ordinary move afterwards is not credited to the estimate.
	if _, changed := a.observe(now.Add(10*time.Second), 0.5, 0); !changed || a.narrowed {
		t.Fatalf("narrowed %v after a move for loss", a.narrowed)
	}
}

func TestAdapterStaysWhenTheEstimateKeepsUpOrIsUnknown(t *testing.T) {
	a := newAdapter(epoch, 2)
	now, _ := feedBW(a, epoch, 3, 0, 0, 0)
	// An estimate of 1.5 times the layer is a link that keeps up.
	now, ch := feedBW(a, now, 20, 3_000_000, 2_000_000, 0)
	if len(ch) != 0 {
		t.Fatalf("moved with a healthy estimate: %v", ch)
	}
	// Reports without an estimate, or without a known layer bitrate, end a streak.
	for _, sample := range [][3]int{{0, 2_000_000, 0}, {1_000_000, 0, 0}} {
		now, _ = feedBW(a, now, adaptBWSamples-1, 1_000_000, 2_000_000, 0)
		now, _ = feedBW(a, now, 1, sample[0], sample[1], sample[2])
		now, ch = feedBW(a, now, adaptBWSamples-1, 1_000_000, 2_000_000, 0)
		if len(ch) != 0 || a.layer != 0 {
			t.Fatalf("sample %v did not end the streak: %v", sample, ch)
		}
		now, _ = feedBW(a, now, 1, 3_000_000, 2_000_000, 0)
	}
	// A sample given during the grace period is dropped.
	b := newAdapter(epoch, 2)
	b.setBandwidth(1, 2_000_000, 0)
	b.observe(epoch.Add(time.Second), 0, 0)
	if b.bwLow != 0 || b.bw != (bwSample{}) {
		t.Fatalf("a sample from the grace period was counted: %+v", b)
	}
	// Nowhere lower to go: no move, no memory of a limit that did not move anyone.
	c := newAdapter(epoch, 0)
	if _, ch := feedBW(c, epoch, 10, 100_000, 2_000_000, 0); len(ch) != 0 || c.narrowed {
		t.Fatalf("moved below the floor: %v", ch)
	}
}

func TestAdapterGoesBackUpOnlyWhenTheBetterLayerFits(t *testing.T) {
	a := newAdapter(epoch, 2)
	now, _ := feedBW(a, epoch, 3+adaptBWSamples, 1_000_000, 2_000_000, 0)
	if a.layer != 1 {
		t.Fatalf("layer %d", a.layer)
	}
	narrowedAt := now
	// Clean for a long time, but the limit is remembered: it stays.
	now, ch := feedBW(a, now, 40, 1_500_000, 1_000_000, 2_000_000)
	if len(ch) != 0 {
		t.Fatalf("went up into a remembered limit: %v", ch)
	}
	// An estimate above 1.5 times the better layer, lasting, lifts the hold.
	_, ch = feedBW(a, now, adaptBWSamples, 3_100_000, 1_000_000, 2_000_000)
	if len(ch) != 1 || ch[0] != 0 {
		t.Fatalf("did not go up on a high estimate: %v", ch)
	}

	// Without such an estimate, the limit is forgotten after adaptCapHold.
	b := newAdapter(epoch, 2)
	now, _ = feedBW(b, epoch, 3+adaptBWSamples, 1_000_000, 2_000_000, 0)
	for now.Sub(narrowedAt) < adaptCapHold+time.Second && b.layer == 1 {
		now, _ = feedBW(b, now, 1, 1_500_000, 1_000_000, 2_000_000)
	}
	if b.layer != 0 || now.Sub(epoch) < adaptCapHold {
		t.Fatalf("layer %d at %v", b.layer, now.Sub(epoch))
	}
}

// link models a path that carries cap bits per second: what the viewer
// receives is the smaller of the layer's bitrate and the cap, nothing is
// lost (the sender's queue absorbs the rest), and the estimator reports about
// 1.5 times what arrives while the link keeps up and 0.85 times when it does not.
func (l link) sample(layer int) (est, cur, next int) {
	cur = l.rates[layer]
	if layer > 0 {
		next = l.rates[layer-1]
	}
	delivered := min(cur, l.cap)
	if cur > l.cap {
		return delivered * 85 / 100, cur, next
	}
	return delivered * 3 / 2, cur, next
}

type link struct {
	rates []int
	cap   int
}

func TestAdapterFollowsASimulatedBandwidthCap(t *testing.T) {
	l := link{rates: []int{6_000_000, 2_500_000, 1_000_000}, cap: 1_500_000}
	a := newAdapter(epoch, 2)
	now := epoch
	var moves []int
	step := func(n int) {
		for range n {
			now = now.Add(time.Second)
			a.setBandwidth(l.sample(a.layer))
			if layer, changed := a.observe(now, 0, 20*time.Millisecond); changed {
				moves = append(moves, layer)
			}
		}
	}
	// No loss, no rise of the round trip time: only the estimate can tell.
	step(30)
	if a.layer != 2 {
		t.Fatalf("layer %d after 30 s on a 1.5 Mbit/s link, moves %v", a.layer, moves)
	}
	if len(moves) != 2 || moves[0] != 1 || moves[1] != 2 {
		t.Fatalf("moves %v, want one layer at a time", moves)
	}
	// It does not keep trying the better layers against the cap.
	step(int(adaptCapHold/time.Second) - 40)
	if len(moves) != 2 {
		t.Fatalf("flapped while the limit is remembered: %v", moves)
	}
	// The link gets faster; once the limit is forgotten it moves up, one layer
	// at a time, and stays up.
	l.cap = 20_000_000
	step(300)
	if a.layer != 0 || moves[len(moves)-1] != 0 {
		t.Fatalf("layer %d after the link got faster, moves %v", a.layer, moves)
	}
	// The cap comes back: down again, without going further than needed.
	l.cap = 3_000_000
	step(60)
	if a.layer != 1 {
		t.Fatalf("layer %d on a 3 Mbit/s link, moves %v", a.layer, moves)
	}
}

func TestLayersMeasureTheirBitrates(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	g := rig.group
	pad := func(n int) []byte { return append(vp8Payload(true, false, 9, true, 5, 'a'), make([]byte, n)...) }
	g.sampleRates(layerNow()) // the first call only starts the clock
	clock.advance(time.Second)
	rig.pkt(0, 1, 1, pad(124_988)) // 125 kB: 1 Mbit/s
	g.sampleRates(layerNow())
	if got := g.rate[0].Load(); got < 900_000 || got > 1_100_000 {
		t.Fatalf("layer 0 rate %d", got)
	}
	// Someone else measuring: nothing to do.
	g.rateMu.Lock()
	clock.advance(time.Second)
	g.sampleRates(layerNow())
	g.rateMu.Unlock()
	clock.advance(-time.Second)
	// Asking again at once measures nothing new.
	rig.pkt(0, 2, 1, pad(124_988))
	g.sampleRates(layerNow())
	if g.rate[0].Load() > 1_100_000 {
		t.Fatal("measured twice in one interval")
	}
	// A second interval smooths; a layer that stops keeps its last size.
	clock.advance(time.Second)
	rig.pkt(0, 3, 1, pad(374_988)) // 3 Mbit/s plus the one from before
	g.sampleRates(layerNow())
	if got := g.rate[0].Load(); got < 1_500_000 || got > 3_500_000 {
		t.Fatalf("smoothed rate %d", got)
	}
	last := g.last[0].Load()
	clock.advance(time.Second)
	g.sampleRates(layerNow())
	if g.rate[0].Load() != 0 || g.last[0].Load() != last {
		t.Fatalf("after silence: rate %d last %d (was %d)", g.rate[0].Load(), g.last[0].Load(), last)
	}
}

func TestBandwidthEstimateMovesAnAutomaticViewerDown(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	rig.group.declared.Store(3)
	vt, _ := rig.viewer(-1)
	pad := func(key bool, n int) []byte {
		return append(vp8Payload(true, key, 10, true, 1, 'a'), make([]byte, n)...)
	}

	// Before the first keyframe nothing is known about the layer.
	vt.observeReportWithBandwidth(0, 0, 1_000_000)
	if vt.ad.bw != (bwSample{}) {
		t.Fatalf("sample before the viewer received a layer: %+v", vt.ad.bw)
	}
	rig.pkt(0, 100, 1000, pad(true, 10))

	before, moves := metrics.SFUBandwidth.Total(), metrics.SFUBandwidthMoves.Load()
	seq := uint16(101)
	second := func(est int) {
		clock.advance(time.Second)
		rig.pkt(0, seq, 1000, pad(false, 249_000)) // 2 Mbit/s
		rig.pkt(1, seq, 1000, pad(false, 99_000))  // 0.8 Mbit/s
		seq++
		vt.observeReportWithBandwidth(0, 20*time.Millisecond, est)
	}
	for range 3 + adaptBWSamples + 1 {
		second(1_000_000)
		if vt.wantA.Load() != 0 && metrics.SFUBandwidthMoves.Load() == moves {
			t.Fatal("a move was not credited to the estimate")
		}
	}
	if got := vt.wantA.Load(); got != 1 {
		t.Fatalf("wants layer %d with an estimate below the layer", got)
	}
	if metrics.SFUBandwidthMoves.Load() != moves+1 || metrics.SFUBandwidth.Total() <= before {
		t.Fatal("the move and the estimates were not counted")
	}

	// Once the viewer receives layer 1, the better layer's bitrate is the one measured for layer 0.
	rig.pkt(1, 600, 5, pad(true, 10))
	if vt.state().Layer != 1 {
		t.Fatalf("state %+v", vt.state())
	}
	second(5_000_000)
	if vt.ad.bwHigh != 0 && vt.ad.bw.next != 0 {
		t.Fatal("sample not consumed")
	}
	if got := rig.group.last[0].Load(); got < 1_500_000 {
		t.Fatalf("layer 0 last rate %d", got)
	}
}
