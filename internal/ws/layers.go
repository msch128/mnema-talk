package ws

import (
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// announceScreenLayers tells the members of a voice room which layers a
// screen share consists of (empty once it ends), so viewers can label their
// quality choices.
func (h *Hub) announceScreenLayers(roomID, sharer uuid.UUID, layers []sfu.Layer) {
	h.sendToVoiceRoom(roomID, events.ScreenLayers, screenLayersPayload(roomID, sharer, layers))
}

func screenLayersPayload(roomID, sharer uuid.UUID, layers []sfu.Layer) map[string]any {
	if layers == nil {
		layers = []sfu.Layer{} // [] rather than null on the wire
	}
	return map[string]any{"channel_id": roomID, "user_id": sharer, "layers": layers}
}

// announceScreenQuality tells a viewer which layer of sharer's screen share it
// receives (layer -1 while none yet) and whether the server chooses it.
func (h *Hub) announceScreenQuality(roomID, viewer, sharer uuid.UUID, st sfu.QualityState) {
	h.sendToVoiceUser(roomID, viewer, events.ScreenQuality, map[string]any{
		"channel_id": roomID, "user_id": sharer, "layer": st.Layer, "auto": st.Auto,
	})
}

// announceLayersWanted tells a sharer which of its layers some viewer needs.
func (h *Hub) announceLayersWanted(roomID, sharer uuid.UUID, layers []int) {
	if layers == nil {
		layers = []int{}
	}
	h.sendToVoiceUser(roomID, sharer, events.ScreenLayersWanted, map[string]any{"channel_id": roomID, "layers": layers})
}

// announceCameraLayersWanted is announceLayersWanted for the layers of the
// sender's camera: the same event, marked with its source.
func (h *Hub) announceCameraLayersWanted(roomID, sharer uuid.UUID, layers []int) {
	if layers == nil {
		layers = []int{}
	}
	h.sendToVoiceUser(roomID, sharer, events.ScreenLayersWanted, map[string]any{"channel_id": roomID, "layers": layers, "source": sfu.SourceCamera})
}

// sendToVoiceUser sends to the connections of userID that are in chID's room.
func (h *Hub) sendToVoiceUser(chID, userID uuid.UUID, eventType string, payload any) {
	data := encode(eventType, payload)
	if data == nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.User.ID != userID {
			continue
		}
		if cur := c.currentVoice(); cur != nil && *cur == chID {
			c.deliver(data)
		}
	}
}

// handleLayers records the layers of the sender's screen share, or of its
// camera with "source": "camera": {layers: [{width, height, fps}, ...]}, best
// first.
func (c *Client) handleLayers(payload json.RawMessage) {
	var req struct {
		Layers []sfu.Layer `json:"layers"`
		Source string      `json:"source"`
	}
	if json.Unmarshal(payload, &req) != nil {
		return
	}
	room := c.currentRoom()
	if room == nil {
		return
	}
	set := room.SetScreenLayers
	if sfu.Source(req.Source) == sfu.SourceCamera {
		set = room.SetCameraLayers
	}
	if err := set(c.User.ID, req.Layers); err != nil {
		slog.Debug("sfu layers rejected", "user", c.User.ID, "source", req.Source, "err", err)
	}
}

// parseQuality reads the optional quality of a screen subscription: "auto" or
// the index of a layer.
func parseQuality(raw json.RawMessage) (layer int, present, ok bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false, true
	}
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return -1, true, name == "auto"
	}
	var index int
	if json.Unmarshal(raw, &index) != nil || index < 0 || index >= sfu.MaxLayers {
		return 0, true, false
	}
	return index, true, true
}
