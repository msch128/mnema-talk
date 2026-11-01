package sfu

import (
	"bytes"
	"testing"
	"time"

	"github.com/pion/rtp"
)

func newHistory(budget int) *nackHistory {
	return &nackHistory{budget: func(float64) int { return budget }}
}

func addPacket(h *nackHistory, seq uint16, size int, now time.Time) {
	h.add(&rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 3000}, bytes.Repeat([]byte{byte(seq)}, size), now)
}

func has(h *nackHistory, seq uint16) bool {
	_, _, ok := h.find(seq, time.Unix(1<<40, 0), 0)
	return ok
}

// With the ring full by count the history keeps the newest packets, also
// across the wrap of the sequence numbers.
func TestHistoryKeepsTheNewestPacketsWhenTheRingIsFull(t *testing.T) {
	h := newHistory(100 << 20)
	now := time.Unix(1, 0)
	for i := 0; i < 3000; i++ {
		last := uint16((65000 + i) % 65536)
		addPacket(h, last, 100, now)
		if !has(h, last) {
			t.Fatalf("packet %d (the newest) is not retrievable after %d packets", last, i+1)
		}
	}
	last := uint16((65000 + 2999) % 65536)
	for back := 0; back < nackSlots-1; back++ {
		if !has(h, last-uint16(back)) {
			t.Fatalf("the newest %d packets are not all retained: %d missing", nackSlots-1, back)
		}
	}
	if has(h, last-nackSlots) {
		t.Fatal("a packet older than the ring is still found")
	}
}

func TestHistoryDropsTheOldestFirstWhenOverTheByteBudget(t *testing.T) {
	h := newHistory(100 << 10)
	now := time.Unix(1, 0)
	for seq := uint16(0); seq < 200; seq++ {
		addPacket(h, seq, 1000, now)
	}
	if got := h.retained(); got > 100<<10+1500+nackSlotBytes {
		t.Fatalf("%d bytes held over a 100 KB budget", got)
	}
	if !has(h, 199) || !has(h, 190) || has(h, 0) {
		t.Fatal("the budget did not drop the oldest packets")
	}
}

func TestHistoryHandlesJumpsGapsAndLatePackets(t *testing.T) {
	h := newHistory(100 << 20)
	now := time.Unix(1, 0)
	for _, seq := range []uint16{10, 11, 13} {
		addPacket(h, seq, 50, now)
	}
	addPacket(h, 12, 50, now) // late, fills the gap
	addPacket(h, 13, 50, now) // a repeat
	addPacket(h, 5, 50, now)  // older than anything kept
	for _, seq := range []uint16{10, 11, 12, 13} {
		if !has(h, seq) {
			t.Fatalf("packet %d missing", seq)
		}
	}
	if has(h, 5) {
		t.Fatal("a packet older than the range was stored")
	}
	// A small jump keeps the range, a jump past the ring starts over.
	addPacket(h, 600, 50, now)
	if !has(h, 10) || !has(h, 600) {
		t.Fatal("a small jump lost the range")
	}
	addPacket(h, 600+nackSlots+10, 50, now)
	if has(h, 10) || has(h, 600) || !has(h, 600+nackSlots+10) {
		t.Fatal("a jump past the ring kept stale packets")
	}
	// A jump to just within a ring's length drops only what no longer fits.
	addPacket(h, 600+nackSlots+10+nackSlots-1, 50, now)
	if !has(h, 600+nackSlots+10+nackSlots-1) {
		t.Fatal("the newest packet is missing after a near-ring jump")
	}
	h.clear()
	if h.retained() != 0 || has(h, 600+nackSlots+10) {
		t.Fatal("clear left packets")
	}
}

func TestHistoryReturnsTheHeaderAndPayloadAsSent(t *testing.T) {
	h := newHistory(100 << 20)
	hdr := rtp.Header{Version: 2, Padding: true, PaddingSize: 7, Marker: true, PayloadType: 96, SequenceNumber: 9, Timestamp: 1234, SSRC: 77}
	if err := hdr.SetExtension(3, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0xAB}, 700)
	h.add(&hdr, payload, time.Unix(1, 0))
	got, body, ok := h.find(9, time.Unix(2, 0), 0)
	if !ok || got.SSRC != 77 || got.Timestamp != 1234 || !got.Marker || got.PaddingSize != 7 || !bytes.Equal(got.GetExtension(3), []byte{1, 2, 3, 4}) {
		t.Fatalf("header %+v ok=%v", got, ok)
	}
	if !bytes.Equal(body, payload) {
		t.Fatal("payload differs")
	}
	// Changing the copy does not change what is remembered.
	body[0] = 0
	if _, again, _ := h.find(9, time.Unix(3, 0), 0); again[0] != 0xAB {
		t.Fatal("the history shares memory with the copy it gave out")
	}
	// A resend within the interval is refused; forgetting it allows the next.
	if _, _, ok := h.find(9, time.Unix(3, 0), time.Minute); ok {
		t.Fatal("resent within the interval")
	}
	h.forgetResend(9)
	if _, _, ok := h.find(9, time.Unix(3, 0), time.Minute); !ok {
		t.Fatal("forgetResend did not allow another resend")
	}
	h.forgetResend(1234) // unknown: nothing happens
	// Empty payloads and oversized packets.
	h.add(&rtp.Header{Version: 2, SequenceNumber: 10}, nil, time.Unix(1, 0))
	if has(h, 10) {
		t.Fatal("an empty packet was stored")
	}
	big := bytes.Repeat([]byte{1}, 3000)
	addPacket(h, 11, len(big), time.Unix(1, 0))
	if _, body, ok := h.find(11, time.Unix(5, 0), 0); !ok || len(body) != 3000 {
		t.Fatal("an oversized packet was not kept whole")
	}
}

func TestLargeFramesRaiseWhatTheHistoryKeeps(t *testing.T) {
	h := newHistory(nackMinBytes)
	now := time.Unix(1, 0)
	// 40 frames of 200 KB (15 packets of 1400 bytes... times ten) of a screen keyframe size.
	seq := uint16(0)
	for frame := 0; frame < 3; frame++ {
		for i := 0; i < 150; i++ {
			h.add(&rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(frame + 1)}, make([]byte, 1400), now)
			seq++
		}
	}
	if floor := h.frameFloor(); floor <= nackMinBytes || floor > nackMaxFloor {
		t.Fatalf("floor %d for 200 KB frames", floor)
	}
	// The last frame is still all there although the budget alone says 64 KB.
	if !has(h, seq-1) || !has(h, seq-150) {
		t.Fatal("a frame of the size seen before is not fully kept")
	}
}

// Adding a packet in steady state takes no allocation (the buffers are pooled).
func TestAddingAPacketDoesNotAllocateOnceWarm(t *testing.T) {
	h := newHistory(200 << 10)
	now := time.Unix(1, 0)
	payload := make([]byte, 1000)
	var seq uint16
	hdr := rtp.Header{Version: 2, PayloadType: 96, SSRC: 1}
	step := func() {
		hdr.SequenceNumber, hdr.Timestamp = seq, uint32(seq)
		seq++
		h.add(&hdr, payload, now)
	}
	for i := 0; i < 2000; i++ {
		step()
	}
	if allocs := testing.AllocsPerRun(500, step); allocs > 0.1 {
		t.Fatalf("%v allocations per stored packet", allocs)
	}
}

func BenchmarkHistoryAdd(b *testing.B) {
	h := newHistory(200 << 10)
	now := time.Unix(1, 0)
	payload := make([]byte, 1000)
	hdr := rtp.Header{Version: 2, PayloadType: 96, SSRC: 1}
	b.ReportAllocs()
	var seq uint16
	for b.Loop() {
		hdr.SequenceNumber, hdr.Timestamp = seq, uint32(seq)
		seq++
		h.add(&hdr, payload, now)
	}
}
