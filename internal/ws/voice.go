package ws

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/pion/webrtc/v4"
)

// voiceKey is one user's stay in one voice room. Two tabs of a user can sit
// in different rooms; join time, mute state and grace are tracked per room.
type voiceKey struct {
	user uuid.UUID
	ch   uuid.UUID
}

// MuteState is whether a voice member muted their microphone or deafened.
type MuteState struct {
	Muted    bool `json:"muted"`
	Deafened bool `json:"deafened"`
}

// VoiceUser is a member of a voice room, since when they are in it and
// whether they are muted or deafened.
type VoiceUser struct {
	auth.User
	JoinedAt time.Time `json:"joined_at"`
	MuteState
}

// voiceSnapshot copies the current voice rooms (channel → users).
func (h *Hub) voiceSnapshot() map[uuid.UUID]map[uuid.UUID]VoiceUser {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[uuid.UUID]map[uuid.UUID]VoiceUser, len(h.voice))
	for chID, users := range h.voice {
		cp := make(map[uuid.UUID]VoiceUser, len(users))
		for id, u := range users {
			k := voiceKey{id, chID}
			cp[id] = VoiceUser{User: u.Public(), JoinedAt: h.voiceSince[k], MuteState: h.voiceMute[k]}
		}
		out[chID] = cp
	}
	return out
}

// voiceRooms says since when each voice room is occupied, plus the server's
// clock so clients can count up without trusting their own.
func (h *Hub) voiceRooms(c *Client, snap *perm.Snapshot) map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	started := make(map[uuid.UUID]time.Time, len(h.roomSince))
	for id, t := range h.roomSince {
		started[id] = t
	}
	return map[string]any{"started": visibleTo(c, snap, started), "now": time.Now()}
}

// voiceChannel returns chID if it exists and is a voice channel.
func (h *Hub) voiceChannel(ctx context.Context, chID uuid.UUID) (*chat.ChannelInfo, bool) {
	ch, err := chat.LoadChannel(ctx, h.DB, chID)
	if err != nil || ch.Type != chat.ChannelTypeVoice {
		return nil, false
	}
	return ch, true
}

// applyUserUpdate keeps the hub's copies of a user's public profile current
// after a name, status or avatar change (a user_update with a User payload).
func (h *Hub) applyUserUpdate(payload any) {
	var u auth.User
	switch v := payload.(type) {
	case auth.User:
		u = v
	case *auth.User:
		if v == nil {
			return
		}
		u = *v
	default:
		return // e.g. {id, disabled}
	}
	if u.ID == uuid.Nil {
		return
	}
	pub := u.Public()
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.profiles[u.ID]; ok {
		h.profiles[u.ID] = pub
	}
	for _, users := range h.voice {
		if _, ok := users[u.ID]; ok {
			users[u.ID] = pub
		}
	}
}

// profileLocked is c's user as currently known: the connection's snapshot
// updated by later user_update events. Callers hold h.mu.
func (h *Hub) profileLocked(c *Client) auth.User {
	if p, ok := h.profiles[c.User.ID]; ok {
		return p
	}
	return c.User.Public()
}

// joinVoice puts c into ch's voice room. Re-joining the same room keeps the
// presence entry but renegotiates the SFU peer, which is how a client attaches
// its microphone once getUserMedia resolves.
func (h *Hub) joinVoice(c *Client, ch *chat.ChannelInfo) {
	c.voiceMu.Lock()
	defer c.voiceMu.Unlock()
	if c.closed.Load() || !c.nativeLive() {
		return
	}
	snap, ok := h.permSnapshot(ch.ID)
	if !ok || !c.can(snap, ch.ID, perm.ViewChannel|perm.Connect) {
		slog.Info("voice join refused, missing permission", "user", c.User.Username, "channel", ch.ID)
		c.SendEvent("voice_kicked", map[string]any{"channel_id": ch.ID, "reason": "missing_permission"})
		return
	}
	// A user returning within the grace period resumes silently in the same
	// room; joining a different room ends the lingering presence first.
	rejoin := false
	for _, g := range h.takeUserGraces(c.User.ID) {
		if g.channelID == ch.ID {
			rejoin = true
		} else {
			h.removePresenceUnlessPresent(c.User.ID, g.channelID, c)
		}
	}
	if cur := c.currentVoice(); cur != nil {
		if *cur == ch.ID {
			rejoin = true
			// Already in this room with a live media connection: nothing to
			// renegotiate. (A client that wants a fresh connection leaves
			// first, or reconnects its socket.)
			if h.SFU != nil && c.peer().Healthy() {
				return
			}
		} else {
			h.leaveVoiceLocked(c, *cur)
		}
	}
	key := voiceKey{c.User.ID, ch.ID}
	h.mu.Lock()
	if h.closed || !c.nativeLive() {
		h.mu.Unlock()
		return
	}
	// A full room (the channel's user_limit, set by the admin) turns away
	// newcomers; someone already present (another tab, a reload within the
	// grace period) always gets back in.
	if _, present := h.voice[ch.ID][c.User.ID]; !present && ch.UserLimit > 0 && len(h.voice[ch.ID]) >= ch.UserLimit {
		h.mu.Unlock()
		slog.Info("voice join refused, room full", "user", c.User.Username, "channel", ch.ID, "limit", ch.UserLimit)
		// The client ends the call on voice_kicked; reason tells it why.
		c.SendEvent("voice_kicked", map[string]any{"channel_id": ch.ID, "reason": "room_full"})
		return
	}
	now := time.Now()
	if h.voice[ch.ID] == nil {
		h.voice[ch.ID] = map[uuid.UUID]auth.User{}
	}
	if _, ok := h.roomSince[ch.ID]; !ok {
		h.roomSince[ch.ID] = now
	}
	// Already present through another connection (reconnect, second tab).
	if _, present := h.voice[ch.ID][c.User.ID]; present {
		rejoin = true
	}
	if _, ok := h.voiceSince[key]; !ok || !rejoin {
		h.voiceSince[key] = now
	}
	profile := h.profileLocked(c)
	h.voice[ch.ID][c.User.ID] = profile
	joined := VoiceUser{User: profile, JoinedAt: h.voiceSince[key], MuteState: h.voiceMute[key]}
	roomStarted := h.roomSince[ch.ID]
	h.mu.Unlock()
	id := ch.ID
	c.setVoice(&id)

	if h.afterSeat != nil {
		h.afterSeat() // tests change permissions exactly here
	}
	// A permission change may have run its sweep before this seat existed:
	// check again, on the state as it is now, now that the seat can be found.
	snap, ok = h.permSnapshot(ch.ID)
	if !ok || !c.can(snap, ch.ID, perm.ViewChannel|perm.Connect) {
		slog.Info("voice join revoked, permission changed meanwhile", "user", c.User.Username, "channel", ch.ID)
		h.leaveVoiceLocked(c, ch.ID)
		c.SendEvent("voice_kicked", map[string]any{"channel_id": ch.ID, "reason": "missing_permission"})
		return
	}

	if h.SFU != nil {
		sendOffer := func(offer webrtc.SessionDescription) { c.SendEvent("webrtc_offer", offer) }
		sendICE := func(cand *webrtc.ICECandidateInit) { c.SendEvent("webrtc_candidate", cand) }
		var peer *sfu.Peer
		var err error
		if c.native == nil {
			_, peer, err = h.SFU.Join(ch.ID, c.User.ID, sendOffer, sendICE)
		} else {
			_, peer, err = h.SFU.JoinWithAuthorization(ch.ID, c.User.ID, sendOffer, sendICE, c.nativeLive)
		}
		if err != nil {
			slog.Error("sfu join failed", "user", c.User.ID, "channel", ch.ID, "err", err)
		}
		c.setPeer(peer)
		if h.afterPeer != nil {
			h.afterPeer() // tests change permissions exactly here
		}
		// A sweep between the check and now found a seat without a peer and
		// skipped it: decide again on the state as it is now.
		snap, ok = h.permSnapshot(ch.ID)
		if !ok || !c.can(snap, ch.ID, perm.ViewChannel|perm.Connect) {
			slog.Info("voice join revoked, permission changed meanwhile", "user", c.User.Username, "channel", ch.ID)
			h.leaveVoiceLocked(c, ch.ID)
			c.SendEvent("voice_kicked", map[string]any{"channel_id": ch.ID, "reason": "missing_permission"})
			return
		}
		h.applyMediaPolicy(c, snap)
		// A late joiner learns who shares a screen or runs a camera, and who
		// watches each share.
		if room := h.SFU.Room(ch.ID); room != nil {
			for uid, st := range room.MediaStates() {
				if uid != c.User.ID {
					c.SendEvent("webrtc_media_state", mediaStatePayload(ch.ID, uid, st))
				}
			}
			for sharer, viewers := range room.ScreenViewers() {
				c.SendEvent("screen_viewers", screenViewersPayload(ch.ID, sharer, viewers))
			}
			for sharer, layers := range room.ScreenLayers() {
				c.SendEvent("screen_layers", screenLayersPayload(ch.ID, sharer, layers))
			}
		}
	}
	slog.Info("voice join", "user", c.User.Username, "channel", ch.ID, "rejoin", rejoin)
	if !rejoin {
		if h.Presence != nil {
			h.Presence(c.User.ID, ch.ID, true)
		}
		c.emitApplicationEvent("voice_state_update", map[string]any{"action": "join", "channel_id": ch.ID, "user": joined, "started_at": roomStarted}, false, nil, ch.ID)
	}
}

// leaveCurrentVoice is an explicit leave of whatever room c is in.
func (h *Hub) leaveCurrentVoice(c *Client) {
	c.voiceMu.Lock()
	defer c.voiceMu.Unlock()
	if cur := c.currentVoice(); cur != nil {
		h.leaveVoiceLocked(c, *cur)
	}
}

// leaveVoiceLocked is an explicit leave: presence ends immediately unless
// another connection of the same user is still in the room. Callers hold
// c.voiceMu.
func (h *Hub) leaveVoiceLocked(c *Client, chID uuid.UUID) {
	if h.SFU != nil {
		h.SFU.RemovePeer(chID, c.peer())
	}
	h.takeGrace(voiceKey{c.User.ID, chID})
	c.setVoice(nil)
	h.removePresenceUnlessPresent(c.User.ID, chID, c)
}
