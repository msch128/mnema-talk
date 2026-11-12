package sfu

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/pion/rtp"
)

// Numbers across a layer switch. A viewer of a layered video gets one
// stream: switchLocked makes the new layer's sequence numbers continue after
// the newest packet sent. When the last packet forwarded arrived late
// (reordered on the way, or repaired after a loss) it is not the newest, and
// continuing after it gave the new layer's first packets numbers the viewer
// holds for other packets (lane 9, H-L9-03). A receiver drops a packet whose
// number it has, so the new layer's keyframe arrived incomplete.

// switchCase is one generated stream: layer 0 from start, `late` of its last
// packets delivered in reverse (0 = in order), then a switch to layer 2.
type switchCase struct {
	start uint16
	run   int
	late  int
}

// deliverSwitch plays c and returns the packets the viewer got.
func deliverSwitch(t *testing.T, c switchCase) []rtp.Packet {
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
	return out.take()
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

// newestSent is the newest sequence number and timestamp of pkts.
func newestSent(pkts []rtp.Packet) (uint16, uint32) {
	seq, ts := pkts[0].SequenceNumber, pkts[0].Timestamp
	for _, p := range pkts[1:] {
		if int16(p.SequenceNumber-seq) > 0 {
			seq = p.SequenceNumber
		}
		if int32(p.Timestamp-ts) > 0 {
			ts = p.Timestamp
		}
	}
	return seq, ts
}

// Whether or not the last packet before the switch was late, no sequence
// number is delivered twice, and the new layer's packets carry numbers and
// timestamps newer than everything sent before.
func TestLatePacketBeforeASwitchReusesNoSequenceNumbers(t *testing.T) {
	var inOrder, late int
	for _, c := range generateSwitchCases(400) {
		got := deliverSwitch(t, c)
		if len(got) != c.run+3 {
			t.Fatalf("%+v: delivered %d packets, want %d", c, len(got), c.run+3)
		}
		if dup := duplicates(seqs(got)); dup > 0 {
			t.Fatalf("%+v: %d numbers delivered twice: %v", c, dup, seqs(got))
		}
		seq, ts := newestSent(got[:c.run])
		for _, p := range got[c.run:] {
			if int16(p.SequenceNumber-seq) <= 0 || int32(p.Timestamp-ts) <= 0 {
				t.Fatalf("%+v: the new layer's %d/%d is not after the newest sent, %d/%d", c, p.SequenceNumber, p.Timestamp, seq, ts)
			}
		}
		checkPictureIDs(t, c, got)
		if c.late < 2 {
			inOrder++
		} else {
			late++
		}
	}
	// The perturbation happened: cases of both kinds were generated.
	if inOrder == 0 || late == 0 {
		t.Fatalf("generator: %d in order, %d late", inOrder, late)
	}
	t.Logf("%d cases with a late packet before the switch, %d in order: no number delivered twice", late, inOrder)
}

// checkPictureIDs: the VP8 picture IDs of the new layer's frames (15 bits
// here) are none of the frames delivered before the switch and come after
// the newest of them, and its TL0PICIDX after the newest one (all of layer 0
// share one).
func checkPictureIDs(t *testing.T, c switchCase, got []rtp.Packet) {
	t.Helper()
	before := map[uint16]bool{}
	var newPic uint16
	var newTl0 uint8
	for i, p := range got[:c.run] {
		d, _ := parseVP8(p.Payload)
		before[d.pic] = true
		if i == 0 || (d.pic-newPic)&0x7fff != 0 && (d.pic-newPic)&0x7fff < 1<<14 {
			newPic = d.pic
		}
		if i == 0 || int8(d.tl0-newTl0) > 0 {
			newTl0 = d.tl0
		}
	}
	for _, p := range got[c.run:] {
		d, _ := parseVP8(p.Payload)
		if ahead := (d.pic - newPic) & 0x7fff; before[d.pic] || ahead == 0 || ahead >= 1<<14 || int8(d.tl0-newTl0) <= 0 {
			t.Fatalf("%+v: the new layer's picture ID %d / TL0PICIDX %d after the newest %d / %d (before: %v)", c, d.pic, d.tl0, newPic, newTl0, before)
		}
	}
}

// A switch marks the newest number before it, so that a late packet of the
// new layer from before its keyframe is dropped; the mark retires once the
// stream has moved on, before the 16-bit comparison with it turns round
// (32768 numbers on). Every packet of a long run after a switch is delivered.
func TestAStreamFlowsLongAfterASwitch(t *testing.T) {
	s := newRewriteSim(t, 65000, 1, 9, 5, true)
	s.send(0, true, false, s.now)
	s.retarget(1)
	s.hold(1, false, true) // sent by layer 1 before its keyframe, arrives late
	s.send(1, true, false, s.now)
	for range 100 {
		s.send(1, false, true, s.now)
	}
	s.release(false) // late() requires it dropped while the mark holds
	if s.arrived != nil {
		t.Fatal("a late packet from before the keyframe of the switch was delivered")
	}
	delivered := 0
	for i := range 40000 {
		s.now = s.now.Add(time.Millisecond)
		s.send(1, false, i%8 == 0, s.now)
		if s.arrived != nil {
			delivered++
		}
	}
	if delivered != 40000 {
		t.Fatalf("%d of 40000 packets after a switch delivered", delivered)
	}
}
