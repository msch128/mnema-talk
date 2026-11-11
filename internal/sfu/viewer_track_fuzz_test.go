package sfu

import (
	"bytes"
	"testing"
	"time"

	"github.com/pion/rtp"
)

// fuzzLayers is the number of layers the simulated share sends.
const fuzzLayers = 3

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

// rewriteSim drives one viewer track with packets of several layers and
// checks every packet it lets through against the one before.
type rewriteSim struct {
	t       *testing.T
	vt      *viewerTrack
	layers  [fuzzLayers]layerSource
	wide    bool
	now     time.Time
	last    *delivered
	arrived *rtp.Packet
}

func newRewriteSim(t *testing.T, seq uint16, ts uint32, pic uint16, tl0 uint8, wide bool) *rewriteSim {
	s := &rewriteSim{t: t, wide: wide, now: time.Unix(1_700_000_000, 0)}
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
// packet of, or (3) a change of what the viewer should receive. The high four
// bits are the milliseconds that pass before the packet, or (14, 15) make its
// clock reading older than the clock: every layer is forwarded by a goroutine
// of its own, which may have read the clock before another layer's packet
// went out.
func (s *rewriteSim) step(op byte) {
	if op&3 == 3 {
		s.retarget(int(op>>2) & 3)
		return
	}
	at := s.now
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

// send offers the next packet of a layer: the start of a keyframe, the start
// of a delta frame or the continuation of the frame before. at is the clock
// reading the packet is forwarded with.
func (s *rewriteSim) send(layer int, key, frame bool, at time.Time) {
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
	src := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: l.seq, Timestamp: l.ts}, Payload: payload}
	sent := bytes.Clone(payload)

	target, before := s.vt.eff, s.vt.cur
	s.arrived = nil
	s.vt.mu.Lock()
	switched, _, _ := s.vt.writeLocked(layer, src, at)
	s.vt.mu.Unlock()

	if !bytes.Equal(payload, sent) {
		s.t.Fatalf("the source packet was modified: % x, was % x", payload, sent)
	}
	wantSwitch := layer != before && layer == target && key
	if switched != wantSwitch {
		s.t.Fatalf("layer %d (key %v) with the viewer on %d wanting %d: switched=%v", layer, key, before, target, switched)
	}
	if through := layer == before || wantSwitch; through != (s.arrived != nil) {
		s.t.Fatalf("layer %d with the viewer on %d wanting %d: delivered=%v", layer, before, target, s.arrived != nil)
	}
	if s.arrived != nil {
		s.check(src, switched, at)
	}
}

// check compares the packet that just arrived with the one before it.
func (s *rewriteSim) check(src *rtp.Packet, switched bool, at time.Time) {
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
		// A new layer carries on where the last packet left off.
		if now.out.SequenceNumber != prev.out.SequenceNumber+1 {
			t.Fatalf("sequence number %d after %d across a switch", now.out.SequenceNumber, prev.out.SequenceNumber)
		}
		// The timestamp always moves forward, by the time that passed when the
		// clock readings say that any did.
		ticks := int64(int32(now.out.Timestamp - prev.out.Timestamp))
		elapsed := max(now.at.Sub(prev.at).Microseconds(), 0) * videoClockRate / 1_000_000
		if ticks < 1 || ticks+1 < elapsed || ticks > elapsed+1 {
			t.Fatalf("timestamp moved %d ticks across a switch, %d elapsed", ticks, elapsed)
		}
		if now.pic != (prev.pic+1)&s.picMask() || now.tl0 != prev.tl0+1 {
			t.Fatalf("picture ID %d after %d, TL0PICIDX %d after %d across a switch", now.pic, prev.pic, now.tl0, prev.tl0)
		}
	default:
		// Within a layer every counter moves as the sender's does.
		if now.out.SequenceNumber-prev.out.SequenceNumber != now.src.SequenceNumber-prev.src.SequenceNumber ||
			now.out.Timestamp-prev.out.Timestamp != now.src.Timestamp-prev.src.Timestamp ||
			(now.pic-prev.pic)&s.picMask() != (now.srcPic-prev.srcPic)&s.picMask() ||
			now.tl0-prev.tl0 != now.srcTl0-prev.srcTl0 {
			t.Fatalf("numbering drifted within a layer: %+v after %+v", now, prev)
		}
	}
}

// FuzzViewerLayerRewrite simulates a viewer moved between the layers of a
// share and checks that the stream it receives stays continuous: sequence
// numbers, timestamps, picture IDs and TL0PICIDX carry on across every
// switch, wrap-around included, and only a keyframe of the wanted layer
// starts a new one.
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
	f.Fuzz(func(t *testing.T, script []byte, seq uint16, ts uint32, pic uint16, tl0 uint8, wide bool) {
		sim := newRewriteSim(t, seq, ts, pic, tl0, wide)
		for _, op := range script {
			sim.step(op)
		}
	})
}
