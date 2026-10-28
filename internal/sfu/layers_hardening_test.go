package sfu

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A viewer that floods keyframe requests costs constant work per request and
// counts as one trouble signal.
func TestKeyframeFloodIsConstantWorkAndTrouble(t *testing.T) {
	a := newAdapter(epoch, 2)
	now := epoch.Add(10 * time.Second)
	if !a.keyframeRequested(now) {
		t.Fatal("the first request must be counted")
	}
	allocs := testing.AllocsPerRun(1000, func() {
		if a.keyframeRequested(now.Add(10 * time.Millisecond)) {
			t.Fatal("a request inside the gap was counted")
		}
	})
	if allocs != 0 {
		t.Fatalf("a flooded request allocated %v times", allocs)
	}
	if got := a.recentPLIs(now); got != 1 {
		t.Fatalf("flood counted %d requests", got)
	}
	if layer, changed := a.observe(now.Add(time.Second), 0, 0); !changed || layer != 1 {
		t.Fatalf("flood is one trouble signal: %d %v", layer, changed)
	}
	// The signal is consumed; the next clean report is clean.
	if _, changed := a.observe(now.Add(5*time.Second), 0, 0); changed {
		t.Fatal("flood signal not consumed")
	}
	// Slow requests wrap the ring without growing it.
	for i := range 50 {
		a.keyframeRequested(now.Add(time.Duration(i) * time.Second))
	}
	if got := a.recentPLIs(now.Add(49 * time.Second)); got > adaptPLIs {
		t.Fatalf("ring holds %d", got)
	}
}

// Spamming PLIs on one viewer must not stall the forwarding path of the share.
func TestKeyframeFloodDoesNotStallForwarding(t *testing.T) {
	rig := newLayerRig(t, "video/VP8", 2)
	flooder, _ := rig.viewer(-1)
	other, out := rig.viewer(0)
	_ = other

	var stop atomic.Bool
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				flooder.viewerKeyframe()
			}
		}()
	}
	start := time.Now()
	rig.pkt(0, 1, 1, vp8Payload(true, true, 1, true, 1, 'k'))
	for i := 0; i < 2000; i++ {
		rig.pkt(0, uint16(2+i), uint32(2+i), vp8Payload(true, false, uint16(2+i), true, 1, 'd'))
	}
	took := time.Since(start)
	stop.Store(true)
	wg.Wait()
	if took > 3*time.Second {
		t.Fatalf("forwarding 2000 packets took %v under a PLI flood", took)
	}
	if n := len(out.take()); n != 2001 {
		t.Fatalf("the other viewer got %d packets", n)
	}
}

// The share is stopped and started again with the same wanted layers inside
// the hold: the sender is told again, and no silent layer is resolved.
func TestRestartWithinHoldAnnouncesAgain(t *testing.T) {
	useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 2)
	g := rig.group
	g.setDeclared(2)
	next := func() []int {
		t.Helper()
		select {
		case l := <-rig.wanted:
			return l
		case <-time.After(2 * time.Second):
			t.Fatal("no announcement")
			return nil
		}
	}
	vt, _ := rig.viewer(1)
	for { // drain to the settled announcement
		l := next()
		if len(l) == 2 {
			break
		}
	}
	g.shareStopped()
	if g.resolve(1) != -1 || g.live.Load() != 0 || g.declared.Load() != 0 {
		t.Fatal("a stopped share kept live layers")
	}
	select {
	case l := <-rig.wanted:
		t.Fatalf("a stopped share announced %v", l)
	case <-time.After(50 * time.Millisecond):
	}
	// Restart: layers arrive and are declared; the viewer still wants layer 1.
	g.markLive(rig.infos[0])
	g.markLive(rig.infos[1])
	g.markLive(rig.infos[1]) // already live: nothing to do
	g.setDeclared(2)
	if l := next(); len(l) != 1 || l[0] != 1 {
		t.Fatalf("restart announced %v", l)
	}
	if vt.eff != 1 {
		t.Fatalf("viewer resolved %d", vt.eff)
	}
	// markLive ignores a layer that was replaced.
	old := rig.infos[0]
	g.shareStopped()
	g.infos[0] = nil
	g.markLive(old)
	if g.live.Load() != 0 {
		t.Fatal("a detached layer came back to life")
	}
}

// A new capture on a group that outlived the last share announces afresh.
func TestGroupReuseResetsAnnouncement(t *testing.T) {
	useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 1)
	g := rig.group
	g.refreshWanted()
	<-rig.wanted
	g.mu.Lock()
	g.resetAnnouncedLocked()
	g.hold = [maxLayers]time.Time{}
	g.mu.Unlock()
	g.refreshWanted()
	select {
	case <-rig.wanted:
	case <-time.After(2 * time.Second):
		t.Fatal("no announcement after the reset")
	}
}

// With no loss to report because nothing arrives, an automatic viewer moves
// to a layer that delivers.
func TestAutoViewerEscapesATotalStall(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 2)
	vt, out := rig.viewer(-1)
	rig.pkt(0, 1, 1, vp8Payload(true, true, 1, true, 1, 'k'))
	out.take()
	if vt.ad.layer != 0 {
		t.Fatal("did not start on the best layer")
	}

	// Quiet for a short while, nothing flows: no move.
	clock.advance(time.Second)
	vt.observeReport(0, 0)
	if vt.ad.layer != 0 {
		t.Fatal("moved too early")
	}
	// Past the stall time but no other layer delivers: stays.
	clock.advance(2 * time.Second)
	vt.observeReport(0, 0)
	if vt.ad.layer != 0 {
		t.Fatal("moved with nowhere to go")
	}
	// Layer 1 delivers (not a keyframe: it only shows that it flows).
	rig.pkt(1, 1, 1, vp8Payload(true, false, 1, true, 1, 'd'))
	vt.observeReport(0, 0)
	if vt.ad.layer != 1 || vt.wantA.Load() != 1 {
		t.Fatalf("stalled viewer stayed on layer %d", vt.ad.layer)
	}
	if !rig.asked(1) {
		t.Fatal("the new layer was not asked for a keyframe")
	}
	// The move restarts the clock for the reports that follow.
	vt.observeReport(0, 0)
	if vt.ad.layer != 1 {
		t.Fatal("moved again")
	}
	rig.pkt(1, 2, 2, vp8Payload(true, true, 2, true, 1, 'k'))
	if len(out.take()) == 0 {
		t.Fatal("no picture after the switch")
	}
}

func TestStallCornerCases(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	g := rig.group
	// A viewer that never got a packet is judged from its creation.
	vt, _ := rig.viewer(-1)
	clock.advance(3 * time.Second)
	rig.pkt(2, 1, 1, vp8Payload(true, false, 1, true, 1, 'd'))
	if got := g.flowing(0, layerNow()); got != 2 {
		t.Fatalf("flowing layer %d", got)
	}
	if got := g.flowing(3, layerNow()); got != 2 {
		t.Fatalf("flowing layer below the wish: %d", got)
	}
	vt.observeReport(0, 0)
	if vt.ad.layer != 2 {
		t.Fatalf("never-started viewer on %d", vt.ad.layer)
	}
	// The flowing layer is the one it is on already: no move.
	clock.advance(3 * time.Second)
	rig.pkt(2, 2, 2, vp8Payload(true, false, 2, true, 1, 'd'))
	if vt.stalledLocked(layerNow()) {
		t.Fatal("moved onto its own layer")
	}
	// Worse layers win ties; a dead layer is not flowing.
	clock.advance(3 * time.Second)
	if g.flowing(1, layerNow()) != -1 {
		t.Fatal("a silent group has a flowing layer")
	}
	rig.pkt(0, 1, 1, vp8Payload(true, false, 1, true, 1, 'd'))
	rig.pkt(2, 3, 3, vp8Payload(true, false, 3, true, 1, 'd'))
	if got := g.flowing(1, layerNow()); got != 2 {
		t.Fatalf("tie went to %d", got)
	}
	g.detach(rig.infos[2])
	if got := g.flowing(1, layerNow()); got != 0 {
		t.Fatalf("only layer 0 is live: %d", got)
	}
	// A viewer with a choice of its own is not touched.
	fixed, _ := rig.viewer(1)
	fixed.observeReport(0, 0)
	if fixed.pref != 1 {
		t.Fatal("a chosen layer moved")
	}
	// A flood does not reach the publisher for a chosen layer either way.
	fixed.viewerKeyframe()
}

func TestSetScreenLayersUnchangedDoesNothing(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	pub.join(s, room)
	r := s.Room(room)
	layers := []Layer{{1920, 1080, 30}, {1280, 720, 30}}
	if err := r.SetScreenLayers(pub.id, layers); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	first := &r.layerMeta[pub.id][0]
	r.mu.RUnlock()
	if err := r.SetScreenLayers(pub.id, layers); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	same := &r.layerMeta[pub.id][0] == first
	r.mu.RUnlock()
	if !same {
		t.Fatal("unchanged layers were stored again")
	}
	if err := r.SetScreenLayers(pub.id, layers[:1]); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	got := r.layerMeta[pub.id]
	r.mu.RUnlock()
	if len(got) != 1 {
		t.Fatalf("changed layers not stored: %v", got)
	}
}
