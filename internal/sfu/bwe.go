package sfu

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// The SFU estimates how much a viewer's connection carries with Google
// Congestion Control (Pion's pkg/gcc). The browser reports the arrival time of
// every packet it receives in transport-wide congestion control feedback (the
// transport-cc header extension, which the SFU stamps on what it sends); the
// estimator turns the send and arrival times into a bitrate. The estimate is
// only read (by the layer adapter, adapt.go): nothing is paced, delayed or
// dropped, so audio, cameras and layers that are not adapted are untouched.
// The cost per connection is the estimator's history of the last 250 packets
// and two small goroutines.
const (
	// bweInitialBitrate starts high so a fresh connection does not look
	// slow while the estimate ramps up; the estimate falls on its own the
	// moment the feedback shows queues building.
	bweInitialBitrate = 20_000_000
	bweMinBitrate     = 150_000
	bweMaxBitrate     = 60_000_000
	// bweMinFeedback is how many feedback packets (about one every 50 to
	// 100 ms) an estimate needs before it is trusted.
	bweMinFeedback = 30
)

// bweKey is the RTCP attribute under which the estimator is handed to whoever
// reads a viewer's feedback.
type bweKey struct{}

// bandwidthSource gives the estimate of one connection in bits per second,
// zero while it is not known.
type bandwidthSource interface {
	bandwidth() int
}

// newEstimator is gcc's constructor; tests replace it.
var newEstimator = gcc.NewSendSideBWE

// bweFactory builds one estimator interceptor per connection. It is added to
// the registry first, so it is the innermost writer: it records what actually
// leaves, after the authorization gate, and it must see packets after the
// transport-cc stamp, which is added after it.
type bweFactory struct{}

func (bweFactory) NewInterceptor(string) (interceptor.Interceptor, error) {
	pacer := &passPacer{writers: map[uint32]interceptor.RTPWriter{}}
	est, err := newEstimator(
		gcc.SendSideBWEInitialBitrate(bweInitialBitrate),
		gcc.SendSideBWEMinBitrate(bweMinBitrate),
		gcc.SendSideBWEMaxBitrate(bweMaxBitrate),
		gcc.SendSideBWEPacer(pacer),
	)
	if err != nil {
		return nil, fmt.Errorf("bandwidth estimator: %w", err)
	}
	return &bweInterceptor{est: est, pacer: pacer}, nil
}

type bweInterceptor struct {
	interceptor.NoOp
	est   *gcc.SendSideBWE
	pacer *passPacer
	// feedback counts the transport-wide feedback packets seen.
	feedback atomic.Uint32
	// closeOnce: the estimator panics when closed twice.
	closeOnce sync.Once
	closeErr  error
}

// bandwidth is the estimate once enough feedback has arrived to trust it.
func (b *bweInterceptor) bandwidth() int {
	if b.feedback.Load() < bweMinFeedback {
		return 0
	}
	return max(b.est.GetTargetBitrate(), 0)
}

func (b *bweInterceptor) BindLocalStream(info *interceptor.StreamInfo, w interceptor.RTPWriter) interceptor.RTPWriter {
	out := b.est.AddStream(info, w)
	// Retransmissions of a stream (RTX) have an SSRC of their own and go the
	// same way as the stream.
	if info.SSRCRetransmission != 0 {
		b.pacer.alias(info.SSRCRetransmission, info.SSRC)
	}
	return out
}

func (b *bweInterceptor) BindRTCPReader(r interceptor.RTCPReader) interceptor.RTCPReader {
	return interceptor.RTCPReaderFunc(func(buf []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		n, attr, err := r.Read(buf, a)
		if err != nil {
			return n, attr, err
		}
		if attr == nil {
			attr = interceptor.Attributes{}
		}
		attr.Set(bweKey{}, bandwidthSource(b))
		if n > 0 {
			b.feedEstimator(buf[:n], attr)
		}
		return n, attr, nil
	})
}

// feedEstimator hands the transport-wide feedback in the compound RTCP packet
// to the bandwidth estimator. Only those packets are decoded (Unmarshal copies
// what it keeps), and the estimator gets nothing else.
func (b *bweInterceptor) feedEstimator(compound []byte, attr interceptor.Attributes) {
	for _, fb := range transportFeedbackOf(compound) {
		b.feedback.Add(1)
		// An estimator that cannot use a feedback must not stop the reader.
		_ = b.est.WriteRTCP([]rtcp.Packet{fb}, attr)
	}
}

func (b *bweInterceptor) Close() error {
	b.closeOnce.Do(func() { b.closeErr = b.est.Close() })
	return b.closeErr
}

// passPacer is gcc's pacer without pacing: it hands a packet to the writer of
// its stream at once.
type passPacer struct {
	mu      sync.RWMutex
	writers map[uint32]interceptor.RTPWriter
	// target is the last bitrate the estimator set, which only informs.
	target atomic.Int64
}

func (p *passPacer) AddStream(ssrc uint32, w interceptor.RTPWriter) {
	p.mu.Lock()
	p.writers[ssrc] = w
	p.mu.Unlock()
}

// alias sends packets of ssrc to the writer already registered for target.
func (p *passPacer) alias(ssrc, target uint32) {
	p.mu.Lock()
	p.writers[ssrc] = p.writers[target]
	p.mu.Unlock()
}

func (p *passPacer) Write(h *rtp.Header, payload []byte, a interceptor.Attributes) (int, error) {
	p.mu.RLock()
	w, ok := p.writers[h.SSRC]
	p.mu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("bandwidth estimator: unknown stream %d", h.SSRC)
	}
	return w.Write(h, payload, a)
}

func (p *passPacer) SetTargetBitrate(bps int) { p.target.Store(int64(bps)) }

func (*passPacer) Close() error { return nil }
