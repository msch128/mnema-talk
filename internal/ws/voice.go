package ws

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
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
func (h *Hub) voiceRooms() map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	started := make(map[uuid.UUID]time.Time, len(h.roomSince))
	for id, t := range h.roomSince {
		started[id] = t
	}
	return map[string]any{"started": started, "now": time.Now()}
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
	if c.closed.Load() {
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
		} else {
			h.leaveVoiceLocked(c, *cur)
		}
	}
	key := voiceKey{c.User.ID, ch.ID}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
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

	if h.SFU != nil {
		_, peer, err := h.SFU.Join(ch.ID, c.User.ID,
			func(offer webrtc.SessionDescription) { c.SendEvent("webrtc_offer", offer) },
			func(cand *webrtc.ICECandidateInit) { c.SendEvent("webrtc_candidate", cand) })
		if err != nil {
			slog.Error("sfu join failed", "user", c.User.ID, "channel", ch.ID, "err", err)
		}
		c.setPeer(peer)
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
		}
	}
	slog.Info("voice join", "user", c.User.Username, "channel", ch.ID, "rejoin", rejoin)
	if !rejoin {
		h.Broadcast("voice_state_update", map[string]any{"action": "join", "channel_id": ch.ID, "user": joined, "started_at": roomStarted})
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

// dropVoiceForGrace ends c's media connection when its socket goes away.
// Presence lingers for the grace period so a quick reconnect resumes the call
// seamlessly.
func (h *Hub) dropVoiceForGrace(c *Client) {
	c.voiceMu.Lock()
	defer c.voiceMu.Unlock()
	ch := c.currentVoice()
	if ch == nil {
		return
	}
	if h.SFU != nil {
		h.SFU.RemovePeer(*ch, c.peer())
	}
	c.setVoice(nil)
	// A reconnect may already be back in the room before the old socket
	// unregisters; then the user never left and no grace timer runs.
	if !h.userInVoice(c.User.ID, *ch, c) {
		h.startGrace(voiceKey{c.User.ID, *ch})
	}
}

// kickClient takes c out of its voice room and tells it so. It reports
// whether c was in a room (only chID's room when onlyCh is set).
func (h *Hub) kickClient(c *Client, onlyCh *uuid.UUID) bool {
	c.voiceMu.Lock()
	cur := c.currentVoice()
	if cur == nil || (onlyCh != nil && *cur != *onlyCh) {
		c.voiceMu.Unlock()
		return false
	}
	ch := *cur
	h.leaveVoiceLocked(c, ch)
	c.voiceMu.Unlock()
	c.SendEvent("voice_kicked", map[string]any{"channel_id": ch})
	return true
}

// clientsOf lists the connections of userID.
func (h *Hub) clientsOf(userID uuid.UUID) []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []*Client
	for c := range h.clients {
		if c.User.ID == userID {
			out = append(out, c)
		}
	}
	return out
}

// KickFromVoice ends userID's voice presence on every connection and in a
// pending grace period. It reports whether the user was in a voice room.
func (h *Hub) KickFromVoice(userID uuid.UUID) bool {
	kicked := false
	for _, c := range h.clientsOf(userID) {
		if h.kickClient(c, nil) {
			kicked = true
		}
	}
	for _, g := range h.takeUserGraces(userID) {
		h.removePresence(userID, g.channelID)
		kicked = true
	}
	return kicked
}

// CloseVoiceChannel empties a voice room that no longer exists (its channel
// was deleted): every member is disconnected from it and told so, lingering
// presence ends and the SFU room is closed.
func (h *Hub) CloseVoiceChannel(chID uuid.UUID) {
	h.mu.RLock()
	all := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		all = append(all, c)
	}
	h.mu.RUnlock()
	for _, c := range all {
		h.kickClient(c, &chID)
	}

	h.mu.Lock()
	var graces []voiceKey
	for k, g := range h.grace {
		if k.ch == chID {
			g.timer.Stop()
			delete(h.grace, k)
			graces = append(graces, k)
		}
	}
	var rest []uuid.UUID
	for id := range h.voice[chID] {
		rest = append(rest, id)
	}
	h.mu.Unlock()
	for _, k := range graces {
		h.removePresence(k.user, chID)
	}
	for _, id := range rest {
		h.removePresence(id, chID)
	}
	if h.SFU != nil {
		h.SFU.CloseRoom(chID)
	}
}

// DisconnectUser closes every live connection of userID.
func (h *Hub) DisconnectUser(userID uuid.UUID) {
	for _, c := range h.clientsOf(userID) {
		c.close()
	}
}

// userInVoice reports whether another connection than except of userID is in chID's voice room.
func (h *Hub) userInVoice(userID, chID uuid.UUID, except *Client) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for other := range h.clients {
		if other == except || other.User.ID != userID {
			continue
		}
		if cur := other.currentVoice(); cur != nil && *cur == chID {
			return true
		}
	}
	return false
}

// removePresenceUnlessPresent ends userID's presence in chID unless another
// connection than except (a second tab) is still in the room.
func (h *Hub) removePresenceUnlessPresent(userID, chID uuid.UUID, except *Client) {
	if !h.userInVoice(userID, chID, except) {
		h.removePresence(userID, chID)
	}
}

// removePresence drops userID from chID's room and announces it if they were there.
func (h *Hub) removePresence(userID, chID uuid.UUID) {
	key := voiceKey{userID, chID}
	h.mu.Lock()
	_, wasIn := h.voice[chID][userID]
	since, hadSince := h.voiceSince[key]
	if wasIn {
		delete(h.voiceSince, key)
		delete(h.voiceMute, key)
	}
	if users, ok := h.voice[chID]; ok {
		delete(users, userID)
		if len(users) == 0 {
			delete(h.voice, chID)
			delete(h.roomSince, chID)
		}
	}
	h.mu.Unlock()
	if wasIn {
		slog.Info("voice leave", "user", userID, "channel", chID)
		h.Broadcast("voice_state_update", map[string]any{"action": "leave", "channel_id": chID, "user_id": userID})
		if hadSince {
			h.addVoiceTime(userID, time.Since(since))
		}
	}
}

// addVoiceTime adds one finished stay in a voice room to the user's total and
// tells everyone the new total.
func (h *Hub) addVoiceTime(userID uuid.UUID, d time.Duration) {
	secs := int64(d / time.Second)
	if h.DB == nil || secs <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var total int64
	err := h.DB.QueryRow(ctx, `UPDATE users SET voice_seconds = voice_seconds + $1 WHERE id = $2 RETURNING voice_seconds`,
		secs, userID).Scan(&total)
	if err != nil {
		slog.Warn("record voice time", "user", userID, "err", err)
		return
	}
	h.Broadcast("user_stats", map[string]any{"user_id": userID, "voice_seconds": total})
}

// startGrace keeps a user listed in a room for VoiceGrace, then removes them
// unless takeGrace claimed the slot first.
func (h *Hub) startGrace(key voiceKey) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	if old := h.grace[key]; old != nil {
		old.timer.Stop()
	}
	g := &graceLeave{channelID: key.ch}
	g.timer = time.AfterFunc(h.VoiceGrace, func() {
		h.mu.Lock()
		current := h.grace[key] == g
		if current {
			delete(h.grace, key)
		}
		h.mu.Unlock()
		if current {
			h.removePresence(key.user, key.ch)
		}
	})
	h.grace[key] = g
}

// takeGrace cancels and returns a pending grace leave, if any.
func (h *Hub) takeGrace(key voiceKey) *graceLeave {
	h.mu.Lock()
	defer h.mu.Unlock()
	g := h.grace[key]
	if g != nil {
		g.timer.Stop()
		delete(h.grace, key)
	}
	return g
}

// takeUserGraces cancels and returns every pending grace leave of userID.
func (h *Hub) takeUserGraces(userID uuid.UUID) []*graceLeave {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*graceLeave
	for k, g := range h.grace {
		if k.user == userID {
			g.timer.Stop()
			delete(h.grace, k)
			out = append(out, g)
		}
	}
	return out
}

// sendToVoiceRoom sends an event to the connections currently in chID's room.
func (h *Hub) sendToVoiceRoom(chID uuid.UUID, eventType string, payload any) {
	data := encode(eventType, payload)
	if data == nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if cur := c.currentVoice(); cur != nil && *cur == chID {
			c.deliver(data)
		}
	}
}

func (h *Hub) isMuted(userID, chID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	st := h.voiceMute[voiceKey{userID, chID}]
	return st.Muted || st.Deafened
}

// setMuteState records c's mute/deafen state and tells everyone, so the
// microphone and headphone marks show on their avatar. Deafened implies muted.
func (h *Hub) setMuteState(c *Client, st MuteState) {
	cur := c.currentVoice()
	if cur == nil {
		return
	}
	if st.Deafened {
		st.Muted = true
	}
	key := voiceKey{c.User.ID, *cur}
	h.mu.Lock()
	changed := h.voiceMute[key] != st
	h.voiceMute[key] = st
	h.mu.Unlock()
	if !changed {
		return
	}
	h.Broadcast("voice_mute_state", map[string]any{"channel_id": *cur, "user_id": c.User.ID, "muted": st.Muted, "deafened": st.Deafened})
	if st.Muted && c.speakingChanged(false, time.Now()) {
		h.sendToVoiceRoom(*cur, "voice_speaking", map[string]any{"channel_id": *cur, "user_id": c.User.ID, "active": false})
	}
}
