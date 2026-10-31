package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/pion/webrtc/v4"
)

// clientEvent is the part of a client frame every handler may read.
type clientEvent struct {
	ChannelID uuid.UUID `json:"channel_id"`
	Active    bool      `json:"active"`
	Idle      bool      `json:"idle"`
	Takeover  bool      `json:"takeover"`
	Resume    bool      `json:"resume"`
	Tab       string    `json:"tab"`
	T         float64   `json:"t"`
}

func (c *Client) handle(eventType string, payload json.RawMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var p clientEvent
	_ = json.Unmarshal(payload, &p)

	switch eventType {
	case events.ClientPing:
		c.SendEvent(events.Pong, map[string]any{"t": p.T})
	case events.ClientPresenceIdle:
		c.hub.setIdle(c, p.Idle)
	case events.ClientVoiceJoin:
		c.handleVoiceJoin(ctx, p)
	case events.ClientVoiceLeave:
		// A leave is always honoured, but it spends from the same allowance
		// as joins, so leave/join cycles stay bounded.
		c.hub.voiceGate.take(c.User.ID, time.Now())
		c.hub.leaveCurrentVoice(c)
	case events.ClientTyping:
		c.handleTyping(ctx, p.ChannelID)
	case events.ClientVoiceSpeaking:
		c.handleSpeaking(p.Active)
	case events.ClientVoiceMuteState:
		var st MuteState
		if json.Unmarshal(payload, &st) == nil {
			c.hub.setMuteState(c, st)
		}
	case events.ClientStageHand:
		c.handleStageHand(payload)
	case events.ClientStageTalkPower:
		c.handleStageTalkPower(payload)
	case events.ClientTalkReaction:
		c.handleTalkReaction(ctx, payload)
	case events.ClientWebrtcAnswer:
		c.handleAnswer(payload)
	case events.ClientWebrtcCandidate:
		c.handleCandidate(payload)
	case events.ClientWebrtcDiag:
		c.handleDiag(payload)
	case events.ClientWebrtcRequestKeyframe:
		c.handleRequestKeyframe(payload)
	case events.ClientWebrtcSubscribe:
		c.handleSubscribe(payload)
	case events.ClientWebrtcLayers:
		c.handleLayers(payload)
	case events.ClientWebrtcScreenshareStart:
		// The sharer's own screen needs a fresh keyframe for its viewers
		// (DispatchKeyframe would ask for the videos the sharer watches).
		if room := c.currentRoom(); room != nil {
			room.RequestSourceKeyframe(c.User.ID, sfu.SourceScreen)
		}
	case events.ClientWebrtcScreenshareStop:
		c.removeOwnSource(sfu.SourceScreen)
	case events.ClientWebrtcCameraStop:
		c.removeOwnSource(sfu.SourceCamera)
	case events.ClientMediaHandshake:
		c.handleMediaHandshake(payload)
	}
}

// ownKeyframeSource reads the optional {source} of webrtc_request_keyframe:
// "screen" or "camera" asks for a keyframe of the sender's own video of that
// source (after it changed size or device); without it the request is for the
// videos the sender watches.
func ownKeyframeSource(payload json.RawMessage) (sfu.Source, bool) {
	var req struct {
		Source string `json:"source"`
	}
	if json.Unmarshal(payload, &req) != nil {
		return "", false
	}
	switch sfu.Source(req.Source) {
	case sfu.SourceScreen:
		return sfu.SourceScreen, true
	case sfu.SourceCamera:
		return sfu.SourceCamera, true
	}
	return "", false
}

func (c *Client) handleRequestKeyframe(payload json.RawMessage) {
	room := c.currentRoom()
	if room == nil {
		return
	}
	if source, own := ownKeyframeSource(payload); own {
		room.RequestSourceKeyframe(c.User.ID, source)
		return
	}
	room.DispatchKeyframe(c.User.ID)
}

// currentRoom is the SFU room c is seated in, or nil when it is in none, the
// SFU is off.
func (c *Client) currentRoom() *sfu.Room {
	cur := c.currentVoice()
	if cur == nil || c.hub.SFU == nil {
		return nil
	}
	return c.hub.SFU.Room(*cur)
}

func (c *Client) removeOwnSource(source sfu.Source) {
	if room := c.currentRoom(); room != nil {
		room.RemoveUserSource(c.User.ID, source)
	}
}

func (c *Client) handleVoiceJoin(ctx context.Context, p clientEvent) {
	h := c.hub
	channelID := p.ChannelID
	if len(p.Tab) <= 64 {
		c.setTab(p.Tab)
	}
	if !h.voiceGate.take(c.User.ID, time.Now()) {
		// Over the join/leave allowance: refused, and the client ends
		// the attempt on voice_kicked.
		metrics.WSDropped.With("voice_churn").Inc()
		c.SendEvent(events.VoiceKicked, map[string]any{"channel_id": channelID})
		return
	}
	ch, ok := h.voiceChannel(ctx, channelID)
	if !ok {
		return
	}
	if ch.IsLobby && h.Lobby != nil {
		h.enterLobby(c, ch)
		return
	}
	switch {
	case p.Takeover:
		h.takeOverVoice(c, ch)
	case p.Resume:
		h.resumeVoice(c, ch)
	default:
		h.joinVoice(c, ch)
	}
}

func (c *Client) handleTyping(ctx context.Context, channelID uuid.UUID) {
	if !c.allowTyping(channelID, time.Now()) {
		return
	}
	// Only someone who could send a message shows as typing, and only to
	// those who can see the channel.
	if c.canTypeInChannel(ctx, channelID) {
		c.emitApplicationEvent(events.Typing, map[string]any{"channel_id": channelID, "user_id": c.User.ID}, true, nil, channelID)
	}
}

func (c *Client) canTypeInChannel(ctx context.Context, channelID uuid.UUID) bool {
	snap, ok := c.hub.permSnapshot(channelID)
	if !ok || !c.hub.channelKnown(ctx, snap, channelID) {
		return false
	}
	return c.can(snap, channelID, perm.ViewChannel|perm.SendMessages)
}

func (c *Client) handleSpeaking(active bool) {
	cur := c.currentVoice()
	if cur == nil {
		return
	}
	// A muted or deafened member is never shown as speaking.
	active = active && !c.hub.isMuted(c.User.ID, *cur)
	if c.speakingChanged(active, time.Now()) {
		c.emitApplicationEvent(events.VoiceSpeaking, map[string]any{"channel_id": *cur, "user_id": c.User.ID, "active": active}, false, cur, uuid.Nil)
	}
}

func (c *Client) handleAnswer(payload json.RawMessage) {
	var answer webrtc.SessionDescription
	if json.Unmarshal(payload, &answer) != nil {
		return
	}
	peer := c.peer()
	if peer == nil {
		return
	}
	if err := peer.SetAnswer(answer); err != nil {
		slog.Warn("sfu set remote description, the offer is sent again", "user", c.User.ID, "err", err)
	}
}

func (c *Client) handleCandidate(payload json.RawMessage) {
	var cand webrtc.ICECandidateInit
	if json.Unmarshal(payload, &cand) != nil {
		return
	}
	slog.Debug("sfu remote candidate", "user", c.User.Username, "candidate", cand.Candidate)
	if peer := c.peer(); peer != nil {
		// Held until the answer is applied; a rejected candidate is
		// logged by the SFU with its reason (rate-limited).
		_ = peer.AddRemoteCandidate(cand)
	}
}

// handleDiag logs a browser's own view of its voice connection, for
// troubleshooting.
func (c *Client) handleDiag(payload json.RawMessage) {
	if len(payload) > payloadCap(events.ClientWebrtcDiag) || !c.allowDiag(time.Now()) {
		return
	}
	// A browser whose ICE failed is the operator's lead for "no audio": it
	// is logged at warn so the default level shows it, together with the
	// candidate types it gathered and how many ICE servers it had.
	var d struct {
		ICE string `json:"ice"`
	}
	level := slog.LevelDebug
	if json.Unmarshal(payload, &d) == nil && d.ICE == "failed" {
		level = slog.LevelWarn
	}
	slog.Log(context.Background(), level, "webrtc client diag", "user", c.User.Username, "diag", json.RawMessage(payload))
}

// channelKnown reports whether the channel exists. The permission snapshot
// knows which do, so no query is needed per typing notice; without a
// permission model (tests) the database is asked.
func (h *Hub) channelKnown(ctx context.Context, snap *perm.Snapshot, ch uuid.UUID) bool {
	if snap == nil {
		_, err := chat.LoadChannel(ctx, h.DB, ch)
		return err == nil
	}
	return snap.KnowsChannel(ch) || snap.KnowsDM(ch)
}
