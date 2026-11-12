package sfu

import (
	"bytes"
	"testing"
	"time"

	"github.com/pion/rtp"
)

// fuzzLayers is the number of layers the simulated share sends.
const fuzzLayers = 3

// fuzzMaxOps bounds a script, so the sequence numbers delivered stay within
// one turn of the 16-bit counter and a number seen twice means a reuse.
const fuzzMaxOps = 4096

// layerSource is the sender's side of one layer: the numbers of its last
// packet. Every layer counts on its own, as separate encoders do.
type layerSource struct {
	seq  uint16
	ts   uint32
	pic  uint16
	tl0  uint8
	sent bool
}

// delivered is one packet the viewer received, with the source numbers it had.
type delivered struct {
	out, src rtp.Header
	pic      uint16
	srcPic   uint16
	tl0      uint8
	srcTl0   uint8
	at       time.Time
}

// heldPacket is a packet of a layer that is late: lost on the way and
// repaired by a retransmission, or overtaken by the packets after it.
type heldPacket struct {
	layer int
	key   bool
	pkt   *rtp.Packet
}

// origin is the source packet an outgoing sequence number was given to.
type origin struct {
	layer int
	seq   uint16
}

// rewriteSim drives one viewer track with packets of several layers and
// checks every packet it lets through against the one before and against
// everything it delivered.
type rewriteSim struct {
	t       *testing.T
	vt      *viewerTrack
	layers  [fuzzLayers]layerSource
	wide    bool
	now     time.Time
	last    *delivered
	arrived *rtp.Packet
	held    []heldPacket
	// owner maps every sequence number delivered to its source packet.
	owner map[uint16]origin
	// The newest sequence number and timestamp delivered.
	newSeq uint16
	newTs  uint32
	// After a switch of a stream that had begun (rebased): the newest numbers
	// delivered before it, and the source number of the keyframe it began at.
	rebased        bool
	floorSeq       uint16
	floorTs        uint32
	curLayer, kSrc int
}

func newRewriteSim(t *testing.T, seq uint16, ts uint32, pic uint16, tl0 uint8, wide bool) *rewriteSim {
	s := &rewriteSim{t: t, wide: wide, now: time.Unix(1_700_000_000, 0), owner: map[uint16]origin{}}
	s.vt = &viewerTrack{group: &layerGroup{kind: codecVP8, source: SourceScreen}, cur: -1, eff: 0}
	s.vt.sink = func(p *rtp.Packet) error {
		cp := *p
		cp.Payload = bytes.Clone(p.Payload)
		s.arrived = &cp
		return nil
	}
	for i := range s.layers {
		// Far apart, so a switch has to bridge a real jump in every counter.
		s.layers[i] = layerSource{
			seq: seq + uint16(i)*0x5555, ts: ts + uint32(i)*0x55555555,
			pic: (pic + uint16(i)*0x2aaa) & s.picMask(), tl0: tl0 + uint8(i)*85,
		}
	}
	return s
}

func (s *rewriteSim) picMask() uint16 {
	if s.wide {
		return 0x7fff
	}
	return 0x7f
}

// step runs one byte of the script. The low two bits pick a layer to send a
// packet of, or (3) another operation chosen by the high four bits: 0 a
// change of what the viewer should receive, 1 to 7 a packet that is late
// (see hold), 8 to 15 the late packets arriving (see release). For a packet
// the high four bits are the milliseconds that pass before it, or (14, 15)
// make its clock reading older than the clock: every layer is forwarded by a
// goroutine of its own, which may have read the clock before another layer's
// packet went out.
func (s *rewriteSim) step(op byte) {
	if op&3 == 3 {
		switch h := op >> 4; {
		case h == 0:
			s.retarget(int(op>>2) & 3)
		case h < 8:
			s.hold(int(op>>2)&3%fuzzLayers, h&4 != 0, h&2 != 0)
		default:
			s.release(h&1 != 0)
		}
		return
	}
	var at time.Time
	switch ms := op >> 4; ms {
	case 15:
		at = s.now.Add(-15 * time.Microsecond)
	case 14:
		at = s.now.Add(-2 * time.Millisecond)
	default:
		s.now = s.now.Add(time.Duration(ms) * time.Millisecond)
		at = s.now
	}
	s.send(int(op&3), op&0x04 != 0, op&0x08 != 0, at)
}

// retarget points the viewer at another layer, or (3) takes the layer it is
// receiving away, as the sender ending that layer does.
func (s *rewriteSim) retarget(to int) {
	if to == fuzzLayers {
		s.vt.layerGone(s.vt.cur)
		return
	}
	s.vt.mu.Lock()
	s.vt.eff = to
	s.vt.mu.Unlock()
}

// next is the sender's next packet of a layer: the start of a keyframe, the
// start of a delta frame or the continuation of the frame before.
func (s *rewriteSim) next(layer int, key, frame bool) *rtp.Packet {
	l := &s.layers[layer]
	start := key || frame || !l.sent
	if start {
		l.ts += 3000
		l.pic = (l.pic + 1) & s.picMask()
		l.tl0++
	}
	l.seq++
	l.sent = true
	payload := vp8Payload(start, key, l.pic, s.wide, l.tl0, byte(layer), byte(l.seq))
	return &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: l.seq, Timestamp: l.ts}, Payload: payload}
}

// hold makes the next packet of a layer late: it does not arrive now, and
// the packets after it do.
func (s *rewriteSim) hold(layer int, key, frame bool) {
	s.held = append(s.held, heldPacket{layer: layer, key: key, pkt: s.next(layer, key, frame)})
}

// release lets the late packets arrive, the newest first; with twice each
// arrives twice, as the original and its retransmission both do.
func (s *rewriteSim) release(twice bool) {
	for i := len(s.held) - 1; i >= 0; i-- {
		h := s.held[i]
		s.late(h)
		if twice {
			s.late(h)
		}
	}
	s.held = s.held[:0]
}

// write hands src to the viewer track and returns whether it switched.
func (s *rewriteSim) write(layer int, src *rtp.Packet, at time.Time) bool {
	sent := bytes.Clone(src.Payload)
	s.arrived = nil
	s.vt.mu.Lock()
	switched, _, _ := s.vt.writeLocked(layer, src, at)
	s.vt.mu.Unlock()
	if !bytes.Equal(src.Payload, sent) {
		s.t.Fatalf("the source packet was modified: % x, was % x", src.Payload, sent)
	}
	return switched
}

// send offers the next packet of a layer, in order. at is the clock reading
// the packet is forwarded with.
func (s *rewriteSim) send(layer int, key, frame bool, at time.Time) {
	src := s.next(layer, key, frame)
	target, before := s.vt.eff, s.vt.cur
	switched := s.write(layer, src, at)
	wantSwitch := layer != before && layer == target && key
	if switched != wantSwitch {
		s.t.Fatalf("layer %d (key %v) with the viewer on %d wanting %d: switched=%v", layer, key, before, target, switched)
	}
	if through := layer == before || wantSwitch; through != (s.arrived != nil) {
		s.t.Fatalf("layer %d with the viewer on %d wanting %d: delivered=%v", layer, before, target, s.arrived != nil)
	}
	if s.arrived != nil {
		s.check(layer, src, switched, at)
	}
}

// late offers a late packet. It switches like any other packet, and of the
// layer being forwarded it goes through unless the source sent it before
// the keyframe the viewer was switched at: then its number would be one the
// layer before already used.
func (s *rewriteSim) late(h heldPacket) {
	src := h.pkt
	target, before := s.vt.eff, s.vt.cur
	switched := s.write(h.layer, src, s.now)
	wantSwitch := h.layer != before && h.layer == target && h.key
	if switched != wantSwitch {
		s.t.Fatalf("late packet of layer %d (key %v) with the viewer on %d wanting %d: switched=%v", h.layer, h.key, before, target, switched)
	}
	stale := s.rebased && h.layer == s.curLayer && int16(src.SequenceNumber-uint16(s.kSrc)) < 0
	if through := wantSwitch || h.layer == before && !stale; through != (s.arrived != nil) {
		s.t.Fatalf("late packet %d of layer %d with the viewer on %d wanting %d (stale %v): delivered=%v",
			src.SequenceNumber, h.layer, before, target, stale, s.arrived != nil)
	}
	if s.arrived != nil {
		s.check(h.layer, src, switched, s.now)
	}
}

// check compares the packet that just arrived with the one before it and
// with everything delivered.
func (s *rewriteSim) check(layer int, src *rtp.Packet, switched bool, at time.Time) {
	t, got := s.t, s.arrived
	if len(got.Payload) != len(src.Payload) {
		t.Fatalf("payload of %d bytes delivered as %d", len(src.Payload), len(got.Payload))
	}
	sd, _ := parseVP8(src.Payload)
	d, ok := parseVP8(got.Payload)
	if !ok || d.start != sd.start || d.key != sd.key || !bytes.Equal(got.Payload[sd.tl0Pos+1:], src.Payload[sd.tl0Pos+1:]) {
		t.Fatalf("delivered % x for % x", got.Payload, src.Payload)
	}
	now := &delivered{out: got.Header, src: src.Header, pic: d.pic, srcPic: sd.pic, tl0: d.tl0, srcTl0: sd.tl0, at: at}
	prev := s.last
	s.last = now
	switch {
	case prev == nil:
		// The first layer is forwarded with the sender's own numbers.
		if now.out.SequenceNumber != now.src.SequenceNumber || now.out.Timestamp != now.src.Timestamp || now.pic != now.srcPic || now.tl0 != now.srcTl0 {
			t.Fatalf("first packet renumbered: %+v", now)
		}
	case switched:
		s.checkSwitch(now, prev)
		s.rebased, s.floorSeq, s.floorTs = true, s.newSeq, s.newTs
	default:
		// Within a layer every counter moves as the sender's does.
		if now.out.SequenceNumber-prev.out.SequenceNumber != now.src.SequenceNumber-prev.src.SequenceNumber ||
			now.out.Timestamp-prev.out.Timestamp != now.src.Timestamp-prev.src.Timestamp ||
			(now.pic-prev.pic)&s.picMask() != (now.srcPic-prev.srcPic)&s.picMask() ||
			now.tl0-prev.tl0 != now.srcTl0-prev.srcTl0 {
			t.Fatalf("numbering drifted within a layer: %+v after %+v", now, prev)
		}
	}
	if switched {
		s.curLayer, s.kSrc = layer, int(src.SequenceNumber)
	}
	s.checkNewer(now)
	s.checkOwner(layer, now)
}

// checkSwitch: a new layer carries on after the newest packet delivered.
func (s *rewriteSim) checkSwitch(now, prev *delivered) {
	t := s.t
	if now.out.SequenceNumber != s.newSeq+1 {
		t.Fatalf("sequence number %d after %d (newest %d) across a switch", now.out.SequenceNumber, prev.out.SequenceNumber, s.newSeq)
	}
	// The timestamp always moves forward from the newest one delivered, by the
	// time that passed since the last packet when the clock readings say that
	// any did.
	ticks := int64(int32(now.out.Timestamp - s.newTs))
	elapsed := max(now.at.Sub(prev.at).Microseconds(), 0) * videoClockRate / 1_000_000
	if ticks < 1 || ticks+1 < elapsed || ticks > elapsed+1 {
		t.Fatalf("timestamp moved %d ticks across a switch, %d elapsed", ticks, elapsed)
	}
	if now.pic != (prev.pic+1)&s.picMask() || now.tl0 != prev.tl0+1 {
		t.Fatalf("picture ID %d after %d, TL0PICIDX %d after %d across a switch", now.pic, prev.pic, now.tl0, prev.tl0)
	}
}

// checkNewer: after a switch every packet is newer, by sequence number and
// timestamp, than everything delivered before the switch, and the newest
// numbers delivered move on.
func (s *rewriteSim) checkNewer(now *delivered) {
	seq, ts := now.out.SequenceNumber, now.out.Timestamp
	if s.rebased && (int16(seq-s.floorSeq) <= 0 || int32(ts-s.floorTs) <= 0) {
		s.t.Fatalf("%d/%d delivered after a switch, not after the newest before it, %d/%d", seq, ts, s.floorSeq, s.floorTs)
	}
	first := len(s.owner) == 0
	if first || int16(seq-s.newSeq) > 0 {
		s.newSeq = seq
	}
	if first || int32(ts-s.newTs) > 0 {
		s.newTs = ts
	}
}

// checkOwner: a sequence number is never delivered for two different source
// packets (the same packet arriving twice keeps its number).
func (s *rewriteSim) checkOwner(layer int, now *delivered) {
	o := origin{layer: layer, seq: now.src.SequenceNumber}
	if had, ok := s.owner[now.out.SequenceNumber]; ok && had != o {
		s.t.Fatalf("sequence number %d delivered for packet %d of layer %d and before for %d of layer %d",
			now.out.SequenceNumber, o.seq, o.layer, had.seq, had.layer)
	}
	s.owner[now.out.SequenceNumber] = o
}

// FuzzViewerLayerRewrite simulates a viewer moved between the layers of a
// share and checks that the stream it receives stays continuous: sequence
// numbers, timestamps, picture IDs and TL0PICIDX carry on across every
// switch, wrap-around included, and only a keyframe of the wanted layer
// starts a new one. Packets also arrive late, overtaken or repaired and
// twice: no sequence number is then given to two packets, and after a switch
// sequence numbers and timestamps are newer than everything delivered before.
func FuzzViewerLayerRewrite(f *testing.F) {
	// Layer 0 from its keyframe, a move to layer 1 that waits for a keyframe,
	// the layer taken away, and back.
	script := []byte{0x04, 0x00, 0x18, 0x07, 0x01, 0x00, 0x15, 0x01, 0x0f, 0x00, 0x05, 0x0b, 0x26, 0x02, 0x03, 0x04, 0x00}
	f.Add(script, uint16(99), uint32(1), uint16(9), uint8(5), true)
	f.Add(script, uint16(0xfffe), uint32(0xfffff000), uint16(0x7ffe), uint8(0xfe), true)
	f.Add(script, uint16(0xaaa9), uint32(0xaaaaa000), uint16(0x7e), uint8(0xa9), false)
	f.Add([]byte{0x05, 0x06, 0x04, 0x07, 0x05, 0x0b, 0x06, 0x03, 0x04}, uint16(0x5554), uint32(0x55554000), uint16(0x5554), uint8(0x54), true)
	// Switches whose keyframe carries a clock reading older than the last
	// packet sent, by a tick and by several frames.
	f.Add([]byte{0x04, 0x07, 0xf5, 0x01, 0x03, 0xe4, 0x10}, uint16(99), uint32(1_450_000), uint16(9), uint8(5), true)
	// Layer 0 loses a packet of a new frame and one more, both repaired after
	// the next packets (the newest repair first), then a switch to layer 2
	// (H-L9-03), across the wrap; and with each repair arriving twice.
	late := []byte{0x04, 0x00, 0x23, 0x13, 0x00, 0x00, 0x83, 0x0b, 0x16, 0x02}
	f.Add(late, uint16(0xfff9), uint32(1), uint16(9), uint8(5), true)
	f.Add(append(bytes.Clone(late[:6]), 0x93, 0x0b, 0x16, 0x02), uint16(99), uint32(0xffffd000), uint16(0x7e), uint8(5), false)
	// A late packet of layer 0 from before its keyframe, after a switch away
	// and back to it: it must not take a number of the stream again.
	f.Add([]byte{0x04, 0x00, 0x13, 0x00, 0x0b, 0x06, 0x03, 0x04, 0x83, 0x00}, uint16(200), uint32(9000), uint16(9), uint8(5), true)
	f.Fuzz(func(t *testing.T, script []byte, seq uint16, ts uint32, pic uint16, tl0 uint8, wide bool) {
		sim := newRewriteSim(t, seq, ts, pic, tl0, wide)
		for _, op := range script[:min(len(script), fuzzMaxOps)] {
			sim.step(op)
		}
	})
}
