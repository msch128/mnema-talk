package ws

import (
	"encoding/base64"
	"encoding/json"

	"github.com/google/uuid"
)

// maxMediaHandshakeBytes is the largest opaque handshake the hub will relay.
// 32 KiB of raw bytes is about 44 KiB of base64, inside the WebSocket frame.
const maxMediaHandshakeBytes = 32 << 10

// handleMediaHandshake relays an opaque handshake to the other connections in
// the sender's current voice room. The bytes are not parsed or logged.
func (c *Client) handleMediaHandshake(payload json.RawMessage) {
	var body struct {
		ChannelID uuid.UUID `json:"channel_id"`
		Wire      string    `json:"wire"`
	}
	if json.Unmarshal(payload, &body) != nil || body.Wire == "" {
		return
	}
	cur := c.currentVoice()
	if cur == nil || *cur != body.ChannelID {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(body.Wire)
	if err != nil || len(raw) == 0 || len(raw) > maxMediaHandshakeBytes {
		return
	}
	c.hub.sendToVoiceRoomExcept(c, *cur, "media_handshake", map[string]any{
		"channel_id": *cur,
		"user_id":    c.User.ID,
		"wire":       body.Wire,
	})
}
