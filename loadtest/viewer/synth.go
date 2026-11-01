package main

import (
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
)

// Synthetic media for the headless publishers. The SFU forwards payloads
// without decoding them (the browsers seal them end to end), and reads only
// the RTP payload descriptor: VP8's tells where a frame starts and whether it
// is a keyframe. So a frame here is a correct descriptor, a frame tag with the
// right keyframe bit, and random bytes of the size a real encoder would
// produce at the configured bitrate.

const (
	// videoClock is the RTP clock of video.
	videoClock = 90000
	// maxVideoPayload keeps a packet below a typical path MTU with its headers.
	maxVideoPayload = 1100
	// keyframeFactor is how many times larger a keyframe is than an
	// interframe, a typical ratio for screen content.
	keyframeFactor = 4
	// opusBytes is the payload of one 20 ms Opus frame at about 32 kbit/s.
	opusBytes   = 80
	opusFrameMs = 20
	opusClock   = 48000
)

// vp8Source produces the packets of a VP8 stream at a bitrate and frame rate.
type vp8Source struct {
	fps        int
	keyEvery   int // frames between keyframes
	interBytes int
	keyBytes   int
	seq        uint16
	ts         uint32
	frame      int
	keyRequest atomic.Bool
	noise      []byte
}

// newVP8Source plans a stream of kbps kbit/s on average with a keyframe every
// keySeconds. The average holds because the interframes are sized for what the
// keyframes leave over.
func newVP8Source(kbps, fps, keySeconds int) *vp8Source {
	fps = max(fps, 1)
	keyEvery := max(fps*max(keySeconds, 1), 1)
	perSecond := kbps * 1000 / 8
	// One cycle: 1 keyframe of keyframeFactor units and keyEvery-1 interframes of 1.
	units := keyEvery - 1 + keyframeFactor
	inter := max(perSecond*keyEvery/fps/units, 8)
	noise := make([]byte, inter*keyframeFactor+maxVideoPayload)
	for i := range noise {
		noise[i] = byte(rand.UintN(256))
	}
	s := &vp8Source{
		fps: fps, keyEvery: keyEvery, interBytes: inter, keyBytes: inter * keyframeFactor,
		seq: uint16(rand.UintN(1 << 16)), ts: rand.Uint32(), noise: noise,
	}
	s.keyRequest.Store(true)
	return s
}

// requestKeyframe makes the next frame a keyframe, as a PLI asks.
func (s *vp8Source) requestKeyframe() { s.keyRequest.Store(true) }

// nextFrame returns the packets of the next frame.
func (s *vp8Source) nextFrame() []*rtp.Packet {
	key := s.keyRequest.Swap(false) || s.frame%s.keyEvery == 0
	size := s.interBytes
	if key {
		size = s.keyBytes
		s.frame = 0
	}
	s.frame++
	pkts := s.packetize(size, key)
	s.ts += uint32(videoClock / s.fps)
	return pkts
}

// packetize splits one frame into packets (RFC 7741 descriptor: S bit on the
// first, partition 0), with the marker bit on the last.
func (s *vp8Source) packetize(size int, key bool) []*rtp.Packet {
	var pkts []*rtp.Packet
	for sent := 0; sent < size; {
		n := min(size-sent, maxVideoPayload-1)
		payload := make([]byte, 1+n)
		copy(payload[1:], s.noise[sent%len(s.noise):])
		if sent == 0 {
			payload[0] = 0x10 // S=1, PID=0
			// The first payload byte is the frame tag: bit 0 clear for a keyframe.
			if key {
				payload[1] &^= 0x01
			} else {
				payload[1] |= 0x01
			}
		}
		sent += n
		pkts = append(pkts, &rtp.Packet{
			Header:  rtp.Header{Version: 2, SequenceNumber: s.seq, Timestamp: s.ts, Marker: sent >= size},
			Payload: payload,
		})
		s.seq++
	}
	return pkts
}

// frameInterval is the time between frames.
func (s *vp8Source) frameInterval() time.Duration { return time.Second / time.Duration(s.fps) }

// dtxPlan makes a speaker talk for a share of a cycle and be silent for the rest,
// the way Opus with DTX (usedtx=1) sends: 20 ms frames while talking, and in
// the silence no packet but a one-byte comfort-noise frame every 400 ms. A zero
// plan is the old speaker who never stops.
type dtxPlan struct {
	cycleTicks int // 20 ms ticks in one talk-and-silence cycle
	talkTicks  int // how many of them are talking
	offset     int // where in the cycle this speaker starts
}

const (
	dtxCycle     = 10 * time.Second
	dtxSilenceMs = 400 // the comfort-noise frame interval of Opus DTX
)

// newDTXPlan is the plan of a speaker who talks talkPct percent of the time.
func newDTXPlan(talkPct int) dtxPlan {
	cycle := int(dtxCycle / (opusFrameMs * time.Millisecond))
	talk := min(max(talkPct, 0), 100) * cycle / 100
	return dtxPlan{cycleTicks: cycle, talkTicks: talk, offset: int(rand.UintN(uint(cycle)))}
}

func (d dtxPlan) talking(tick int) bool {
	return d.cycleTicks == 0 || (tick+d.offset)%d.cycleTicks < d.talkTicks
}

// opusSource produces 20 ms Opus-sized packets, or with a plan the packets of
// a speaker whose encoder uses DTX.
type opusSource struct {
	seq      uint16
	ts       uint32
	payload  []byte
	plan     dtxPlan
	tick     int
	silent   int  // ticks since the last comfort-noise frame
	wasQuiet bool // the last tick was silence: speech resumes with the marker bit
}

func newOpusSource(plan dtxPlan) *opusSource {
	p := make([]byte, opusBytes)
	for i := range p {
		p[i] = byte(rand.UintN(256))
	}
	return &opusSource{seq: uint16(rand.UintN(1 << 16)), ts: rand.Uint32(), payload: p, plan: plan}
}

// next is the packet of the next 20 ms tick, or nil when DTX sends nothing.
// The timestamp runs on with every tick, so a silence shows as a gap in it
// while the sequence numbers stay consecutive.
func (s *opusSource) next() *rtp.Packet {
	talking := s.plan.talking(s.tick)
	s.tick++
	var p *rtp.Packet
	switch {
	case talking:
		p = &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: s.seq, Timestamp: s.ts, Marker: s.wasQuiet}, Payload: s.payload}
		s.silent = 0
	case s.silent == 0 || s.silent*opusFrameMs >= dtxSilenceMs:
		p = &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: s.seq, Timestamp: s.ts}, Payload: []byte{0xf8}}
		s.silent = 1
	default:
		s.silent++
	}
	s.wasQuiet = !talking
	if p != nil {
		s.seq++
	}
	s.ts += opusClock * opusFrameMs / 1000
	return p
}
