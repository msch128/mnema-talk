package ws

import (
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// announceMediaState tells all clients that userID started or stopped
// a screen share or camera so they can show the LIVE indicator and offer to watch.
func (h *Hub) announceMediaState(roomID, userID uuid.UUID, st sfu.MediaState) {
	h.Broadcast("webrtc_media_state", mediaStatePayload(roomID, userID, st))
}

func mediaStatePayload(roomID, userID uuid.UUID, st sfu.MediaState) map[string]any {
	return map[string]any{"channel_id": roomID, "user_id": userID, "screen": st.Screen, "camera": st.Camera}
}

// announceScreenViewers tells the members of a voice room who watches
// sharer's screen share. Like in Discord, only the room sees it.
func (h *Hub) announceScreenViewers(roomID, sharer uuid.UUID, viewers []uuid.UUID) {
	h.sendToVoiceRoom(roomID, "screen_viewers", screenViewersPayload(roomID, sharer, viewers))
}

func screenViewersPayload(roomID, sharer uuid.UUID, viewers []uuid.UUID) map[string]any {
	if viewers == nil {
		viewers = []uuid.UUID{} // [] rather than null on the wire
	}
	return map[string]any{"channel_id": roomID, "user_id": sharer, "viewers": viewers}
}

// handleSubscribe applies a viewer's choice of which video to receive:
// {kind: "screen"|"camera", user_id, on} for one publisher, or
// {kind: "camera", all: true, on} for every camera.
func (c *Client) handleSubscribe(payload json.RawMessage) {
	req, ok := parseSubscribe(payload, c.User.ID)
	if !ok {
		return
	}
	cur := c.currentVoice()
	if cur == nil || c.hub.SFU == nil {
		return
	}
	room := c.hub.SFU.Room(*cur)
	if room == nil {
		return
	}
	if err := room.Subscribe(c.User.ID, req.publisher, req.kind, req.all, req.on); err != nil {
		slog.Debug("sfu subscribe rejected", "user", c.User.ID, "err", err)
	}
}

// subscribeRequest is a validated webrtc_subscribe.
type subscribeRequest struct {
	kind      sfu.Source
	publisher uuid.UUID // uuid.Nil with all
	all       bool
	on        bool
}

// parseSubscribe validates a webrtc_subscribe payload from viewer: a screen
// or camera of one other user, or every camera at once.
func parseSubscribe(payload json.RawMessage, viewer uuid.UUID) (subscribeRequest, bool) {
	var req struct {
		Kind   string `json:"kind"`
		UserID string `json:"user_id"`
		All    bool   `json:"all"`
		On     bool   `json:"on"`
	}
	if json.Unmarshal(payload, &req) != nil {
		return subscribeRequest{}, false
	}
	kind := sfu.Source(req.Kind)
	if kind != sfu.SourceScreen && kind != sfu.SourceCamera {
		return subscribeRequest{}, false
	}
	if req.All && kind != sfu.SourceCamera {
		return subscribeRequest{}, false
	}
	out := subscribeRequest{kind: kind, all: req.All, on: req.On}
	if !req.All {
		id, err := uuid.Parse(req.UserID)
		if err != nil || id == uuid.Nil || id == viewer {
			return subscribeRequest{}, false
		}
		out.publisher = id
	}
	return out, true
}
