package ws

import (
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/pion/webrtc/v4"
)

// joinVoice puts c into ch's voice room. Re-joining the same room keeps the
// presence entry but renegotiates the SFU peer, which is how a client attaches
// its microphone once getUserMedia resolves.
func (h *Hub) joinVoice(c *Client, ch *chat.ChannelInfo) {
	c.voiceMu.Lock()
	defer c.voiceMu.Unlock()
	if c.closed.Load() || !c.commandsAllowed() {
		return
	}
	snap, ok := h.permSnapshot(ch.ID)
	if !ok || !c.can(snap, ch.ID, perm.ViewChannel|perm.Connect) {
		slog.Info("voice join refused, missing permission", "user", c.User.Username, "channel", ch.ID)
		c.SendEvent("voice_kicked", map[string]any{"channel_id": ch.ID, "reason": "missing_permission"})
		return
	}
	rejoin, done := h.leaveOtherRooms(c, ch.ID)
	if done {
		return
	}
	seat, ok := h.takeSeat(c, ch, rejoin)
	if !ok {
		return
	}
	id := ch.ID
	c.setVoice(&id)

	if h.afterSeat != nil {
		h.afterSeat() // tests change permissions exactly here
	}
	// A permission change may have run its sweep before this seat existed:
	// check again, on the state as it is now, now that the seat can be found.
	if _, ok := h.recheckJoin(c, ch.ID); !ok {
		return
	}
	if h.SFU != nil && !h.attachPeer(c, ch.ID) {
		return
	}
	slog.Info("voice join", "user", c.User.Username, "channel", ch.ID, "rejoin", seat.rejoin)
	if !seat.rejoin {
		if h.Presence != nil {
			h.Presence(c.User.ID, ch.ID, true)
		}
		c.emitApplicationEvent("voice_state_update", map[string]any{"action": "join", "channel_id": ch.ID, "user": seat.joined, "started_at": seat.roomStarted}, false, nil, ch.ID)
	}
}

// leaveOtherRooms settles what c's earlier voice presence means for a join of
// chID. A user returning within the grace period resumes silently in the same
// room; joining a different room ends the lingering presence first. rejoin
// reports that c is already in chID; done that its media connection is live
// too, so there is nothing to do.
func (h *Hub) leaveOtherRooms(c *Client, chID uuid.UUID) (rejoin, done bool) {
	for _, g := range h.takeUserGraces(c.User.ID) {
		if g.channelID == chID {
			rejoin = true
		} else {
			h.removePresenceUnlessPresent(c.User.ID, g.channelID, c)
		}
	}
	cur := c.currentVoice()
	if cur == nil {
		return rejoin, false
	}
	if *cur != chID {
		h.leaveVoiceLocked(c, *cur)
		return rejoin, false
	}
	// Already in this room with a live media connection: nothing to
	// renegotiate. (A client that wants a fresh connection leaves
	// first, or reconnects its socket.)
	return true, h.SFU != nil && c.peer().Healthy()
}

// voiceSeat is what a join leaves behind for its announcement.
type voiceSeat struct {
	rejoin      bool
	joined      VoiceUser
	roomStarted time.Time
}

// takeSeat records c in ch's room. It refuses (telling the client) when the
// hub is closing or the room is full.
func (h *Hub) takeSeat(c *Client, ch *chat.ChannelInfo, rejoin bool) (voiceSeat, bool) {
	key := voiceKey{c.User.ID, ch.ID}
	h.mu.Lock()
	if h.closed || !c.commandsAllowed() {
		h.mu.Unlock()
		return voiceSeat{}, false
	}
	// A full room (the channel's user_limit, set by the admin) turns away
	// newcomers; someone already present (another tab, a reload within the
	// grace period) always gets back in.
	_, present := h.voice[ch.ID][c.User.ID]
	if !present && ch.UserLimit > 0 && len(h.voice[ch.ID]) >= ch.UserLimit {
		h.mu.Unlock()
		slog.Info("voice join refused, room full", "user", c.User.Username, "channel", ch.ID, "limit", ch.UserLimit)
		// The client ends the call on voice_kicked; reason tells it why.
		c.SendEvent("voice_kicked", map[string]any{"channel_id": ch.ID, "reason": "room_full"})
		return voiceSeat{}, false
	}
	now := time.Now()
	if h.voice[ch.ID] == nil {
		h.voice[ch.ID] = map[uuid.UUID]auth.User{}
	}
	if _, ok := h.roomSince[ch.ID]; !ok {
		h.roomSince[ch.ID] = now
	}
	// Already present through another connection (reconnect, second tab).
	rejoin = rejoin || present
	if _, ok := h.voiceSince[key]; !ok || !rejoin {
		h.voiceSince[key] = now
	}
	profile := h.profileLocked(c)
	h.voice[ch.ID][c.User.ID] = profile
	seat := voiceSeat{
		rejoin:      rejoin,
		joined:      VoiceUser{User: profile, JoinedAt: h.voiceSince[key], MuteState: h.voiceMute[key]},
		roomStarted: h.roomSince[ch.ID],
	}
	h.mu.Unlock()
	return seat, true
}

// recheckJoin decides again, on the permission state as it is now, whether c
// may be in chID. If not, the seat is given up and the client told.
func (h *Hub) recheckJoin(c *Client, chID uuid.UUID) (*perm.Snapshot, bool) {
	snap, ok := h.permSnapshot(chID)
	if !ok || !c.can(snap, chID, perm.ViewChannel|perm.Connect) {
		slog.Info("voice join revoked, permission changed meanwhile", "user", c.User.Username, "channel", chID)
		h.leaveVoiceLocked(c, chID)
		c.SendEvent("voice_kicked", map[string]any{"channel_id": chID, "reason": "missing_permission"})
		return nil, false
	}
	return snap, true
}

// attachPeer joins c to the SFU room and brings it up to date. It reports
// false when the permission check after the join revoked the seat.
func (h *Hub) attachPeer(c *Client, chID uuid.UUID) bool {
	sendOffer := func(offer webrtc.SessionDescription) { c.SendEvent("webrtc_offer", offer) }
	sendICE := func(cand *webrtc.ICECandidateInit) { c.SendEvent("webrtc_candidate", cand) }
	var peer *sfu.Peer
	var err error
	if c.native == nil {
		_, peer, err = h.SFU.Join(chID, c.User.ID, sendOffer, sendICE)
	} else {
		_, peer, err = h.SFU.JoinWithAuthorization(chID, c.User.ID, sendOffer, sendICE, c.commandsAllowed)
	}
	if err != nil {
		slog.Error("sfu join failed", "user", c.User.ID, "channel", chID, "err", err)
	}
	c.setPeer(peer)
	if h.afterPeer != nil {
		h.afterPeer() // tests change permissions exactly here
	}
	// A sweep between the check and now found a seat without a peer and
	// skipped it: decide again on the state as it is now.
	snap, ok := h.recheckJoin(c, chID)
	if !ok {
		return false
	}
	h.applyMediaPolicy(c, snap)
	h.sendRoomState(c, chID)
	return true
}

// sendRoomState tells a late joiner who shares a screen or runs a camera, and
// who watches each share.
func (h *Hub) sendRoomState(c *Client, chID uuid.UUID) {
	room := h.SFU.Room(chID)
	if room == nil {
		return
	}
	for uid, st := range room.MediaStates() {
		if uid != c.User.ID {
			c.SendEvent("webrtc_media_state", mediaStatePayload(chID, uid, st))
		}
	}
	for sharer, viewers := range room.ScreenViewers() {
		c.SendEvent("screen_viewers", screenViewersPayload(chID, sharer, viewers))
	}
	for sharer, layers := range room.ScreenLayers() {
		c.SendEvent("screen_layers", screenLayersPayload(chID, sharer, layers))
	}
}
