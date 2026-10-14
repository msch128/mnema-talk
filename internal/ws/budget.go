package ws

import "time"

const (
	// maxEventsPerSec caps a connection's ordinary events (typing, speaking,
	// subscriptions, ...).
	maxEventsPerSec = 40
	// maxSignalingPerSec is a separate, larger budget for the events a media
	// connection cannot do without: an answer, ICE candidate, or media
	// handshake dropped by the flood guard would leave the call half
	// negotiated or silent. A browser sends a burst of candidates per
	// negotiation, far below this.
	maxSignalingPerSec = 200
	// maxHandshakesPerSec and maxHandshakeBytesPerSec are the media_handshake
	// budget. A relayed handshake fans out to every other connection in the
	// room, so it is metered apart from the signaling budget and by bytes as
	// well. A join, leave or rekey is a handful of messages (key package,
	// commit, welcome; the last two up to ~44 KiB of base64 each), so a full
	// room still passes while one member cannot push megabytes per second
	// at everyone else.
	maxHandshakesPerSec     = 10
	maxHandshakeBytesPerSec = 256 << 10
	// maxFramesPerSec bounds all frames before they are even parsed.
	maxFramesPerSec = maxEventsPerSec + maxSignalingPerSec
)

// signalingEvents are the events counted against maxSignalingPerSec, with
// the largest payload each may carry. An answer is a full SDP and stays
// under maxMessageBytes; an ICE candidate is one short line.
var signalingEvents = map[string]int{
	"webrtc_answer":    maxMessageBytes,
	"webrtc_candidate": 2 << 10,
}

// handshakeEvent is metered by handleHandshake, not by signalingEvents.
const handshakeEvent = "media_handshake"

// eventBudget is a connection's per-second flood guard. It is used only by
// the connection's read loop, so it needs no lock.
type eventBudget struct {
	window    time.Time
	frames    int
	events    int
	signaling int

	handshakes     int
	handshakeBytes int
}

func (b *eventBudget) roll(now time.Time) {
	if now.Sub(b.window) >= time.Second {
		b.window, b.frames, b.events, b.signaling = now, 0, 0, 0
		b.handshakes, b.handshakeBytes = 0, 0
	}
}

// allowFrame counts a raw frame against the overall cap.
func (b *eventBudget) allowFrame(now time.Time) bool {
	b.roll(now)
	b.frames++
	return b.frames <= maxFramesPerSec
}

// allowEvent counts a parsed event against its category's budget. An
// oversized signaling payload is dropped (and still counted).
func (b *eventBudget) allowEvent(eventType string, payloadLen int, now time.Time) bool {
	b.roll(now)
	if eventType == handshakeEvent {
		// One handshake is the only copy of a key package or welcome. It
		// stays inside the socket frame limit; the voice room then caps the
		// decoded body. Dropped attempts still count.
		b.handshakes++
		b.handshakeBytes += payloadLen
		return b.handshakes <= maxHandshakesPerSec &&
			b.handshakeBytes <= maxHandshakeBytesPerSec &&
			payloadLen <= maxMessageBytes
	}
	if maxLen, ok := signalingEvents[eventType]; ok {
		b.signaling++
		return b.signaling <= maxSignalingPerSec && payloadLen <= maxLen
	}
	b.events++
	return b.events <= maxEventsPerSec
}
