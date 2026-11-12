package sfu

import (
	"io"
	"log/slog"
	"strconv"
	"strings"
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

// Pion keeps, for the life of a connection, one SRTCP read stream for every
// SSRC an RTCP packet of the member names that nothing on the connection
// reads: PLI, NACK, FIR and REMB name a media SSRC, a receiver or sender
// report each of its up to 31 blocks, BYE and SDES each source. Pion drops
// none and has no cap. rtcpStreamCap is the logger and buffer factory of one
// member connection and bounds them: a stream gets a buffer (rtcpBuffer, at
// most about 20 KB) only for an SSRC the SFU may read; one for an SSRC the
// client declared or the SFU just stopped sending gets a quietBuffer that
// keeps nothing; and the connection is closed past any of these caps.
const (
	// maxUnknownRTCPStreams: streams for SSRCs the SFU neither sent nor was
	// told of. An honest client names none (it names its own SSRCs, the
	// SFU's, the ones it declared in its answer, and ones that just ended).
	maxUnknownRTCPStreams = 64
	// maxBufferedRTCPStreams: streams with a buffer open at once (counted by
	// the buffer factory, so it holds even if Pion's log line changes). A
	// viewer reads one per track it receives, plus one per SSRC the client
	// sends: a full Talk is a few hundred.
	maxBufferedRTCPStreams = 1024
	// maxQuietRTCPStreams: quiet streams (about 0.5 KB each) made in the life
	// of the connection. A viewer makes up to one or two per track the SFU
	// stopped sending it: in a Talk of 50 with cameras coming and going for
	// eight hours, at most about 15 000.
	maxQuietRTCPStreams = 32768
	// rtcpRetiredSSRCs is how many of the SSRCs whose stream closed are
	// remembered: reports for one arrive within seconds of its end.
	rtcpRetiredSSRCs = 1024
	// rtcpDeclaredPerLine bounds the SSRCs taken from an answer per m-line
	// (a line declares its SSRC, an RTX SSRC and maybe simulcast layers).
	rtcpDeclaredPerLine = 4
	// maxSentTracks: senders the SFU makes in the life of the connection.
	// Each has SSRCs of its own, and Pion's SRTP context keeps their state
	// (about 160 bytes a sender, more with RTX) until the connection closes,
	// with no way to drop it; every subscription switched on makes one. The
	// same honest count as maxQuietRTCPStreams applies.
	maxSentTracks = 32768
)

// pionUnhandledRTCP is the line Pion's undeclaredRTCPMediaProcessor logs once
// for each unread stream it keeps (webrtc v4 peerconnection.go).
const pionUnhandledRTCP = "Incoming unhandled RTCP ssrc(%d), OnTrack will not be fired"

// pionUnhandledRTCPChanged stands, in tests, for a Pion that logs another line.
var pionUnhandledRTCPChanged atomic.Bool

type rtcpStreamCap struct {
	logs logging.LoggerFactory
	log  *slog.Logger
	pc   atomic.Pointer[webrtc.PeerConnection]
	// closing is set when the cap closes the connection; from then on Pion's
	// new streams get a closedBuffer.
	closing atomic.Bool

	mu sync.Mutex
	// sent are the SSRCs of the tracks the SFU sends now, true for the one it
	// reads feedback on and false for RTX; declared those of the client's last
	// answer; retired the last SSRCs the SFU stopped sending or reading,
	// oldest first in retiredRing; unknown the SSRCs of unread streams that
	// are none of these.
	sent                       map[uint32]bool
	declared, retired, unknown map[uint32]struct{}
	retiredRing                [rtcpRetiredSSRCs]uint32
	retiredNext                int
	buffered, quiet            int
	// lines is the most m-lines an answer of the client had.
	lines int
	// senders counts the senders made on the connection, up to senderCap
	// (maxSentTracks; tests lower it).
	senders, senderCap int
}

func newRTCPStreamCap(logs logging.LoggerFactory, log *slog.Logger) *rtcpStreamCap {
	return &rtcpStreamCap{logs: logs, log: log, sent: map[uint32]bool{}, senderCap: maxSentTracks,
		declared: map[uint32]struct{}{}, retired: map[uint32]struct{}{}, unknown: map[uint32]struct{}{}}
}

// bind names the connection to close. Join binds it before the connection can
// have exchanged any media.
func (c *rtcpStreamCap) bind(pc *webrtc.PeerConnection) { c.pc.Store(pc) }

// sentTrack records the SSRCs of a track the SFU now sends on the connection,
// as long as there are fewer than buffered streams may be open, and closes
// the connection past maxSentTracks senders.
func (c *rtcpStreamCap) sentTrack(sender *webrtc.RTPSender) {
	if c == nil {
		return
	}
	encodings := sender.GetParameters().Encodings
	c.mu.Lock()
	for _, e := range encodings {
		if len(c.sent) < maxBufferedRTCPStreams {
			c.sent[uint32(e.SSRC)] = true
		}
		if e.RTX.SSRC != 0 && len(c.sent) < maxBufferedRTCPStreams {
			c.sent[uint32(e.RTX.SSRC)] = false
		}
	}
	c.senders++
	senders, over := c.senders, c.senders > c.senderCap
	c.mu.Unlock()
	if over {
		c.trip("senders", senders)
	}
}

// stoppedTrack retires the SSRCs of a track the SFU no longer sends.
func (c *rtcpStreamCap) stoppedTrack(sender *webrtc.RTPSender) {
	if c == nil {
		return
	}
	encodings := sender.GetParameters().Encodings
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range encodings {
		c.retire(uint32(e.SSRC))
		if e.RTX.SSRC != 0 {
			c.retire(uint32(e.RTX.SSRC))
		}
	}
}

// retire requires c.mu.
func (c *rtcpStreamCap) retire(ssrc uint32) {
	delete(c.sent, ssrc)
	if _, ok := c.retired[ssrc]; ok {
		return
	}
	delete(c.retired, c.retiredRing[c.retiredNext])
	c.retiredRing[c.retiredNext] = ssrc
	c.retired[ssrc] = struct{}{}
	c.retiredNext = (c.retiredNext + 1) % rtcpRetiredSSRCs
}

// sdpOf is the SDP of a description, or "" for none.
func sdpOf(d *webrtc.SessionDescription) string {
	if d == nil {
		return ""
	}
	return d.SDP
}

// declare takes the SSRCs of the client's answer to the SFU's offer, at most
// rtcpDeclaredPerLine for each m-line, and only from as many m-lines as the
// offer has: Pion accepts an answer with more, and the client must not choose
// how many lines it is forgiven for. Firefox, for one, sends the receiver
// reports of each line it only receives on from an SSRC it declares there;
// nothing on the SFU reads those.
func (c *rtcpStreamCap) declare(answer, offer string) {
	if c == nil {
		return
	}
	offered := strings.Count(offer, "\nm=")
	declared := map[uint32]struct{}{}
	lines, budget := 0, 0
	for line := range strings.Lines(answer) {
		switch {
		case strings.HasPrefix(line, "m=") && lines == offered:
			budget = 0
		case strings.HasPrefix(line, "m="):
			lines++
			budget = rtcpDeclaredPerLine
		case strings.HasPrefix(line, "a=ssrc:") && budget > 0:
			id, _, _ := strings.Cut(strings.TrimPrefix(line, "a=ssrc:"), " ")
			if ssrc, err := strconv.ParseUint(strings.TrimSpace(id), 10, 32); err == nil {
				if _, seen := declared[uint32(ssrc)]; !seen {
					declared[uint32(ssrc)] = struct{}{}
					budget--
				}
			}
		}
	}
	c.mu.Lock()
	c.declared = declared
	c.forgiveNewLines(declared, lines)
	c.mu.Unlock()
}

// forgiveNewLines takes off the unknown count the SSRCs a client reported
// from before the SFU had the answer that declares them: a browser may start
// on a new line before its answer arrives over the WebSocket. Their stream
// keeps its buffer, so at most one is forgiven per m-line the connection
// gains, once in its life. Requires c.mu.
func (c *rtcpStreamCap) forgiveNewLines(declared map[uint32]struct{}, lines int) {
	budget := lines - c.lines
	c.lines = max(c.lines, lines)
	for ssrc := range c.unknown {
		if budget <= 0 {
			return
		}
		if _, ok := declared[ssrc]; ok {
			delete(c.unknown, ssrc)
			budget--
		}
	}
}

// buffer is the connection's BufferFactory. Pion calls it, under its stream
// lock, for every stream it opens or the client's RTCP makes.
func (c *rtcpStreamCap) buffer(kind packetio.BufferPacketType, ssrc uint32) io.ReadWriteCloser {
	if kind != packetio.RTCPBufferPacket {
		return srtpBuffer(kind, ssrc)
	}
	if c.closing.Load() {
		return closedBuffer{}
	}
	c.mu.Lock()
	_, declared := c.declared[ssrc]
	_, retired := c.retired[ssrc]
	read, sent := c.sent[ssrc]
	quiet := (declared || retired || sent) && !read
	if quiet {
		c.quiet++
	} else {
		c.buffered++
	}
	q, b := c.quiet, c.buffered
	c.mu.Unlock()
	if q > maxQuietRTCPStreams || b > maxBufferedRTCPStreams {
		c.trip("streams", q+b)
	}
	if quiet {
		return newQuietBuffer()
	}
	return &rtcpBuffer{attrBuffer: srtpBuffer(kind, ssrc).(attrBuffer), limit: c, ssrc: ssrc}
}

// closed is called once when a buffered stream closes: its SSRC is retired.
func (c *rtcpStreamCap) closed(ssrc uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buffered--
	c.retire(ssrc)
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
	if format == pionUnhandledRTCP && len(args) == 1 && !pionUnhandledRTCPChanged.Load() {
		ssrc, _ := args[0].(uint32)
		l.limit.unhandled(ssrc)
	}
	l.LeveledLogger.Warnf(format, args...)
}

// unhandled counts a stream Pion keeps unread, unless its SSRC is one the SFU
// sends (reports that came before the SFU opened its stream), the client
// declared, or the SFU just stopped sending.
func (c *rtcpStreamCap) unhandled(ssrc uint32) {
	c.mu.Lock()
	_, declared := c.declared[ssrc]
	_, retired := c.retired[ssrc]
	_, sent := c.sent[ssrc]
	if !declared && !retired && !sent {
		c.unknown[ssrc] = struct{}{}
	}
	unknown := len(c.unknown)
	c.mu.Unlock()
	if unknown > maxUnknownRTCPStreams {
		c.trip("unknown", unknown)
	}
}

// trip closes the connection once, on a goroutine of its own: Pion calls the
// cap from the loops its SRTCP session waits for while closing.
func (c *rtcpStreamCap) trip(count string, n int) {
	pc := c.pc.Load()
	if pc == nil || !c.closing.CompareAndSwap(false, true) {
		return
	}
	c.log.Warn("sfu connection keeps state for too many SSRCs, closing it", count, n)
	metrics.SFUIngressDropped.With("rtcp_ssrcs").Inc()
	safego.Go("sfu_rtcp_cap", safego.Abandon, func() { _ = pc.Close() })
}
