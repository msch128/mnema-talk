package sfu

import (
	"log/slog"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
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
const ingressAbuseWindows = 10

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
	slog.Warn("sfu publisher exceeds the ingress limit, closing its connection", "user", info.SenderID, "source", string(info.Source))
	metrics.SFUIngressDropped.With("closed").Inc()
	if info.publisher != nil && info.publisher.PC != nil {
		_ = info.publisher.PC.Close()
	}
}
