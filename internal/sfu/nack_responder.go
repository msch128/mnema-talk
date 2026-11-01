package sfu

import (
	"encoding/binary"
	"math/bits"
	"sync"
	"sync/atomic"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/nack"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// nackResponderFactory answers the NACKs of one subscriber connection from a
// history of the video it was sent. It replaces Pion's responder, whose
// history has a fixed size; see nack.go. Pion's is left in the chain with a
// filter that gives it no streams (disablePionResponder).
type nackResponderFactory struct {
	load *peerLoad
	now  func() time.Time
	// limits is the retransmission budget of each connection; the zero value
	// means defaultResendLimits.
	limits resendLimits
}

func (f nackResponderFactory) NewInterceptor(string) (interceptor.Interceptor, error) {
	now := f.now
	if now == nil {
		now = time.Now
	}
	limits := f.limits
	if limits == (resendLimits{}) {
		limits = defaultResendLimits
	}
	return &nackResponder{load: f.load, now: now, streams: map[uint32]*nackStream{}, budgetFor: newResendBudget(limits)}, nil
}

// disablePionResponder is the option that leaves Pion's own responder with no
// stream to answer for.
func disablePionResponder() nack.ResponderOption {
	return nack.ResponderStreamsFilter(func(*interceptor.StreamInfo) bool { return false })
}

type nackStream struct {
	info    *interceptor.StreamInfo
	writer  interceptor.RTPWriter
	history nackHistory
	rtxSeq  rtp.Sequencer
	mu      sync.Mutex
	rtt     time.Duration
}

// nackResponder is the interceptor of one connection.
type nackResponder struct {
	interceptor.NoOp
	load *peerLoad
	now  func() time.Time

	mu      sync.Mutex
	streams map[uint32]*nackStream
	// budgetFor limits the resends of this connection.
	budgetFor *resendBudget
	// count is len(streams), readable without the lock on every packet.
	count atomic.Int32
	// answering is set while a NACK is being answered: one at a time.
	answering atomic.Bool
	// afterResend, when set (tests), is called when a NACK has been answered.
	afterResend func()
}

// supportsNack: only video streams negotiate NACK feedback.
func supportsNack(info *interceptor.StreamInfo) bool {
	for _, fb := range info.RTCPFeedback {
		if fb.Type == "nack" && fb.Parameter == "" {
			return true
		}
	}
	return false
}

// budget is the history this connection lets one of its tracks hold: the
// bitrate times the window the load allows, within the track's share of the
// connection's budget and never less than a keyframe.
func (n *nackResponder) budget(rate float64) int {
	peers := n.load.count()
	streams := max(int(n.count.Load()), 1)
	share := max(nackConnBytes(peers)/streams, nackMinBytes)
	if rate <= 0 {
		return min(nackStartBytes, share)
	}
	return max(min(int(rate*nackWindow(peers).Seconds()), share), nackMinBytes)
}

func (n *nackResponder) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {
	if !supportsNack(info) {
		return writer
	}
	s := &nackStream{info: info, writer: writer, rtxSeq: rtp.NewRandomSequencer()}
	s.history.budget = n.budget
	n.mu.Lock()
	n.streams[info.SSRC] = s
	n.count.Store(int32(len(n.streams)))
	n.mu.Unlock()
	return interceptor.RTPWriterFunc(func(h *rtp.Header, payload []byte, a interceptor.Attributes) (int, error) {
		if h.SSRC == info.SSRC {
			s.history.add(h, payload, n.now())
		}
		return writer.Write(h, payload, a)
	})
}

func (n *nackResponder) UnbindLocalStream(info *interceptor.StreamInfo) {
	n.mu.Lock()
	s := n.streams[info.SSRC]
	delete(n.streams, info.SSRC)
	n.count.Store(int32(len(n.streams)))
	n.mu.Unlock()
	if s != nil {
		s.history.clear()
	}
}

func (n *nackResponder) Close() error {
	n.mu.Lock()
	streams := n.streams
	n.streams = map[uint32]*nackStream{}
	n.count.Store(0)
	n.mu.Unlock()
	for _, s := range streams {
		s.history.clear()
	}
	return nil
}

func (n *nackResponder) BindRTCPReader(r interceptor.RTCPReader) interceptor.RTCPReader {
	return interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		size, attr, err := r.Read(b, a)
		if err != nil || size == 0 {
			return size, attr, err
		}
		scanRTCP(b[:size], n.feedback)
		return size, attr, err
	})
}

// feedback looks at one RTCP packet read from the subscriber.
func (n *nackResponder) feedback(typ byte, format uint8, raw []byte) {
	switch {
	case typ == rtcpTypeReceiverReport:
		n.noteRTT(raw)
	case typ == rtcpTypeTransportFeedback && format == fmtNACK:
		// One NACK is answered at a time: a subscriber flooding requests cannot
		// start work faster than it finishes, and loses the ones in between
		// (it asks again for what it still misses).
		if !n.answering.CompareAndSwap(false, true) {
			return
		}
		var req rtcp.TransportLayerNack
		if req.Unmarshal(raw) != nil {
			n.answering.Store(false)
			return
		}
		go n.answer(&req)
	}
}

// answer resends in a goroutine of its own, so the reader is not held up.
func (n *nackResponder) answer(req *rtcp.TransportLayerNack) {
	defer n.answering.Store(false)
	n.resend(req)
	if n.afterResend != nil {
		n.afterResend()
	}
}

// noteRTT keeps the round trip time of each stream a receiver report names; a
// packet is not resent again within a round trip of its last resend.
func (n *nackResponder) noteRTT(raw []byte) {
	var rr rtcp.ReceiverReport
	if rr.Unmarshal(raw) != nil {
		return
	}
	now := n.now()
	for _, r := range rr.Reports {
		n.mu.Lock()
		s := n.streams[r.SSRC]
		n.mu.Unlock()
		if s == nil {
			continue
		}
		if _, rtt, ok := receiverReportOf(&rr, r.SSRC, now); ok && rtt > 0 {
			s.mu.Lock()
			s.rtt = rtt
			s.mu.Unlock()
		}
	}
}

// nackMaxSeqs is the most packets one NACK is answered for; a request names up
// to about 17 per pair and a receiver has no use for more than a few frames.
const nackMaxSeqs = 128

// nackSeqs is the number of packets a request names.
func nackSeqs(req *rtcp.TransportLayerNack) int {
	total := 0
	for _, pair := range req.Nacks {
		total += bits.OnesCount16(uint16(pair.LostPackets)) + 1
	}
	return total
}

// resend answers one NACK: the first nackMaxSeqs packets it names that are
// still remembered, as far as the connection's retransmission budget goes.
func (n *nackResponder) resend(req *rtcp.TransportLayerNack) {
	n.mu.Lock()
	s := n.streams[req.MediaSSRC]
	n.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	interval := s.rtt + 5*time.Millisecond
	s.mu.Unlock()
	rate := n.mediaRate()
	left := min(nackSeqs(req), nackMaxSeqs)
	for _, pair := range req.Nacks {
		pair.Range(func(seq uint16) bool {
			if left == 0 {
				return false
			}
			if !n.resendOne(s, seq, interval, rate) {
				// The budget is empty: the rest of this request is refused.
				metrics.SFURetransmitDropped.Add(uint64(left))
				left = 0
				return false
			}
			left--
			return true
		})
		if left == 0 {
			return
		}
	}
}

// resendOne resends packet seq; false when the budget refused it. The budget
// is asked before the packet is copied, and a refused packet stays as it was.
func (n *nackResponder) resendOne(s *nackStream, seq uint16, interval time.Duration, mediaRate float64) bool {
	now := n.now()
	size, ok := s.history.size(seq, now, interval)
	if !ok {
		return true
	}
	if !n.budgetFor.allow(now, size, mediaRate) {
		return false
	}
	header, payload, ok := s.history.find(seq, now, interval)
	if !ok {
		return true
	}
	if s.info.SSRCRetransmission != 0 && s.info.PayloadTypeRetransmission != 0 {
		header, payload = s.rtxForm(header, payload)
	}
	if _, err := s.writer.Write(&header, payload, interceptor.Attributes{}); err != nil {
		s.history.forgetResend(seq)
		return true
	}
	metrics.SFURetransmitted.Inc()
	return true
}

// rtxForm wraps a packet for the retransmission stream: its own SSRC, payload
// type and numbering, the original sequence number in front of the payload.
func (s *nackStream) rtxForm(h rtp.Header, payload []byte) (rtp.Header, []byte) {
	out := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(out, h.SequenceNumber)
	copy(out[2:], payload)
	h.SSRC = s.info.SSRCRetransmission
	h.PayloadType = s.info.PayloadTypeRetransmission
	h.SequenceNumber = s.rtxSeq.NextSequenceNumber()
	h.Padding, h.PaddingSize = false, 0
	return h, out
}

// mediaRate is what this connection is sent now, in bytes per second.
func (n *nackResponder) mediaRate() float64 {
	n.mu.Lock()
	streams := make([]*nackStream, 0, len(n.streams))
	for _, s := range n.streams {
		streams = append(streams, s)
	}
	n.mu.Unlock()
	total := 0.0
	for _, s := range streams {
		total += s.history.bitrate()
	}
	return total
}
