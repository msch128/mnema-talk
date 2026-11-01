package sfu

import (
	"sync"
	"time"

	"github.com/pion/rtp"
)

// What a subscriber's history of one video track may hold. The window is how
// much time of the track's own bitrate it covers and the byte cap is the share
// of the connection's budget one track gets; both shrink as the server fills
// up, and are looked at on every packet, so a connection that joined early
// does not keep the large history it got when the server was empty.
const (
	// nackSlots is the most packets a track remembers (the ring's size).
	nackSlots = 1024
	// nackMinBytes is the least a track remembers, enough for a keyframe
	// (40 or more packets) to be answered whatever the load.
	nackMinBytes = 64 << 10
	// nackStartBytes is the budget before the track's bitrate is known.
	nackStartBytes = 256 << 10
	// nackRateEvery is how often the track's bitrate is measured.
	nackRateEvery = 500 * time.Millisecond
	// nackPacketOverhead is counted on top of the payload of a stored packet
	// (its header and the bookkeeping).
	nackPacketOverhead = 64
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

// Payload buffers come in three sizes, so a 120 byte packet does not hold 1.5 KB.
var nackPools = [...]struct {
	size int
	pool sync.Pool
}{
	{size: 256}, {size: 640}, {size: 1500},
}

func nackBuffer(n int) []byte {
	for i := range nackPools {
		if n <= nackPools[i].size {
			if b, ok := nackPools[i].pool.Get().(*[]byte); ok {
				return (*b)[:n]
			}
			return make([]byte, n, nackPools[i].size)
		}
	}
	return make([]byte, n)
}

func nackRecycle(b []byte) {
	for i := range nackPools {
		if cap(b) == nackPools[i].size {
			b = b[:0]
			nackPools[i].pool.Put(&b)
			return
		}
	}
}

// nackEntry is one remembered packet.
type nackEntry struct {
	header     rtp.Header
	payload    []byte
	resentAt   time.Time
	storedSize int
}

// nackHistory is the remembered packets of one video track of one subscriber.
type nackHistory struct {
	mu      sync.Mutex
	ring    [nackSlots]*nackEntry
	started bool
	// oldest and highest are the sequence numbers of the retained range.
	oldest, highest uint16
	bytes           int

	// The bitrate of the track, in bytes per second, measured every nackRateEvery.
	rate     float64
	rateAt   time.Time
	rateSeen int
	sent     int
}

// add remembers a packet and drops what no longer fits.
func (h *nackHistory) add(header *rtp.Header, payload []byte, now time.Time, budget func(rate float64) int) {
	if len(payload) == 0 {
		return
	}
	seq := header.SequenceNumber
	e := &nackEntry{header: header.Clone(), payload: append(nackBuffer(len(payload))[:0], payload...)}
	e.storedSize = len(payload) + nackPacketOverhead
	h.mu.Lock()
	defer h.mu.Unlock()
	h.measure(now, e.storedSize)
	if !h.started {
		h.started, h.oldest, h.highest = true, seq, seq
	} else if diff := seq - h.highest; diff == 0 || diff >= 1<<15 {
		// A repeat or an old packet; the numbers are not going back.
		nackRecycle(e.payload)
		return
	}
	h.highest = seq
	h.put(seq, e)
	h.trim(budget(h.rate))
}

// measure counts size bytes sent and updates the bitrate every nackRateEvery.
func (h *nackHistory) measure(now time.Time, size int) {
	h.sent += size
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

func (h *nackHistory) put(seq uint16, e *nackEntry) {
	idx := int(seq) % nackSlots
	if old := h.ring[idx]; old != nil {
		h.drop(idx, old)
	}
	h.ring[idx] = e
	h.bytes += e.storedSize
}

func (h *nackHistory) drop(idx int, e *nackEntry) {
	h.bytes -= e.storedSize
	nackRecycle(e.payload)
	h.ring[idx] = nil
}

// trim forgets the oldest packets until the history fits max bytes and the ring.
func (h *nackHistory) trim(max int) {
	for h.oldest != h.highest && (h.bytes > max || int(h.highest-h.oldest) >= nackSlots) {
		idx := int(h.oldest) % nackSlots
		if e := h.ring[idx]; e != nil {
			h.drop(idx, e)
		}
		h.oldest++
	}
}

// find returns a copy of packet seq for resending, or false when it is not
// remembered or was resent within interval. The copy is the caller's: the
// history goes on without it.
func (h *nackHistory) find(seq uint16, now time.Time, interval time.Duration) (rtp.Header, []byte, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.started || h.highest-seq >= 1<<15 || seq-h.oldest >= 1<<15 {
		return rtp.Header{}, nil, false
	}
	e := h.ring[int(seq)%nackSlots]
	if e == nil || e.header.SequenceNumber != seq {
		return rtp.Header{}, nil, false
	}
	if !e.resentAt.IsZero() && now.Sub(e.resentAt) < interval {
		return rtp.Header{}, nil, false
	}
	e.resentAt = now
	return e.header.Clone(), append([]byte(nil), e.payload...), true
}

// forgetResend allows seq to be resent at once again (its resend was not sent).
func (h *nackHistory) forgetResend(seq uint16) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e := h.ring[int(seq)%nackSlots]; e != nil && e.header.SequenceNumber == seq {
		e.resentAt = time.Time{}
	}
}

// bitrate is the track's measured bitrate in bytes per second (zero before
// the first measurement).
func (h *nackHistory) bitrate() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rate
}

// retained is the bytes the history holds now.
func (h *nackHistory) retained() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bytes
}

// clear releases everything.
func (h *nackHistory) clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, e := range h.ring {
		if e != nil {
			h.drop(i, e)
		}
	}
	h.started, h.bytes = false, 0
}
