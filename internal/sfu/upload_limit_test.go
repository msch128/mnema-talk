package sfu

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
)

// limitRig is a layered share with n automatic viewers on its best layer,
// whose layers have the given bitrates (bits per second).
func limitRig(t *testing.T, n int, rates [3]int64) (*layerRig, []*viewerTrack) {
	t.Helper()
	rig := newLayerRig(t, "video/VP8", 3)
	rig.group.declared.Store(3)
	for i, r := range rates {
		rig.group.last[i].Store(r)
	}
	var vts []*viewerTrack
	for range n {
		vt, _ := rig.viewer(-1)
		vts = append(vts, vt)
	}
	// The layers keep the bitrates given above: no measuring from the few test packets.
	rig.group.rateAt = 1 << 62
	rig.pkt(0, 100, 1000, vp8Payload(true, true, 10, true, 1, 'a'))
	for _, vt := range vts {
		if vt.state().Layer != 0 {
			t.Fatalf("viewer not on the best layer: %+v", vt.state())
		}
	}
	return rig, vts
}

func viewsOf(t *testing.T, now time.Time, vts ...*viewerTrack) []uploadView {
	t.Helper()
	var out []uploadView
	for _, vt := range vts {
		v, ok := vt.uploadView(now)
		if !ok {
			t.Fatal("viewer has no view")
		}
		out = append(out, v)
	}
	return out
}

func TestAdapterSqueezeMovesDownAtOnceAndClimbsBackQuickly(t *testing.T) {
	a := newAdapter(epoch, 2)
	if !a.setSqueeze(epoch, 1) || a.layer != 1 || a.top() != 1 {
		t.Fatalf("layer %d top %d after one squeeze", a.layer, a.top())
	}
	if a.setSqueeze(epoch, 1) {
		t.Fatal("the same squeeze changed the layer")
	}
	a.setSqueeze(epoch, 9) // more than there are layers: the floor
	if a.layer != 2 || a.top() != 2 {
		t.Fatalf("layer %d top %d with a huge squeeze", a.layer, a.top())
	}
	// Let back, it climbs to the best without waiting for a long clean stretch.
	now := epoch.Add(time.Minute)
	a.setSqueeze(now, 0)
	r := &ramp{rates: []int{12_000_000, 5_200_000, 2_300_000}, cap: 1_000_000_000, est: bweInitialBitrate}
	_, moves := runRamp(a, r, now, 20)
	if a.layer != 0 || len(moves) != 2 {
		t.Fatalf("layer %d, moves %v 20 s after the squeeze was lifted", a.layer, moves)
	}
	// A squeeze never takes the viewer above the layer it is on.
	b := newAdapter(epoch, 2)
	b.layer = 2
	if b.setSqueeze(epoch, 1) || b.layer != 2 {
		t.Fatalf("squeeze moved a viewer already lower: %d", b.layer)
	}
}

func TestUploadViewSaysWhatAStepSavesAndCosts(t *testing.T) {
	clock := useFakeClock(t)
	_, vts := limitRig(t, 1, [3]int64{8_000_000, 4_000_000, 2_000_000})
	vt := vts[0]
	v := viewsOf(t, clock.now, vt)[0]
	if v.rate != 8_000_000 || v.down != 4_000_000 || v.up != 0 || v.squeeze != 0 {
		t.Fatalf("view %+v", v)
	}
	vt.setSqueeze(1)
	if got := vt.wantA.Load(); got != 1 {
		t.Fatalf("squeezed viewer wants layer %d", got)
	}
	// Not yet on layer 1 (waiting for its keyframe): the view is of layer 0 still.
	v = viewsOf(t, clock.now, vt)[0]
	if v.squeeze != 1 || v.up != 0 {
		t.Fatalf("view while switching %+v", v)
	}
}

func TestUploadViewIgnoresViewersItCannotMove(t *testing.T) {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	rig.group.declared.Store(3)
	chosen, _ := rig.viewer(0)
	auto, _ := rig.viewer(-1)
	if _, ok := auto.uploadView(clock.now); ok {
		t.Fatal("a viewer without a picture got a view")
	}
	rig.pkt(0, 100, 1000, vp8Payload(true, true, 10, true, 1, 'a'))
	if _, ok := chosen.uploadView(clock.now); ok {
		t.Fatal("a viewer who chose a layer got a view")
	}
	if _, ok := auto.uploadView(clock.now); ok {
		t.Fatal("a layer of unknown bitrate got a view")
	}
	chosen.setSqueeze(1) // does nothing to a chosen layer
	if chosen.wantA.Load() != 0 {
		t.Fatal("squeeze moved a viewer who chose")
	}
	auto.close()
	if _, ok := auto.uploadView(clock.now); ok {
		t.Fatal("a closed viewer got a view")
	}
	auto.setSqueeze(1)
}

func TestGovernorStepsTheBiggestConsumersDownFirstAndBackUpWithHysteresis(t *testing.T) {
	clock := useFakeClock(t)
	_, big := limitRig(t, 2, [3]int64{8_000_000, 4_000_000, 2_000_000})
	_, small := limitRig(t, 2, [3]int64{3_000_000, 1_500_000, 800_000})
	all := append(append([]*viewerTrack{}, big...), small...)
	g := &uploadGovernor{limit: 30_000_000}
	now := clock.now

	// Under 85%: nothing moves.
	if capped := g.step(now, 25_000_000, viewsOf(t, now, all...)); capped != 0 {
		t.Fatalf("%d capped under the limit", capped)
	}
	// 31 Mbit/s: 7 Mbit/s above the 80% target. One big viewer saves 4, the second 4.
	if capped := g.step(now, 31_000_000, viewsOf(t, now, all...)); capped != 2 {
		t.Fatalf("%d capped, want the two big viewers", capped)
	}
	for i, vt := range all {
		want := int32(0)
		if i < 2 {
			want = 1
		}
		if got := vt.wantA.Load(); got != want {
			t.Fatalf("viewer %d wants layer %d, want %d", i, got, want)
		}
	}
	// Still over, but inside the hold: nothing more.
	if capped := g.step(now.Add(time.Second), 31_000_000, viewsOf(t, now, all...)); capped != 2 || wanting(small, 0) != 2 {
		t.Fatalf("moved again inside the hold: %d capped, %d small viewers still on 0", capped, wanting(small, 0))
	}
	// Between 70% and 85%: no change either way.
	later := now.Add(uploadSqueezeHold + time.Second)
	views := viewsOf(t, later, all...)
	g.step(later, 24_000_000, views)
	if all[0].wantA.Load() != 1 || all[1].wantA.Load() != 1 {
		t.Fatal("moved inside the hysteresis band")
	}
	// Below 70%: one viewer is let back, then the next one only after the hold.
	g.step(later, 18_000_000, viewsOf(t, later, all...))
	if n := squeezed(all[:2]); n != 1 {
		t.Fatalf("%d viewers still squeezed after the first relax, want one", n)
	}
	g.step(later.Add(time.Second), 18_000_000, viewsOf(t, later, all...))
	if n := squeezed(all[:2]); n != 1 {
		t.Fatalf("a second viewer was let back inside the relax hold: %d squeezed", n)
	}
	g.step(later.Add(uploadRelaxHold+time.Second), 18_000_000, viewsOf(t, later, all...))
	if n := squeezed(all[:2]); n != 0 {
		t.Fatalf("%d still squeezed after the hold, want none", n)
	}
	// With little room the cost of coming back is not taken on.
	g2 := &uploadGovernor{limit: 30_000_000}
	g2.step(later, 31_000_000, viewsOf(t, later, all...))
	if squeezed(all[:2]) != 2 {
		t.Fatalf("second round: %d squeezed", squeezed(all[:2]))
	}
	g2.step(later.Add(time.Hour), 1_000_000, viewsOf(t, later, all...))
	if n := squeezed(all[:2]); n != 1 {
		t.Fatalf("%d squeezed after one relax with plenty of room", n)
	}
}

// squeezed counts the viewers the governor holds below their best layer.
func squeezed(vts []*viewerTrack) int {
	n := 0
	for _, vt := range vts {
		vt.mu.Lock()
		if vt.ad.squeeze > 0 {
			n++
		}
		vt.mu.Unlock()
	}
	return n
}

func wanting(vts []*viewerTrack, layer int32) int {
	n := 0
	for _, vt := range vts {
		if vt.wantA.Load() == layer {
			n++
		}
	}
	return n
}

func TestGovernorCountsWhatItCannotStepDown(t *testing.T) {
	g := &uploadGovernor{limit: 10_000_000}
	before := metrics.SFUUploadUnmet.Load()
	now := time.Unix(100, 0)
	// Audio and plain tracks only: no views.
	g.step(now, 20_000_000, nil)
	g.step(now.Add(time.Second), 20_000_000, nil)
	if got := metrics.SFUUploadUnmet.Load() - before; got != 2 {
		t.Fatalf("unmet counted %d times, want 2", got)
	}
	// Viewers on their smallest layer cannot go lower either.
	clock := useFakeClock(t)
	_, vts := limitRig(t, 1, [3]int64{8_000_000, 4_000_000, 2_000_000})
	vts[0].setSqueeze(9)
	v := viewsOf(t, clock.now, vts...)
	g.step(now.Add(2*time.Minute), 20_000_000, v)
	if got := metrics.SFUUploadUnmet.Load() - before; got != 3 {
		t.Fatalf("unmet counted %d times, want 3", got)
	}
}

// The loop reads the egress, steps the governor and publishes the numbers.
func TestUploadGovernorLoopFollowsTheEgressAndStopsWithItsContext(t *testing.T) {
	clock := useFakeClock(t)
	_, vts := limitRig(t, 2, [3]int64{8_000_000, 4_000_000, 2_000_000})
	var mu sync.Mutex
	var bytes uint64
	var fake = time.Unix(2_000_000, 0)
	src := uploadSource{
		egressBytes: func() uint64 { mu.Lock(); defer mu.Unlock(); b := bytes; bytes += 2_500_000; return b },
		now:         func() time.Time { mu.Lock(); defer mu.Unlock(); fake = fake.Add(time.Second); return fake },
		views: func(now time.Time) []uploadView {
			return viewsOf(t, clock.now, vts...)
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runUploadGovernor(ctx, 20_000_000, time.Millisecond, src); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for metrics.SFUUploadCapped.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if metrics.SFUUploadCapped.Load() == 0 || metrics.SFUUploadLimit.Load() != 20_000_000 || metrics.SFUUploadEgress.Load() == 0 {
		t.Fatalf("capped %d limit %d egress %d", metrics.SFUUploadCapped.Load(), metrics.SFUUploadLimit.Load(), metrics.SFUUploadEgress.Load())
	}
	cancel()
	<-done
	if metrics.SFUUploadLimit.Load() != 0 {
		t.Fatal("the limit gauge stayed after the loop ended")
	}
}

func TestStartUploadGovernorIsOffAtZeroAndFindsEveryLayerGroup(t *testing.T) {
	s := newTestSFU(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.StartUploadGovernor(ctx, 0)
	if metrics.SFUUploadLimit.Load() != 0 {
		t.Fatal("a limit of 0 started the governor")
	}
	clock := useFakeClock(t)
	rig, vts := limitRig(t, 1, [3]int64{8_000_000, 4_000_000, 2_000_000})
	cam, camViewers := limitRig(t, 1, [3]int64{2_000_000, 1_000_000, 500_000})
	s.rooms[uuid.New()] = &Room{
		groups:       map[uuid.UUID]*layerGroup{uuid.New(): rig.group},
		cameraGroups: map[uuid.UUID]*layerGroup{uuid.New(): cam.group},
	}
	s.rooms[uuid.New()] = &Room{} // a room with no layers
	got := s.uploadViews(clock.now)
	if len(got) != 2 {
		t.Fatalf("%d views, want the screen's and the camera's viewer", len(got))
	}
	_ = vts
	_ = camViewers
	s.StartUploadGovernor(ctx, 5)
	deadline := time.Now().Add(5 * time.Second)
	for metrics.SFUUploadLimit.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if metrics.SFUUploadLimit.Load() != 5_000_000 {
		t.Fatalf("limit gauge %d", metrics.SFUUploadLimit.Load())
	}
	cancel()
}

// Coming back costs the difference between the layers; with less room than
// that the viewer stays where it is.
func TestGovernorDoesNotLetBackWhatTheRoomCannotCarry(t *testing.T) {
	clock := useFakeClock(t)
	rig, vts := limitRig(t, 1, [3]int64{8_000_000, 4_000_000, 2_000_000})
	g := &uploadGovernor{limit: 30_000_000}
	g.step(clock.now, 31_000_000, viewsOf(t, clock.now, vts...))
	rig.pkt(1, 500, 5, vp8Payload(true, true, 50, true, 1, 'b')) // the viewer arrives on layer 1
	if vts[0].state().Layer != 1 {
		t.Fatalf("state %+v", vts[0].state())
	}
	later := clock.now.Add(time.Hour)
	g.step(later, 20_500_000, viewsOf(t, later, vts...)) // 20.5 + 4 > 24
	if squeezed(vts) != 1 {
		t.Fatal("the viewer was let back although the room could not carry it")
	}
	g.step(later, 19_000_000, viewsOf(t, later, vts...)) // 19 + 4 <= 24
	if squeezed(vts) != 0 {
		t.Fatal("the viewer was not let back with room for it")
	}
}
