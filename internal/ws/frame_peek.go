package ws

import (
	"bytes"
	"encoding/json"

	"github.com/msch128/mnema-talk/internal/events"
)

const (
	// smallEventPayload is the payload cap of every client event that carries
	// a few fields (ping, idle, typing, speaking, join, subscribe, ...).
	smallEventPayload = 1 << 10
	// frameEnvelopeSlack is what a frame may carry beyond its payload: the
	// {"type":...,"payload":...} wrapper and a little whitespace.
	frameEnvelopeSlack = 256
)

// payloadCaps are the larger payloads some events may carry. An answer is a
// full SDP, a handshake an MLS message in base64, a diagnostic a few stats.
var payloadCaps = map[string]int{
	events.ClientWebrtcAnswer:    maxMessageBytes,
	events.ClientWebrtcCandidate: 2 << 10,
	events.ClientMediaHandshake:  maxMessageBytes,
	events.ClientWebrtcDiag:      4 << 10,
}

// payloadCap is the most payload bytes an event of this type may carry.
func payloadCap(eventType string) int {
	if n, ok := payloadCaps[eventType]; ok {
		return n
	}
	return smallEventPayload
}

// peekEventType reads the type of a frame without decoding it, when the type
// is the first member (as every client writes it). It is a prefix scan: the
// payload behind it is not touched.
func peekEventType(message []byte) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(message))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return "", false
	}
	if key, err := dec.Token(); err != nil || key != "type" {
		return "", false
	}
	tok, err := dec.Token()
	eventType, ok := tok.(string)
	return eventType, err == nil && ok
}

// frameTooLarge reports a frame that cannot hold a payload within its type's
// cap, so it can be dropped before it is decoded. Frames whose type is not
// first are judged after decoding.
func frameTooLarge(message []byte) bool {
	eventType, ok := peekEventType(message)
	if !ok {
		return false
	}
	return len(message) > payloadCap(eventType)+frameEnvelopeSlack
}
