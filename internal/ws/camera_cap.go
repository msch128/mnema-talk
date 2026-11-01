package ws

import (
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// maxCameraBatch is how many cameras one webrtc_subscribe may name. It keeps
// the payload under the cap for client events (about 40 characters an ID).
const maxCameraBatch = 16

// announceCamerasCapped tells a viewer whose cameras the server does not
// forward because the viewer asked for more than sfu.MaxViewerCameras.
func (h *Hub) announceCamerasCapped(roomID, viewer uuid.UUID, publishers []uuid.UUID) {
	if publishers == nil {
		publishers = []uuid.UUID{} // [] rather than null on the wire
	}
	h.sendToVoiceUser(roomID, viewer, events.CamerasCapped, map[string]any{"channel_id": roomID, "user_ids": publishers})
}

type cameraBatchWire struct {
	Kind    string   `json:"kind"`
	UserIDs []string `json:"user_ids"`
	On      bool     `json:"on"`
}

// parseCameraBatch reads a webrtc_subscribe that names several cameras:
// {kind: "camera", user_ids: [...], on}. batch is false for any other
// payload (the single-publisher forms); ok is false for a malformed batch,
// which is dropped whole.
func parseCameraBatch(payload json.RawMessage, viewer uuid.UUID) (ids []uuid.UUID, on, batch, ok bool) {
	var wire cameraBatchWire
	if json.Unmarshal(payload, &wire) != nil || wire.UserIDs == nil {
		return nil, false, false, false
	}
	if wire.Kind != string(sfu.SourceCamera) || len(wire.UserIDs) == 0 || len(wire.UserIDs) > maxCameraBatch {
		return nil, false, true, false
	}
	for _, raw := range wire.UserIDs {
		id, valid := parsePublisher(false, raw, viewer)
		if !valid {
			return nil, false, true, false
		}
		ids = append(ids, id)
	}
	return ids, wire.On, true, true
}

// handleCameraBatch applies a batch of camera choices with one renegotiation.
func (c *Client) handleCameraBatch(ids []uuid.UUID, on bool) {
	room := c.activeVoiceRoom()
	if room == nil {
		return
	}
	if err := room.SubscribeCameras(c.User.ID, ids, on); err != nil {
		slog.Debug("sfu camera batch rejected", "user", c.User.ID, "err", err)
	}
}
