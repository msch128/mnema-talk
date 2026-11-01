package sfu

import (
	"sync"
	"time"
	"unsafe"

	"github.com/pion/rtp"
)

// What a subscriber's history of one video track may hold. The window is how
// much time of the track's own bitrate it covers and the byte cap is the share
// of the connection's budget one track gets; both shrink as the server fills
// up, and are looked at on every packet, so a connection that joined early
// does not keep the large history it got when the server was empty.
const (
	// nackSlots is the most packets a track remembers (the ring's largest size);
	// the ring starts at nackFirstSlots and doubles as the track needs it.
	nackSlots      = 1024
	nackFirstSlots = 64
	// nackMinBytes is the least a track remembers whatever the load; a track
	// whose frames are larger (a screen's keyframe is 100 KB or more) keeps
	// room for a frame and a quarter, up to nackMaxFloor.
	nackMinBytes = 64 << 10
	nackMaxFloor = 768 << 10
	// nackStartBytes is the budget before the track's bitrate is known.
	nackStartBytes = 256 << 10
	// nackRateEvery is how often the track's bitrate is measured.
	nackRateEvery = 500 * time.Millisecond
	// nackFrameDecay forgets the size of the largest frame slowly (per frame).
	nackFrameDecay = 0.999
)

// nackWindow is how long a track's history reaches back with peers connections
// on the server.
func nackWindow(peers int64) time.Duration {
	switch {
	case peers <= 16:
		return time.Second
	case peers <= 32:
		return 750 * time.Millisecond
	case peers <= 64:
		return 500 * time.Millisecond
	default:
		return 250 * time.Millisecond
	}
}

// nackConnBytes is the history one subscriber connection may hold in all its
// video tracks together with peers connections on the server.
func nackConnBytes(peers int64) int {
	switch {
	case peers <= 16:
		return 4 << 20
	case peers <= 32:
		return 2 << 20
	case peers <= 64:
		return 1 << 20
	default:
		return 512 << 10
	}
}

// nackBuf is a pooled buffer: the stored header followed by the payload.
type nackBuf struct{ b []byte }

// Buffers come in four sizes, so a 120 byte packet does not hold 1.5 KB.
var nackPools = [...]struct {
	size int
	pool sync.Pool
}{
	{size: 256}, {size: 640}, {size: 1500}, {size: 2048},
}

// getNackBuf returns a buffer with room for n bytes.
func getNackBuf(n int) *nackBuf {
	for i := range nackPools {
		if n <= nackPools[i].size {
			if b, ok := nackPools[i].pool.Get().(*nackBuf); ok {
				return b
			}
			return &nackBuf{b: make([]byte, 0, nackPools[i].size)}
		}
	}
	return &nackBuf{b: make([]byte, 0, n)}
}

func putNackBuf(b *nackBuf) {
	for i := range nackPools {
		if cap(b.b) == nackPools[i].size {
			b.b = b.b[:0]
			nackPools[i].pool.Put(b)
			return
		}
	}
}

// nackSlot is one remembered packet (24 bytes).
type nackSlot struct {
	buf *nackBuf
	// resentAt is when it was last resent (unix nanoseconds, 0 = never).
	resentAt int64
	// size is the bytes it holds, buffer capacity and slot included.
	size   int32
	seq    uint16
	hdrLen uint8
	// padding is the header's padding size.
	padding uint8
}

// nackSlotBytes is what a slot of the ring takes.
const nackSlotBytes = int(unsafe.Sizeof(nackSlot{}))

// nackHistory is the remembered packets of one video track of one subscriber.
// The retained range is [oldest, highest], less than len(ring) long; slot
// seq%len(ring) holds seq. The ring grows with the track, up to nackSlots.
type nackHistory struct {
	mu      sync.Mutex
	ring    []nackSlot
	started bool
	// oldest and highest are the sequence numbers of the retained range.
	oldest, highest uint16
	bytes           int
	// budget is the bytes the history may hold now, given the track's bitrate.
	budget func(rate float64) int

	// The bitrate of the track, in bytes per second, measured every nackRateEvery.
	rate     float64
	rateAt   time.Time
	rateSeen int
	sent     int
	// frame is the bytes of the frame being sent (packets of one RTP
	// timestamp) and frameMax the largest recently.
	frame, frameMax float64
	frameTs         uint32
}

// add remembers a packet and drops what no longer fits.
func (h *nackHistory) add(header *rtp.Header, payload []byte, now time.Time) {
	if len(payload) == 0 {
		return
	}
	need := header.MarshalSize() + len(payload)
	buf := getNackBuf(need)
	hdrLen, err := header.MarshalTo(buf.b[:need])
	if err != nil || hdrLen > 255 {
		putNackBuf(buf)
		return
	}
	buf.b = append(buf.b[:hdrLen], payload...)
	slot := nackSlot{
		buf: buf, seq: header.SequenceNumber, hdrLen: uint8(hdrLen),
		padding: header.PaddingSize, size: int32(cap(buf.b) + nackSlotBytes),
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.measure(now, need, header.Timestamp)
	if !h.store(slot) {
		putNackBuf(buf)
		return
	}
	h.trim(max(h.budget(h.rate), h.frameFloor()))
}

// store places the packet in the ring; false when it is not kept (a repeat,
// or older than anything retained).
func (h *nackHistory) store(slot nackSlot) bool {
	seq := slot.seq
	if !h.started {
		h.started, h.oldest, h.highest = true, seq, seq
		h.grow(1)
		h.put(slot)
		return true
	}
	if back := h.highest - seq; back < 1<<15 {
		// Not newer than the newest: kept when it fills a gap in the range.
		if back == 0 || back >= nackSlots || seq-h.oldest >= 1<<15 || h.ring[int(seq)%len(h.ring)].buf != nil {
			return false
		}
		h.put(slot)
		return true
	}
	h.makeRoomFor(seq)
	h.highest = seq
	h.put(slot)
	return true
}

// makeRoomFor readies the ring for the newer packet seq: it grows, and the
// oldest packets go when seq would not fit. The new packet is never the one dropped.
func (h *nackHistory) makeRoomFor(seq uint16) {
	ahead := seq - h.oldest
	if ahead < nackSlots {
		h.grow(int(ahead) + 1)
		return
	}
	h.grow(nackSlots)
	if seq-h.highest >= nackSlots {
		h.clearRing()
		h.oldest = seq
		return
	}
	for seq-h.oldest >= nackSlots {
		h.dropOldest()
	}
}

// grow makes the ring at least span slots (a power of two, up to nackSlots),
// keeping what it holds.
func (h *nackHistory) grow(span int) {
	n := max(len(h.ring), nackFirstSlots)
	for n < span && n < nackSlots {
		n *= 2
	}
	if n == len(h.ring) {
		return
	}
	old := h.ring
	h.ring = make([]nackSlot, n)
	for i := range old {
		if old[i].buf != nil {
			h.ring[int(old[i].seq)%n] = old[i]
		}
	}
}

// measure counts size bytes sent and updates the bitrate every nackRateEvery
// and the size of the largest frame.
func (h *nackHistory) measure(now time.Time, size int, ts uint32) {
	h.sent += size
	if ts != h.frameTs {
		h.frameMax = max(h.frame, h.frameMax*nackFrameDecay)
		h.frame, h.frameTs = 0, ts
	}
	h.frame += float64(size)
	if h.rateAt.IsZero() {
		h.rateAt, h.rateSeen = now, h.sent
		return
	}
	if dt := now.Sub(h.rateAt); dt >= nackRateEvery {
		inst := float64(h.sent-h.rateSeen) / dt.Seconds()
		if h.rate == 0 {
			h.rate = inst
		} else {
			h.rate = (h.rate + inst) / 2
		}
		h.rateAt, h.rateSeen = now, h.sent
	}
}

// frameFloor is the least the history keeps: a frame and a quarter of the
// largest recent frame, within [nackMinBytes, nackMaxFloor].
func (h *nackHistory) frameFloor() int {
	return max(nackMinBytes, min(int(h.frameMax*1.25), nackMaxFloor))
}

func (h *nackHistory) put(slot nackSlot) {
	h.ring[int(slot.seq)%len(h.ring)] = slot
	h.bytes += int(slot.size)
}

// total is what the history takes: its buffers and its ring.
func (h *nackHistory) total() int {
	return h.bytes + len(h.ring)*nackSlotBytes
}

// release forgets the packet in slot idx.
func (h *nackHistory) release(idx int) {
	if s := &h.ring[idx]; s.buf != nil {
		h.bytes -= int(s.size)
		putNackBuf(s.buf)
		*s = nackSlot{}
	}
}

// dropOldest forgets the oldest packet and moves the range on.
func (h *nackHistory) dropOldest() {
	h.release(int(h.oldest) % len(h.ring))
	h.oldest++
}

// trim forgets the oldest packets until the history fits max bytes.
func (h *nackHistory) trim(max int) {
	for h.oldest != h.highest && h.total() > max {
		h.dropOldest()
	}
}

func (h *nackHistory) clearRing() {
	for i := range h.ring {
		h.release(i)
	}
}

// find returns a copy of packet seq for resending, or false when it is not
// remembered or was resent within interval. The copy is the caller's: the
// history goes on without it.
func (h *nackHistory) find(seq uint16, now time.Time, interval time.Duration) (rtp.Header, []byte, bool) {
	h.mu.Lock()
	s := h.slot(seq)
	if s == nil || !h.mayResend(s, now, interval) {
		h.mu.Unlock()
		return rtp.Header{}, nil, false
	}
	s.resentAt = now.UnixNano()
	raw := append([]byte(nil), s.buf.b...)
	hdrLen, pad := int(s.hdrLen), s.padding
	h.mu.Unlock()
	var header rtp.Header
	if _, err := header.Unmarshal(raw[:hdrLen]); err != nil {
		return rtp.Header{}, nil, false
	}
	header.PaddingSize = pad
	return header, raw[hdrLen:], true
}

// slot is the slot holding packet seq of the retained range, nil if none does.
func (h *nackHistory) slot(seq uint16) *nackSlot {
	if !h.started || h.highest-seq >= 1<<15 || seq-h.oldest >= 1<<15 {
		return nil
	}
	if s := &h.ring[int(seq)%len(h.ring)]; s.buf != nil && s.seq == seq {
		return s
	}
	return nil
}

// mayResend reports whether slot s was not resent within interval.
func (h *nackHistory) mayResend(s *nackSlot, now time.Time, interval time.Duration) bool {
	return s.resentAt == 0 || now.UnixNano()-s.resentAt >= int64(interval)
}

// size is the bytes packet seq takes on the wire if it is remembered and may
// be resent now, without copying it.
func (h *nackHistory) size(seq uint16, now time.Time, interval time.Duration) (int, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.slot(seq)
	if s == nil || !h.mayResend(s, now, interval) {
		return 0, false
	}
	return len(s.buf.b), true
}

// bitrate is the track's measured bitrate in bytes per second.
func (h *nackHistory) bitrate() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rate
}

// forgetResend allows seq to be resent at once again (its resend was not sent).
func (h *nackHistory) forgetResend(seq uint16) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s := h.slot(seq); s != nil {
		s.resentAt = 0
	}
}

// retained is the bytes the history holds now.
func (h *nackHistory) retained() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.total()
}

// clear releases everything.
func (h *nackHistory) clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clearRing()
	h.ring = nil
	h.started, h.bytes = false, 0
}
