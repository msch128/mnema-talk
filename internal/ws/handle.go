package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/pion/webrtc/v4"
)

func (c *Client) handle(eventType string, payload json.RawMessage) {
	h := c.hub
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var p struct {
		ChannelID uuid.UUID `json:"channel_id"`
		Active    bool      `json:"active"`
		Idle      bool      `json:"idle"`
		T         float64   `json:"t"`
	}
	_ = json.Unmarshal(payload, &p)

	switch eventType {
	case "ping":
		c.SendEvent("pong", map[string]any{"t": p.T})

	case "presence_idle":
		h.setIdle(c, p.Idle)

	case "voice_join":
		if ch, ok := h.voiceChannel(ctx, p.ChannelID); ok {
			h.joinVoice(c, ch)
		}

	case "voice_leave":
		h.leaveCurrentVoice(c)

	case "typing":
		if !c.allowTyping(p.ChannelID, time.Now()) {
			return
		}
		if ch, err := chat.LoadChannel(ctx, h.DB, p.ChannelID); err == nil {
			h.broadcastExcept(c.User.ID, "typing", map[string]any{"channel_id": ch.ID, "user_id": c.User.ID})
		}

	case "voice_speaking":
		if cur := c.currentVoice(); cur != nil {
			// A muted or deafened member is never shown as speaking.
			active := p.Active && !h.isMuted(c.User.ID, *cur)
			if c.speakingChanged(active, time.Now()) {
				h.sendToVoiceRoom(*cur, "voice_speaking", map[string]any{"channel_id": *cur, "user_id": c.User.ID, "active": active})
			}
		}

	case "voice_mute_state":
		var st MuteState
		if json.Unmarshal(payload, &st) == nil {
			h.setMuteState(c, st)
		}

	case "webrtc_answer":
		var answer webrtc.SessionDescription
		if err := json.Unmarshal(payload, &answer); err == nil {
			if peer := c.peer(); peer != nil {
				if err := peer.SetAnswer(answer); err != nil {
					slog.Warn("sfu set remote description, the offer is sent again", "user", c.User.ID, "err", err)
				}
			}
		}

	case "webrtc_candidate":
		var cand webrtc.ICECandidateInit
		if err := json.Unmarshal(payload, &cand); err == nil {
			slog.Debug("sfu remote candidate", "user", c.User.Username, "candidate", cand.Candidate)
			if peer := c.peer(); peer != nil {
				if err := peer.PC.AddICECandidate(cand); err != nil {
					slog.Debug("sfu add ice candidate", "user", c.User.ID, "err", err)
				}
			}
		}

	case "webrtc_diag":
		// A browser's own view of its voice connection, for troubleshooting.
		if len(payload) <= 4096 && c.allowDiag(time.Now()) {
			slog.Debug("webrtc client diag", "user", c.User.Username, "diag", json.RawMessage(payload))
		}

	case "webrtc_request_keyframe":
		if cur := c.currentVoice(); cur != nil && h.SFU != nil {
			if room := h.SFU.Room(*cur); room != nil {
				room.DispatchKeyframe(c.User.ID)
			}
		}

	case "webrtc_subscribe":
		c.handleSubscribe(payload)

	case "webrtc_screenshare_start":
		if cur := c.currentVoice(); cur != nil && h.SFU != nil {
			if room := h.SFU.Room(*cur); room != nil {
				room.DispatchKeyframe(c.User.ID)
			}
		}

	case "webrtc_screenshare_stop":
		if cur := c.currentVoice(); cur != nil && h.SFU != nil {
			if room := h.SFU.Room(*cur); room != nil {
				room.RemoveUserSource(c.User.ID, sfu.SourceScreen)
			}
		}

	case "webrtc_camera_stop":
		if cur := c.currentVoice(); cur != nil && h.SFU != nil {
			if room := h.SFU.Room(*cur); room != nil {
				room.RemoveUserSource(c.User.ID, sfu.SourceCamera)
			}
		}
	}
}
