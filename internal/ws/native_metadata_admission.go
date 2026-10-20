package ws

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// NativeMetadataWebSocketHandler uses the qualified native lease/admission/Hub
// machinery with a restricted read pump. Session authentication never permits
// voice, content or cryptographic controls without a separately verified Core
// grant. The existing full native handler remains unmounted.
func (h *Hub) NativeMetadataWebSocketHandler(a NativeAuthenticator) http.Handler {
	return h.nativePumps(a, (*Client).nativeMetadataWritePump, (*Client).nativeMetadataReadPump)
}

func (c *Client) nativeMetadataReadPump() {
	defer func() { c.hub.unregister(c); c.close() }()
	c.conn.SetReadLimit(1024)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { return c.conn.SetReadDeadline(time.Now().Add(pongWait)) })
	var budget eventBudget
	for {
		kind, frame, err := c.conn.ReadMessage()
		if err != nil || !c.dispatchMetadataFrame(&budget, kind, frame) {
			return
		}
	}
}

// dispatchMetadataFrame routes one frame of a metadata session. It reports
// false when the connection was terminated.
func (c *Client) dispatchMetadataFrame(budget *eventBudget, kind int, frame []byte) bool {
	if metadataSocketRejected(c, kind) {
		c.hub.terminateNativeClient(c)
		return false
	}
	now := time.Now()
	if !budget.allowFrame(now) {
		return true
	}
	eventType, payload, valid := parseNativeMetadataFrame(frame)
	if !valid {
		c.hub.terminateNativeClient(c)
		return false
	}
	if c.handleNativeControl(eventType, frame) {
		return true
	}
	if metadataEventRejected(eventType) {
		c.hub.terminateNativeClient(c)
		return false
	}
	if budget.allowEvent(eventType, len(payload), now) {
		c.handle(eventType, payload)
	}
	return true
}

// Metadata sessions are members of the same Hub but must not receive browser
// message/media payloads. Filter on the wire before writing, retaining the real
// native generation/deadline/terminal checks and bounded existing send queue.
func nativeMetadataEventAllowed(raw []byte) bool {
	var event struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return false
	}
	switch event.Type {
	case "server_info", "system_update", "pong", "presence_snapshot", "presence_update", "channels_changed", "member_joined", "user_update", "user_stats", "native_access_renewed":
		return true
	default:
		return false
	}
}
func (c *Client) nativeMetadataWritePump() {
	ping := time.NewTicker(c.hub.pingEvery())
	defer func() { ping.Stop(); c.close() }()
	write := func(kind int, payload []byte) bool {
		if !c.commandsAllowed() {
			c.hub.terminateNativeClient(c)
			return false
		}
		_, _, deadline := c.native.snapshot()
		_ = c.conn.SetWriteDeadline(minTime(time.Now().Add(writeWait), deadline))
		return c.conn.WriteMessage(kind, payload) == nil
	}
	for {
		select {
		case <-c.done:
			return
		case raw := <-c.send:
			if !nativeMetadataEventAllowed(raw) {
				continue
			}
			if !write(websocket.TextMessage, raw) {
				return
			}
		case <-ping.C:
			if !write(websocket.PingMessage, nil) {
				return
			}
		}
	}
}

// Parse closed metadata/control envelopes before dispatch. Duplicate or unknown
// fields are denied, rather than permitting last-value-wins JSON ambiguity.
func metadataSocketRejected(c *Client, kind int) bool {
	if kind != websocket.TextMessage {
		return true
	}
	return !c.commandsAllowed()
}

func metadataEventRejected(eventType string) bool {
	if eventType == "ping" {
		return false
	}
	return eventType != "presence_idle"
}

func nativeMetadataObject(raw []byte) (map[string]json.RawMessage, bool) {
	d := json.NewDecoder(bytes.NewReader(raw))
	if !expectToken(d, json.Delim('{')) {
		return nil, false
	}
	values := make(map[string]json.RawMessage)
	if !readMetadataMembers(d, values) {
		return nil, false
	}
	if !expectToken(d, json.Delim('}')) {
		return nil, false
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, false
	}
	return values, true
}

func readMetadataMembers(d *json.Decoder, values map[string]json.RawMessage) bool {
	for d.More() {
		if !readMetadataMember(d, values) {
			return false
		}
	}
	return true
}

func readMetadataMember(d *json.Decoder, values map[string]json.RawMessage) bool {
	token, err := d.Token()
	if err != nil {
		return false
	}
	key, _ := token.(string) // a decoder always yields a string where an object key is due
	if _, duplicate := values[key]; duplicate {
		return false
	}
	var value json.RawMessage
	if d.Decode(&value) != nil {
		return false
	}
	values[key] = value
	return true
}

func parseNativeMetadataFrame(raw []byte) (string, json.RawMessage, bool) {
	eventType, payload, ok := metadataEnvelope(raw)
	if !ok {
		return "", nil, false
	}
	if eventType == "native_access_renew" {
		_, valid := parseNativeRenewFrame(raw)
		return eventType, payload, valid
	}
	return metadataEvent(eventType, payload)
}

func metadataEnvelope(raw []byte) (string, json.RawMessage, bool) {
	if len(raw) > 1024 {
		return "", nil, false
	}
	fields, ok := nativeMetadataObject(raw)
	if !ok || len(fields) != 2 {
		return "", nil, false
	}
	var eventType string
	if json.Unmarshal(fields["type"], &eventType) != nil {
		return "", nil, false
	}
	payload, found := fields["payload"]
	if !found {
		return "", nil, false
	}
	return eventType, payload, true
}

func metadataEvent(eventType string, payload json.RawMessage) (string, json.RawMessage, bool) {
	object, ok := nativeMetadataObject(payload)
	if !ok || len(object) != 1 {
		return "", nil, false
	}
	if !metadataPayload(eventType, object) {
		return "", nil, false
	}
	return eventType, payload, true
}

func metadataPayload(eventType string, object map[string]json.RawMessage) bool {
	switch eventType {
	case "ping":
		return finiteTimestamp(object["t"])
	case "presence_idle":
		return idleFlag(object)
	default:
		return false
	}
}

func finiteTimestamp(raw json.RawMessage) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	var timestamp float64
	if json.Unmarshal(raw, &timestamp) != nil {
		return false
	}
	if timestamp < 0 || timestamp > 9007199254740991 {
		return false
	}
	return !math.IsInf(timestamp, 0) && !math.IsNaN(timestamp)
}

func idleFlag(object map[string]json.RawMessage) bool {
	value, found := object["idle"]
	if !found || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return false
	}
	var idle bool
	return json.Unmarshal(value, &idle) == nil
}
