package main

import (
	"testing"
)

// isKey reads what the SFU reads: the descriptor of the first packet of a
// frame (S bit, partition 0) and bit 0 of the frame tag after it.
func isKey(payload []byte) (start, key bool) {
	start = payload[0]&0x10 != 0 && payload[0]&0x07 == 0
	return start, start && payload[1]&0x01 == 0
}

func TestFramesStartWithAKeyframeThenInterframesUntilTheNextKeyframe(t *testing.T) {
	s := newVP8Source(1000, 10, 2) // a keyframe every 20 frames
	var keys, frames int
	for range 45 {
		pkts := s.nextFrame()
		start, key := isKey(pkts[0].Payload)
		if !start {
			t.Fatal("first packet of a frame has no start bit")
		}
		for _, p := range pkts[1:] {
			if st, _ := isKey(p.Payload); st {
				t.Fatal("a continuation packet carries the start bit")
			}
		}
		if !pkts[len(pkts)-1].Marker {
			t.Fatal("last packet has no marker")
		}
		for _, p := range pkts[:len(pkts)-1] {
			if p.Marker {
				t.Fatal("marker before the last packet")
			}
		}
		frames++
		if key {
			keys++
			if (frames-1)%20 != 0 {
				t.Fatalf("keyframe at frame %d", frames-1)
			}
		}
	}
	if keys != 3 { // frames 0, 20 and 40
		t.Fatalf("keyframes = %d", keys)
	}
}

func TestBitrateAveragesToTheTargetOverAKeyframeCycle(t *testing.T) {
	const kbps, fps, keySeconds = 1500, 15, 3
	s := newVP8Source(kbps, fps, keySeconds)
	bytes := 0
	for range fps * keySeconds * 4 {
		for _, p := range s.nextFrame() {
			bytes += len(p.Payload)
		}
	}
	got := float64(bytes) * 8 / float64(keySeconds*4) / 1000
	if got < kbps*0.95 || got > kbps*1.1 {
		t.Fatalf("average %.0f kbit/s, want about %d", got, kbps)
	}
}

func TestSequenceNumbersAreContinuousAndTimestampsStepPerFrame(t *testing.T) {
	s := newVP8Source(800, 20, 1)
	var prevSeq uint16
	var frameTS []uint32
	first := true
	for range 30 {
		pkts := s.nextFrame()
		frameTS = append(frameTS, pkts[0].Timestamp)
		for _, p := range pkts {
			if p.Timestamp != pkts[0].Timestamp {
				t.Fatal("packets of one frame differ in timestamp")
			}
			if !first && p.SequenceNumber != prevSeq+1 {
				t.Fatalf("sequence jumped from %d to %d", prevSeq, p.SequenceNumber)
			}
			prevSeq, first = p.SequenceNumber, false
		}
	}
	for i := 1; i < len(frameTS); i++ {
		if frameTS[i]-frameTS[i-1] != videoClock/20 {
			t.Fatalf("timestamp step %d", frameTS[i]-frameTS[i-1])
		}
	}
}

func TestKeyframeRequestMakesTheNextFrameAKeyframe(t *testing.T) {
	s := newVP8Source(500, 30, 10)
	s.nextFrame()
	s.nextFrame()
	if _, key := isKey(s.nextFrame()[0].Payload); key {
		t.Fatal("an interframe was a keyframe")
	}
	s.requestKeyframe()
	if _, key := isKey(s.nextFrame()[0].Payload); !key {
		t.Fatal("the frame after a request is no keyframe")
	}
}

func TestPacketsStayBelowTheMTU(t *testing.T) {
	s := newVP8Source(12000, 30, 1)
	for range 60 {
		for _, p := range s.nextFrame() {
			if len(p.Payload) > maxVideoPayload {
				t.Fatalf("payload of %d bytes", len(p.Payload))
			}
		}
	}
}

func TestOpusPacketsStepByTwentyMilliseconds(t *testing.T) {
	s := newOpusSource()
	a, b := s.next(), s.next()
	if b.SequenceNumber != a.SequenceNumber+1 || b.Timestamp-a.Timestamp != 960 || len(a.Payload) != opusBytes {
		t.Fatalf("packets %+v %+v", a.Header, b.Header)
	}
}
