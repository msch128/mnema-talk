package sfu

import (
	"math/rand/v2"
	"testing"
	"time"
)

// Numbers across a layer switch. A viewer of a layered video gets one
// stream: switchLocked makes the new layer's sequence numbers continue from
// the last packet forwarded. When that packet arrived late (reordered on
// the way, or repaired after a loss) the highest number the viewer already
// has is not the last one, and the new layer's first packets reuse numbers
// the viewer holds for other packets. A receiver drops a packet whose number
// it has, so the new layer's keyframe arrives incomplete.

// switchCase is one generated stream: layer 0 from start, `late` of its last
// packets delivered in reverse (0 = in order), then a switch to layer 2.
type switchCase struct {
	start uint16
	run   int
	late  int
}

// deliverSwitch plays c and returns the sequence numbers the viewer got.
func deliverSwitch(t *testing.T, c switchCase) []uint16 {
	clock := useFakeClock(t)
	rig := newLayerRig(t, "video/VP8", 3)
	vt, out := rig.viewer(0)
	order := make([]int, c.run)
	for i := range order {
		order[i] = i
	}
	for i, j := c.run-c.late, c.run-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}
	for _, i := range order {
		seq := c.start + uint16(i)
		rig.pkt(0, seq, uint32(i)*3000, vp8Payload(true, i == 0, uint16(10+i), true, 5, 'a'))
	}
	vt.setPref(2)
	clock.advance(100 * time.Millisecond)
	rig.pkt(2, 7001, 60, vp8Payload(true, true, 301, true, 3, 'c'))
	rig.pkt(2, 7002, 60, vp8Payload(false, true, 301, true, 3, 'c'))
	rig.pkt(2, 7003, 3060, vp8Payload(true, false, 302, true, 3, 'c'))
	return seqs(out.take())
}

// duplicates counts sequence numbers delivered more than once.
func duplicates(got []uint16) int {
	seen, dup := map[uint16]bool{}, 0
	for _, s := range got {
		if seen[s] {
			dup++
		}
		seen[s] = true
	}
	return dup
}

func generateSwitchCases(n int) []switchCase {
	r := rand.New(rand.NewPCG(9, 9))
	out := make([]switchCase, 0, n)
	for range n {
		start := uint16(r.IntN(1 << 16))
		if r.IntN(2) == 0 {
			start = uint16(65536 - 1 - r.IntN(8)) // across the wrap
		}
		run := 2 + r.IntN(8)
		out = append(out, switchCase{start: start, run: run, late: r.IntN(min(run, 4))})
	}
	return out
}

// KNOWN DEFECT (lane 9, H-L9-03): in every case whose last packet before
// the switch was late, the new layer reuses numbers the viewer has; in order
// (late 0 or 1) it never does. Wanted: no case delivers a number twice.
func TestKnownDefectLatePacketBeforeASwitchReusesSequenceNumbers(t *testing.T) {
	var inOrder, late, lateWithDup, inOrderWithDup int
	for _, c := range generateSwitchCases(400) {
		got := deliverSwitch(t, c)
		if len(got) != c.run+3 {
			t.Fatalf("%+v: delivered %d packets, want %d", c, len(got), c.run+3)
		}
		dup := duplicates(got)
		if c.late < 2 {
			inOrder++
			if dup > 0 {
				inOrderWithDup++
			}
			continue
		}
		late++
		if dup > 0 {
			lateWithDup++
		}
	}
	// The perturbation happened: cases of both kinds were generated.
	if inOrder == 0 || late == 0 {
		t.Fatalf("generator: %d in order, %d late", inOrder, late)
	}
	if inOrderWithDup != 0 {
		t.Fatalf("%d of %d in-order cases delivered a number twice", inOrderWithDup, inOrder)
	}
	if lateWithDup != late {
		t.Fatalf("KNOWN DEFECT fixed? %d of %d late cases delivered a number twice", lateWithDup, late)
	}
	t.Logf("%d of %d cases with a late packet before the switch delivered a number twice; %d in order, none", lateWithDup, late, inOrder)
}
