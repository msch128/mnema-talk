package sfu

import (
	"testing"
	"time"

	"github.com/pion/rtcp"
)

var epoch = time.Unix(1_700_000_000, 0)

// feed gives the adapter a clean report every second for d, returning the
// layer changes it made with the time they happened.
func feed(a *adapter, from time.Time, d time.Duration, loss float64, rtt time.Duration) (end time.Time, changes []int) {
	end = from
	for t := time.Second; t <= d; t += time.Second {
		end = from.Add(t)
		if layer, changed := a.observe(end, loss, rtt); changed {
			changes = append(changes, layer)
		}
	}
	return end, changes
}

func TestAdapterStartsBestAndIgnoresTheKeyframeBurst(t *testing.T) {
	a := newAdapter(epoch, 2)
	if a.layer != 0 {
		t.Fatalf("starts on layer %d", a.layer)
	}
	// Even severe loss in the first seconds is the keyframe's, not the link's.
	if layer, changed := a.observe(epoch.Add(time.Second), 0.9, 0); changed || layer != 0 {
		t.Fatalf("moved during the grace period: %d %v", layer, changed)
	}
	if layer, changed := a.observe(epoch.Add(adaptGrace+time.Second), 0.9, 0); !changed || layer != 1 {
		t.Fatalf("severe loss after the grace period: %d %v", layer, changed)
	}
}

func TestAdapterMovesDownOneLayerAtATimeAndStopsAtTheFloor(t *testing.T) {
	a := newAdapter(epoch, 2)
	now := epoch.Add(10 * time.Second)
	for want := 1; want <= 2; want++ {
		layer, changed := a.observe(now, 0.5, 0)
		if !changed || layer != want {
			t.Fatalf("step %d: layer %d changed %v", want, layer, changed)
		}
		// The reports right after a move are the move's own doing.
		if _, again := a.observe(now.Add(time.Second), 0.5, 0); again {
			t.Fatal("moved twice inside the grace period")
		}
		now = now.Add(adaptGrace + time.Second)
	}
	if layer, changed := a.observe(now, 0.9, 0); changed || layer != 2 {
		t.Fatalf("below the floor: %d %v", layer, changed)
	}
}

func TestAdapterNeedsLasting_OrSevere_Loss(t *testing.T) {
	a := newAdapter(epoch, 2)
	now := epoch.Add(10 * time.Second)
	// One lossy report between clean ones is a blip.
	for _, loss := range []float64{0, 0.08, 0, 0.08, 0} {
		if _, changed := a.observe(now, loss, 0); changed {
			t.Fatalf("moved on a blip (loss %v)", loss)
		}
		now = now.Add(time.Second)
	}
	// Two in a row are not.
	a.observe(now, 0.08, 0)
	if layer, changed := a.observe(now.Add(time.Second), 0.08, 0); !changed || layer != 1 {
		t.Fatalf("lasting loss: %d %v", layer, changed)
	}
}

func TestAdapterWatchesTheRoundTripTime(t *testing.T) {
	a := newAdapter(epoch, 2)
	now := epoch.Add(10 * time.Second)
	// Too few samples to judge.
	for i := 0; i < adaptMinRTTSamples-1; i++ {
		if _, changed := a.observe(now, 0, 900*time.Millisecond); changed {
			t.Fatal("judged the round trip time on too few samples")
		}
		now = now.Add(time.Second)
	}
	b := newAdapter(epoch, 2)
	now = epoch.Add(10 * time.Second)
	for i := 0; i < 6; i++ {
		b.observe(now, 0, 40*time.Millisecond)
		now = now.Add(time.Second)
	}
	// A rise within the margin is queueing noise.
	if _, changed := b.observe(now, 0, 40*time.Millisecond+adaptRTTRise); changed {
		t.Fatal("moved on a small rise")
	}
	if layer, changed := b.observe(now.Add(time.Second), 0, 40*time.Millisecond+adaptRTTRise+time.Millisecond); !changed || layer != 1 {
		t.Fatalf("queues filling up: %d %v", layer, changed)
	}
	// A report without a round trip time does not count against it.
	if _, changed := b.observe(now.Add(10*time.Second), 0, 0); changed {
		t.Fatal("a report without rtt moved the viewer")
	}
}

func TestAdapterTreatsRepeatedKeyframeRequestsAsTrouble(t *testing.T) {
	a := newAdapter(epoch, 2)
	now := epoch.Add(10 * time.Second)
	a.keyframeRequested(now)
	a.keyframeRequested(now.Add(time.Second))
	if _, changed := a.observe(now.Add(2*time.Second), 0, 0); changed {
		t.Fatal("two requests are not trouble")
	}
	a.keyframeRequested(now.Add(3 * time.Second))
	if layer, changed := a.observe(now.Add(3*time.Second), 0, 0); !changed || layer != 1 {
		t.Fatalf("three requests: %d %v", layer, changed)
	}
	// Old requests expire and are forgotten.
	b := newAdapter(epoch, 2)
	for i := 0; i < 5; i++ {
		b.keyframeRequested(now.Add(time.Duration(i) * 3 * time.Second))
	}
	if n := b.recentPLIs(now.Add(15 * time.Second)); n > 2 {
		t.Fatalf("kept %d requests", n)
	}
	if _, changed := b.observe(now.Add(15*time.Second), 0, 0); changed {
		t.Fatal("spread-out requests moved the viewer")
	}
}

// A viewer waiting for the first keyframe of a new layer asks for it again
// and again; those requests are no trouble once the grace period is over.
func TestKeyframeRequestsDuringGraceAreNotTrouble(t *testing.T) {
	a := newAdapter(epoch, 2)
	now := epoch.Add(10 * time.Second)
	a.stalledTo(now, 1)
	for _, d := range []time.Duration{300, 700, 1200} {
		if !a.keyframeRequested(now.Add(d * time.Millisecond)) {
			t.Fatal("a request in the grace period was treated as a flood")
		}
	}
	if _, changed := a.observe(now.Add(adaptGrace+time.Millisecond), 0, 0); changed {
		t.Fatal("requests made during the grace period moved the viewer")
	}
}

func TestAdapterMovesUpOnlyAfterALongCleanStretch(t *testing.T) {
	a := newAdapter(epoch, 2)
	now := epoch.Add(10 * time.Second)
	a.observe(now, 0.5, 0) // down to layer 1
	end, changes := feed(a, now, adaptCleanFor-time.Second, 0, 30*time.Millisecond)
	if len(changes) != 0 {
		t.Fatalf("moved up after only %v: %v", end.Sub(now), changes)
	}
	_, changes = feed(a, end, 3*time.Second, 0, 30*time.Millisecond)
	if len(changes) != 1 || changes[0] != 0 {
		t.Fatalf("moves up after a clean stretch: %v", changes)
	}
	if a.layer != 0 {
		t.Fatalf("layer %d", a.layer)
	}
}

func TestAdapterAnythingButCleanRestartsTheWait(t *testing.T) {
	a := newAdapter(epoch, 2)
	now := epoch.Add(10 * time.Second)
	a.observe(now, 0.5, 0)
	end, _ := feed(a, now, 10*time.Second, 0, 0)
	// Mild loss (not trouble, not clean) at second ten.
	if _, changed := a.observe(end.Add(time.Second), 0.02, 0); changed {
		t.Fatal("mild loss moved the viewer")
	}
	_, changes := feed(a, end.Add(time.Second), adaptCleanFor-time.Second, 0, 0)
	if len(changes) != 0 {
		t.Fatal("the wait was not restarted by mild loss")
	}
	_, changes = feed(a, end.Add(adaptCleanFor), 3*time.Second, 0, 0)
	if len(changes) != 1 {
		t.Fatalf("never moved up: %v", changes)
	}
}

func TestAdapterBacksOffAfterAFailedAttempt(t *testing.T) {
	a := newAdapter(epoch, 2)
	now := epoch.Add(10 * time.Second)
	a.observe(now, 0.5, 0) // layer 1
	now, changes := feed(a, now, adaptCleanFor+time.Second, 0, 0)
	if len(changes) != 1 || a.layer != 0 {
		t.Fatalf("first attempt: %v layer %d", changes, a.layer)
	}
	// Trouble right after the move up: the attempt failed.
	now = now.Add(adaptGrace + time.Second)
	if layer, changed := a.observe(now, 0.5, 0); !changed || layer != 1 {
		t.Fatalf("failure: %d %v", layer, changed)
	}
	// The next attempt waits twice as long.
	start := now
	var movedAfter time.Duration
	for t0 := time.Second; t0 < 3*time.Minute; t0 += time.Second {
		if _, changed := a.observe(start.Add(t0), 0, 0); changed {
			movedAfter = t0
			break
		}
	}
	if movedAfter < 2*adaptCleanFor || movedAfter > 2*adaptCleanFor+2*time.Second {
		t.Fatalf("second attempt after %v, want about %v", movedAfter, 2*adaptCleanFor)
	}
	if a.backoff != 2*adaptCleanFor {
		t.Fatalf("backoff %v after one failure", a.backoff)
	}
	// Failing again doubles again, up to a cap.
	now = start.Add(movedAfter + adaptGrace + time.Second)
	a.observe(now, 0.5, 0)
	if a.backoff != 4*adaptCleanFor {
		t.Fatalf("backoff %v after two failures", a.backoff)
	}
	a.layer, a.backoff, a.graceUntil, a.probeEnd = 0, adaptBackoffMax, time.Time{}, now.Add(time.Hour)
	a.observe(now.Add(time.Second), 0.5, 0)
	if a.backoff != adaptBackoffMax {
		t.Fatalf("backoff %v, want the cap %v", a.backoff, adaptBackoffMax)
	}
}

func TestAdapterForgivesAfterASuccessfulAttempt(t *testing.T) {
	a := newAdapter(epoch, 2)
	now := epoch.Add(10 * time.Second)
	a.observe(now, 0.5, 0)
	a.backoff = 4 * adaptCleanFor // earlier failures
	a.holdUntil = now
	now, _ = feed(a, now, adaptCleanFor+time.Second, 0, 0)
	if a.layer != 0 {
		t.Fatalf("no attempt, layer %d", a.layer)
	}
	// The probe window passes without trouble.
	feed(a, now, adaptProbe+2*time.Second, 0, 0)
	if a.backoff != adaptCleanFor {
		t.Fatalf("backoff %v after a successful attempt", a.backoff)
	}
}

func TestAdapterFloorFollowsTheShare(t *testing.T) {
	a := newAdapter(epoch, 2)
	a.layer = 2
	a.setFloor(1)
	if a.layer != 1 || a.floor != 1 {
		t.Fatalf("layer %d floor %d", a.layer, a.floor)
	}
	a.setFloor(2)
	if a.layer != 1 {
		t.Fatal("a wider floor moved the layer")
	}
}

func TestReceiverReportOf(t *testing.T) {
	now := time.Unix(1_700_000_000, 123_000_000)
	lsr := ntpMiddle32(now.Add(-300 * time.Millisecond))
	delay := uint32(65536 / 10)
	rr := &rtcp.ReceiverReport{Reports: []rtcp.ReceptionReport{
		{SSRC: 7, FractionLost: 64, LastSenderReport: lsr, Delay: delay},
		{SSRC: 9, FractionLost: 255},
	}}
	loss, rtt, ok := receiverReportOf(rr, 7, now)
	if !ok || loss != 0.25 {
		t.Fatalf("loss %v ok %v", loss, ok)
	}
	if rtt < 195*time.Millisecond || rtt > 205*time.Millisecond {
		t.Fatalf("rtt %v, want about 200ms", rtt)
	}
	// Another stream's block is not ours; nothing at all means nothing.
	if loss, _, ok := receiverReportOf(rr, 9, now); !ok || loss < 0.99 {
		t.Fatalf("second block: %v %v", loss, ok)
	}
	if _, _, ok := receiverReportOf(rr, 8, now); ok {
		t.Fatal("a report about no stream of ours counted")
	}
	// No sender report seen yet, or a stale or future one: no round trip time.
	for name, block := range map[string]rtcp.ReceptionReport{
		"none":   {SSRC: 7},
		"stale":  {SSRC: 7, LastSenderReport: ntpMiddle32(now.Add(-30 * time.Second))},
		"future": {SSRC: 7, LastSenderReport: ntpMiddle32(now.Add(5 * time.Second))},
	} {
		if _, rtt, ok := receiverReportOf(&rtcp.ReceiverReport{Reports: []rtcp.ReceptionReport{block}}, 7, now); !ok || rtt != 0 {
			t.Errorf("%s: rtt %v ok %v", name, rtt, ok)
		}
	}
}

func TestNTPMiddle32(t *testing.T) {
	// 1 January 1970 is 2208988800 seconds into the NTP era; the middle 32
	// bits hold the low 16 bits of the seconds and the top 16 of the fraction.
	if got, want := ntpMiddle32(time.Unix(0, 0)), uint32((2208988800&0xffff)<<16); got != want {
		t.Fatalf("epoch: %#x, want %#x", got, want)
	}
	// Half a second adds 0x8000.
	if got, want := ntpMiddle32(time.Unix(0, 500_000_000)), uint32((2208988800&0xffff)<<16|0x8000); got != want {
		t.Fatalf("half second: %#x, want %#x", got, want)
	}
}
