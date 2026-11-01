package ws

import (
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// announceMediaState tells all clients that userID started or stopped
// a screen share or camera so they can show the LIVE indicator and offer to watch.
func (h *Hub) announceMediaState(roomID, userID uuid.UUID, st sfu.MediaState) {
	h.BroadcastChannel(roomID, events.WebrtcMediaState, mediaStatePayload(roomID, userID, st))
}

func mediaStatePayload(roomID, userID uuid.UUID, st sfu.MediaState) map[string]any {
	return map[string]any{"channel_id": roomID, "user_id": userID, "screen": st.Screen, "camera": st.Camera}
}

// announceScreenViewers tells the members of a voice room who watches
// sharer's screen share. Like in Discord, only the room sees it.
func (h *Hub) announceScreenViewers(roomID, sharer uuid.UUID, viewers []uuid.UUID) {
	h.sendToVoiceRoom(roomID, events.ScreenViewers, screenViewersPayload(roomID, sharer, viewers))
}

func screenViewersPayload(roomID, sharer uuid.UUID, viewers []uuid.UUID) map[string]any {
	if viewers == nil {
		viewers = []uuid.UUID{} // [] rather than null on the wire
	}
	return map[string]any{"channel_id": roomID, "user_id": sharer, "viewers": viewers}
}

func (c *Client) activeVoiceRoom() *sfu.Room {
	if c.hub.SFU == nil {
		return nil
	}
	cur := c.currentVoice()
	if cur == nil {
		return nil
	}
	room := c.hub.SFU.Room(*cur)
	return room
}

func (c *Client) applySubscriptionQuality(room *sfu.Room, req subscribeRequest) {
	if req.hasFit && req.on && req.kind == sfu.SourceScreen {
		if err := room.SetScreenFit(c.User.ID, req.publisher, req.fit); err != nil {
			slog.Debug("sfu screen fit rejected", "user", c.User.ID, "err", err)
		}
	}
	if req.hasQuality && req.on && req.kind == sfu.SourceScreen {
		if err := room.SetScreenQuality(c.User.ID, req.publisher, req.quality); err != nil {
			slog.Debug("sfu screen quality rejected", "user", c.User.ID, "err", err)
		}
	}
}

// handleSubscribe applies a viewer's choice of which video to receive:
// {kind: "screen"|"camera", user_id, on} for one publisher, or
// {kind: "camera", all: true, on} for every camera, or
// {kind: "camera", user_ids: [...], on} for up to 16 cameras at once.
func (c *Client) handleSubscribe(payload json.RawMessage) {
	if ids, on, batch, ok := parseCameraBatch(payload, c.User.ID); batch {
		if ok {
			c.handleCameraBatch(ids, on)
		}
		return
	}
	req, ok := parseSubscribe(payload, c.User.ID)
	if !ok {
		return
	}
	room := c.activeVoiceRoom()
	if room == nil {
		return
	}
	c.applySubscriptionQuality(room, req)
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
	// quality is the layer of a screen share ("auto" is -1); hasQuality
	// tells whether the message chose one.
	quality    int
	hasQuality bool
	// fit is the best layer the viewer's picture of a screen share can use;
	// hasFit tells whether the message said.
	fit    int
	hasFit bool
}

type subscribeWire struct {
	Kind    string          `json:"kind"`
	UserID  string          `json:"user_id"`
	All     bool            `json:"all"`
	On      bool            `json:"on"`
	Quality json.RawMessage `json:"quality"`
	Fit     *int            `json:"fit"`
}

func isValidSubscriptionKind(kind sfu.Source, all bool) bool {
	if kind != sfu.SourceScreen && kind != sfu.SourceCamera {
		return false
	}
	if all && kind != sfu.SourceCamera {
		return false
	}
	return true
}

func validateQuality(raw json.RawMessage, kind sfu.Source) (int, bool, bool) {
	quality, hasQuality, ok := parseQuality(raw)
	if !ok || (hasQuality && kind != sfu.SourceScreen) {
		return 0, false, false
	}
	return quality, hasQuality, true
}

// validateFit reads the optional fit of a screen subscription: the index of
// the best layer the viewer's picture can use.
func validateFit(raw *int, kind sfu.Source) (fit int, present, ok bool) {
	if raw == nil {
		return 0, false, true
	}
	if kind != sfu.SourceScreen || *raw < 0 || *raw >= sfu.MaxLayers {
		return 0, false, false
	}
	return *raw, true, true
}

func parsePublisher(all bool, userID string, viewer uuid.UUID) (uuid.UUID, bool) {
	if all {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(userID)
	if err != nil || id == uuid.Nil || id == viewer {
		return uuid.Nil, false
	}
	return id, true
}

// parseSubscribe validates a webrtc_subscribe payload from viewer: a screen
// or camera of one other user, or every camera at once.
func parseSubscribe(payload json.RawMessage, viewer uuid.UUID) (subscribeRequest, bool) {
	var wire subscribeWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return subscribeRequest{}, false
	}
	kind := sfu.Source(wire.Kind)
	if !isValidSubscriptionKind(kind, wire.All) {
		return subscribeRequest{}, false
	}
	quality, hasQuality, ok := validateQuality(wire.Quality, kind)
	if !ok {
		return subscribeRequest{}, false
	}
	fit, hasFit, ok := validateFit(wire.Fit, kind)
	if !ok {
		return subscribeRequest{}, false
	}
	pub, ok := parsePublisher(wire.All, wire.UserID, viewer)
	if !ok {
		return subscribeRequest{}, false
	}
	return subscribeRequest{
		kind:       kind,
		publisher:  pub,
		all:        wire.All,
		on:         wire.On,
		quality:    quality,
		hasQuality: hasQuality,
		fit:        fit,
		hasFit:     hasFit,
	}, true
}
