package sfu

import (
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

func TestResendBudgetFillsAtTheFloorOnAQuietLink(t *testing.T) {
	b := newResendBudget(resendLimits{floor: 100_000, share: 0.25, burst: time.Second})
	now := time.Unix(1, 0)
	// A full bucket (a second of 100 KB/s) takes 100 packets of 1000 bytes.
	granted := 0
	for b.allow(now, 1000, 0) {
		granted++
	}
	if granted != 100 {
		t.Fatalf("burst of %d packets, want 100", granted)
	}
	// Half a second later 50 more fit.
	now = now.Add(500 * time.Millisecond)
	granted = 0
	for b.allow(now, 1000, 0) {
		granted++
	}
	if granted != 50 {
		t.Fatalf("refill of %d packets after 500 ms, want 50", granted)
	}
	// Idle time never stores more than the burst.
	now = now.Add(time.Hour)
	granted = 0
	for b.allow(now, 1000, 0) {
		granted++
	}
	if granted != 100 {
		t.Fatalf("after an hour %d packets, want the burst of 100", granted)
	}
}

func TestResendBudgetFollowsTheMediaRate(t *testing.T) {
	l := resendLimits{floor: 100_000, share: 0.25, burst: time.Second}
	if got := l.rate(0); got != 100_000 {
		t.Fatalf("quiet rate %v", got)
	}
	if got := l.rate(4_000_000); got != 1_000_000 {
		t.Fatalf("rate for 4 MB/s of media = %v, want a quarter", got)
	}
}

func TestResendBudgetLetsAPacketLargerThanTheBucketThroughOnceFull(t *testing.T) {
	b := newResendBudget(resendLimits{floor: 1000, share: 0, burst: time.Second})
	now := time.Unix(1, 0)
	if !b.allow(now, 5000, 0) {
		t.Fatal("a full bucket refused a large packet")
	}
	if b.allow(now, 5000, 0) {
		t.Fatal("an empty bucket allowed a large packet")
	}
	if b.allow(now.Add(-time.Second), 10, 0) {
		t.Fatal("time going backwards refilled the bucket")
	}
}

// A subscriber asking again and again for everything it was sent cannot make
// the server resend more than the budget allows, and what is refused is counted.
func TestARetransmissionStormIsLimited(t *testing.T) {
	clk := &clock{t: time.Unix(1_000_000, 0)}
	limits := resendLimits{floor: 20_000, share: 0, burst: time.Second}
	icp, _ := nackResponderFactory{now: clk.now, limits: limits}.NewInterceptor("")
	n := icp.(*nackResponder)
	w := &wire{}
	out := n.BindLocalStream(videoInfo(80, false), w)
	for seq := uint16(0); seq < 100; seq++ {
		send(out, 80, seq, 1000)
	}
	w.take()
	dropped := metrics.SFURetransmitDropped.Load()

	all := make([]uint16, 0, 100)
	for seq := uint16(0); seq < 100; seq++ {
		all = append(all, seq)
	}
	resentBytes := 0
	for round := 0; round < 10; round++ {
		clk.advance(100 * time.Millisecond) // past the per-packet interval
		nackFor(t, n, 80, all...)
		for _, p := range w.take() {
			resentBytes += len(p.Payload)
		}
	}
	// A second of storm at 20 KB/s: the burst (20 KB) plus the refill (20 KB).
	if resentBytes > 45_000 || resentBytes < 15_000 {
		t.Fatalf("%d bytes resent in a second of storm, want about 40000", resentBytes)
	}
	if metrics.SFURetransmitDropped.Load() == dropped {
		t.Fatal("refused resends were not counted")
	}
	// A refused packet may be asked for again once the budget has refilled.
	clk.advance(5 * time.Second)
	nackFor(t, n, 80, 99)
	if len(w.take()) != 1 {
		t.Fatal("a refused packet could not be asked for again")
	}
}

func TestMediaRateSumsTheStreamsOfAConnection(t *testing.T) {
	clk := &clock{t: time.Unix(1_000_000, 0)}
	icp, _ := nackResponderFactory{now: clk.now}.NewInterceptor("")
	n := icp.(*nackResponder)
	a := n.BindLocalStream(videoInfo(90, false), &wire{})
	b := n.BindLocalStream(videoInfo(91, false), &wire{})
	for i := 0; i < 400; i++ {
		send(a, 90, uint16(i), 1000)
		send(b, 91, uint16(i), 500)
		clk.advance(10 * time.Millisecond)
	}
	got := n.mediaRate()
	// 100 pps of about 1.1 KB and about 0.6 KB.
	if got < 150_000 || got > 200_000 {
		t.Fatalf("media rate %v, want about 170000 bytes/s", got)
	}
}
