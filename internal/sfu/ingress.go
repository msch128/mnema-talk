package sfu

import (
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/safego"
	"github.com/pion/logging"
	"github.com/pion/transport/v5/packetio"
	"github.com/pion/webrtc/v4"
)

// A publisher's media is forwarded to every subscriber, so what one member
// sends multiplies. Each track gets a token bucket far above anything the
// web client produces, and packets over it are dropped (and counted).
//
// The client caps a screen layer at STREAM_MAX_BITRATE (12 Mbit/s, 4K at
// 60 fps) and a camera at 2.5 Mbit/s; video here may use three times the
// screen cap and a burst deep enough for a 4K keyframe. Opus stays under
// 0.5 Mbit/s; audio gets about 1 Mbit/s.
type ingressRates struct {
	bytesPerSec, burstBytes float64
	pktsPerSec, burstPkts   float64
}

var (
	videoIngress = ingressRates{bytesPerSec: 4_500_000, burstBytes: 3 << 20, pktsPerSec: 4000, burstPkts: 3000}
	audioIngress = ingressRates{bytesPerSec: 128_000, burstBytes: 64 << 10, pktsPerSec: 500, burstPkts: 200}
)

// ingressAbuseWindows is how many seconds in a row may see a drop before the
// publisher counts as grossly abusive and its connection is closed.
var ingressAbuseWindows = 10

type ingressVerdict int

const (
	ingressOK ingressVerdict = iota
	ingressDrop
	ingressAbusive
)

// ingressBucket limits one track. It is used by the track's forwarding
// goroutine only.
type ingressBucket struct {
	rates ingressRates
	bytes float64
	pkts  float64
	last  time.Time

	windowStart time.Time
	dropped     int
	badWindows  int
}

func newIngressBucket(kind webrtc.RTPCodecType) *ingressBucket {
	rates := audioIngress
	if kind == webrtc.RTPCodecTypeVideo {
		rates = videoIngress
	}
	return &ingressBucket{rates: rates, bytes: rates.burstBytes, pkts: rates.burstPkts}
}

// admit decides about one packet of n bytes arriving at now.
func (b *ingressBucket) admit(n int, now time.Time) ingressVerdict {
	if b.last.IsZero() {
		b.last, b.windowStart = now, now
	}
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.bytes = min(b.rates.burstBytes, b.bytes+elapsed*b.rates.bytesPerSec)
	b.pkts = min(b.rates.burstPkts, b.pkts+elapsed*b.rates.pktsPerSec)

	if now.Sub(b.windowStart) >= time.Second {
		if b.dropped > 0 {
			b.badWindows++
		} else {
			b.badWindows = 0
		}
		b.dropped, b.windowStart = 0, now
	}
	if b.bytes >= float64(n) && b.pkts >= 1 {
		b.bytes -= float64(n)
		b.pkts--
		return ingressOK
	}
	b.dropped++
	if b.badWindows >= ingressAbuseWindows {
		return ingressAbusive
	}
	return ingressDrop
}

// ingressAbuse closes the connection of a publisher that kept exceeding its
// limits for many seconds.
func ingressAbuse(info *TrackInfo) {
	info.logger().Warn("sfu publisher exceeds the ingress limit, closing its connection", "user", info.SenderID, "source", string(info.Source))
	metrics.SFUIngressDropped.With("closed").Inc()
	if info.publisher != nil && info.publisher.PC != nil {
		_ = info.publisher.PC.Close()
	}
}

// Pion keeps, for the life of a connection, one SRTCP read stream with a
// buffer for every SSRC an RTCP packet of the member names that nothing on the
// connection reads: PLI, NACK, FIR and REMB name a media SSRC, a receiver or
// sender report each of its up to 31 blocks, BYE and SDES each source. Pion
// drops none and has no cap, so the SFU bounds what one stream may hold
// (srtpBuffer) and how many there may be (rtcpStreamCap).

// The buffers of a connection's streams. Pion's default for SRTCP is 100 KB:
// a stream that is read (feedback on a track the SFU sends) holds a few
// packets at a time, and one that nobody reads keeps all it got.
const (
	rtcpBufferPackets = 128
	rtcpBufferBytes   = 16 << 10
	// rtpBufferBytes is Pion's own limit of an SRTP stream (srtpBufferSize).
	rtpBufferBytes = 1000 * 1000
)

// The unread streams a connection may make before it is closed. A browser
// makes them only for SSRCs that just ended: the reports still on the way
// when the SFU stopped sending it a track. Those SSRCs count only against
// maxRTCPStreams; any other counts against maxUnknownRTCPStreams too.
const (
	maxUnknownRTCPStreams = 64
	maxRTCPStreams        = 2048
)

// srtpBuffer is the SettingEngine's BufferFactory: Pion's own buffer (srtp's
// packetBuffer, which keeps each packet's attributes) with the limits above.
func srtpBuffer(kind packetio.BufferPacketType, _ uint32) io.ReadWriteCloser {
	b := packetio.NewBuffer()
	if kind == packetio.RTCPBufferPacket {
		b.SetLimitCount(rtcpBufferPackets)
		b.SetLimitSize(rtcpBufferBytes)
	} else {
		b.SetLimitSize(rtpBufferBytes)
	}
	return attrBuffer{b}
}

type attrBuffer struct{ *packetio.Buffer }

func (b attrBuffer) Read(p []byte) (int, error) {
	n, _, err := b.Buffer.Read(p, nil)
	return n, err
}

func (b attrBuffer) Write(p []byte) (int, error) { return b.Buffer.Write(p, nil) }

func (b attrBuffer) ReadWithAttributes(p []byte, a packetio.Attributes) (int, packetio.Attributes, error) {
	return b.Buffer.Read(p, a)
}

func (b attrBuffer) WriteWithAttributes(p []byte, a packetio.Attributes) (int, error) {
	return b.Buffer.Write(p, a)
}

// pionUnhandledRTCP is the line Pion's undeclaredRTCPMediaProcessor logs once
// for each unread stream it keeps (webrtc v4 peerconnection.go). The logger
// factory is the only per-connection hook Pion has on them.
const pionUnhandledRTCP = "Incoming unhandled RTCP ssrc(%d), OnTrack will not be fired"

// rtcpStreamCap is the logger factory of one member connection. It counts the
// unread SRTCP streams Pion keeps on it and closes it past a cap.
type rtcpStreamCap struct {
	logs logging.LoggerFactory
	log  *slog.Logger
	pc   atomic.Pointer[webrtc.PeerConnection]

	mu sync.Mutex
	// sentSSRCs are the SSRCs the SFU sent on the connection, RTX included.
	sentSSRCs      map[uint32]struct{}
	unknown, total int
	// closing is set when the cap closes the connection; from then on Pion's
	// new streams get no buffer (see buffer).
	closing atomic.Bool
}

// bind names the connection to close. Join binds it before the connection can
// have exchanged any media.
func (c *rtcpStreamCap) bind(pc *webrtc.PeerConnection) { c.pc.Store(pc) }

// sent records the SSRCs of a track the SFU now sends on the connection.
func (c *rtcpStreamCap) sent(sender *webrtc.RTPSender) {
	if c == nil {
		return
	}
	encodings := sender.GetParameters().Encodings
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sentSSRCs == nil {
		c.sentSSRCs = make(map[uint32]struct{})
	}
	for _, e := range encodings {
		for _, ssrc := range []webrtc.SSRC{e.SSRC, e.RTX.SSRC} {
			if ssrc != 0 {
				c.sentSSRCs[uint32(ssrc)] = struct{}{}
			}
		}
	}
}

// NewLogger counts on the connection's own logger only.
func (c *rtcpStreamCap) NewLogger(scope string) logging.LeveledLogger {
	l := c.logs.NewLogger(scope)
	if scope != "pc" {
		return l
	}
	return rtcpCountingLogger{LeveledLogger: l, limit: c}
}

type rtcpCountingLogger struct {
	logging.LeveledLogger
	limit *rtcpStreamCap
}

func (l rtcpCountingLogger) Warnf(format string, args ...any) {
	if format == pionUnhandledRTCP && len(args) == 1 {
		ssrc, _ := args[0].(uint32)
		l.limit.unhandled(ssrc)
	}
	l.LeveledLogger.Warnf(format, args...)
}

// buffer is the connection's BufferFactory: srtpBuffer, and once the cap
// closes the connection, a closed buffer for every SRTCP stream Pion still
// makes while the connection closes.
func (c *rtcpStreamCap) buffer(kind packetio.BufferPacketType, ssrc uint32) io.ReadWriteCloser {
	if kind == packetio.RTCPBufferPacket && c.closing.Load() {
		return closedBuffer{}
	}
	return srtpBuffer(kind, ssrc)
}

// closedBuffer holds nothing and takes nothing.
type closedBuffer struct{}

func (closedBuffer) Read([]byte) (int, error)  { return 0, io.EOF }
func (closedBuffer) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (closedBuffer) Close() error              { return nil }

// unhandled counts one more unread stream. The connection is closed on a
// goroutine of its own, as Pion logs this from the loop its SRTCP session
// waits for while closing.
func (c *rtcpStreamCap) unhandled(ssrc uint32) {
	pc := c.pc.Load()
	c.mu.Lock()
	c.total++
	if _, ok := c.sentSSRCs[ssrc]; !ok {
		c.unknown++
	}
	unknown, total := c.unknown, c.total
	over := unknown > maxUnknownRTCPStreams || total > maxRTCPStreams
	c.mu.Unlock()
	if !over || pc == nil || !c.closing.CompareAndSwap(false, true) {
		return
	}
	c.log.Warn("sfu connection keeps too many unread RTCP streams, closing it", "unknown", unknown, "streams", total)
	metrics.SFUIngressDropped.With("rtcp_ssrcs").Inc()
	safego.Go("sfu_rtcp_cap", safego.Abandon, func() { _ = pc.Close() })
}
