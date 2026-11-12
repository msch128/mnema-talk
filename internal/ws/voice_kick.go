package ws

import (
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/safego"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// kickClient takes c out of its voice room and tells it so. It reports
// whether c was in a room (only chID's room when onlyCh is set).
func (h *Hub) kickClient(c *Client, onlyCh *uuid.UUID) bool {
	return h.kickClientReason(c, onlyCh, "")
}

// kickClientReason is kickClient telling the client why (voice_kicked
// reason), e.g. "missing_permission".
func (h *Hub) kickClientReason(c *Client, onlyCh *uuid.UUID, reason string) bool {
	return h.kickClientPeer(c, onlyCh, reason, nil)
}

// kickLostPeer takes c out of its voice room because the SFU gave up on its
// media connection peer, and tells it (voice_kicked, reason
// "connection_lost"). A client that has meanwhile rejoined over another
// peer is left alone.
func (h *Hub) kickLostPeer(c *Client, peer *sfu.Peer) bool {
	return h.kickClientPeer(c, nil, "connection_lost", peer)
}

// kickClientPeer is kickClientReason limited, when peer is set, to a client
// whose current media peer it is.
func (h *Hub) kickClientPeer(c *Client, onlyCh *uuid.UUID, reason string, peer *sfu.Peer) bool {
	var ch uuid.UUID
	kicked := false
	safego.Locked(&c.voiceMu, func() {
		cur := c.currentVoice()
		if cur == nil || (onlyCh != nil && *cur != *onlyCh) || (peer != nil && c.peer() != peer) {
			return
		}
		ch, kicked = *cur, true
		h.leaveVoiceLocked(c, ch)
	})
	if !kicked {
		return false
	}
	c.SendEvent(wire.VoiceKicked{ChannelID: ch, Reason: reason})
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

// VoiceMembers lists who is in the voice room of a channel now.
func (h *Hub) VoiceMembers(chID uuid.UUID) []uuid.UUID {
	room := h.voiceSnapshot()[chID]
	out := make([]uuid.UUID, 0, len(room))
	for id := range room {
		out = append(out, id)
	}
	return out
}

// CloseVoiceChannel empties a voice room that no longer exists (its channel
// was deleted): every member is disconnected from it and told so, lingering
// presence ends and the SFU room is closed.
func (h *Hub) CloseVoiceChannel(chID uuid.UUID) {
	h.kickAllFromChannel(chID)
	graces, rest := h.drainChannelPresence(chID)
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

func (h *Hub) kickAllFromChannel(chID uuid.UUID) {
	h.mu.RLock()
	all := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		all = append(all, c)
	}
	h.mu.RUnlock()
	for _, c := range all {
		h.kickClient(c, &chID)
	}
}

func (h *Hub) drainChannelPresence(chID uuid.UUID) ([]voiceKey, []uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
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
	return graces, rest
}

// EndingUser marks every open connection of userID as ending: the account is
// being banned or kicked, so the connections stay open to flush the notice but
// drop what they receive and cannot take a seat in a Talk. DisconnectUser
// closes them a moment later.
func (h *Hub) EndingUser(userID uuid.UUID) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, set := range []map[*Client]struct{}{h.clients, h.pending} {
		for c := range set {
			if c.User.ID == userID {
				c.ending.Store(true)
			}
		}
	}
}

// DisconnectUser closes every live connection of userID.
func (h *Hub) DisconnectUser(userID uuid.UUID) {
	var clients []*Client
	safego.Locked(&h.mu, func() {
		for c := range h.clients {
			if c.User.ID == userID {
				c.shutdown()
				clients = append(clients, c)
			}
		}
		for c := range h.pending {
			if c.User.ID == userID {
				delete(h.pending, c)
				c.shutdown()
				clients = append(clients, c)
			}
		}
	})
	for _, c := range clients {
		c.close()
	}
}

// enforceFailClosed runs the enforcement fn for userIDs (nil: everybody). The
// change that asked for it is committed already and nothing else re-checks
// live connections, so a panic part-way must not leave members with a seat or
// a right the change took away. The panic is counted and logged, then the
// members concerned lose their connections and seats, which are checked again
// when they reconnect and rejoin. The caller goes on: the request that made
// the change is not turned into an error after it was saved. A panic in the
// fail-closed clean-up itself is not recovered here.
func (h *Hub) enforceFailClosed(userIDs []uuid.UUID, fn func()) {
	safego.Run("ws_perm_enforce", func() { h.disconnectForRecheck(userIDs) }, fn)
}

// disconnectForRecheck closes every connection of userIDs (nil: everybody)
// and ends their Talk seats, so nobody keeps what an interrupted enforcement
// did not get to take away. Every member and every seat is handled on its own
// under safego.Run: a panic for one is counted and logged and spares nobody
// else. A seat is ended by kickUserFromChannel, which also removes one that
// has no connection and no grace period.
func (h *Hub) disconnectForRecheck(userIDs []uuid.UUID) {
	users, seats := h.recheckTargets(wantUserIDs(userIDs))
	for id := range users {
		safego.Run("ws_perm_enforce_conn", safego.Abandon, func() { h.DisconnectUser(id) })
	}
	for key := range seats {
		safego.Run("ws_perm_enforce_seat", safego.Abandon, func() { h.kickUserFromChannel(key.user, key.ch, "missing_permission") })
	}
}

// recheckTargets lists the wanted users (nil: all) who are connected or hold
// a seat, and the seats (listed or in a grace period) they hold.
func (h *Hub) recheckTargets(want map[uuid.UUID]bool) (map[uuid.UUID]struct{}, map[voiceKey]struct{}) {
	users := map[uuid.UUID]struct{}{}
	seats := map[voiceKey]struct{}{}
	for _, c := range h.clientsMatching(want) {
		users[c.User.ID] = struct{}{}
	}
	add := func(key voiceKey) {
		if want == nil || want[key.user] {
			users[key.user] = struct{}{}
			seats[key] = struct{}{}
		}
	}
	safego.Locked(h.mu.RLocker(), func() {
		for ch, room := range h.voice {
			for id := range room {
				add(voiceKey{id, ch})
			}
		}
		for key := range h.grace {
			add(key)
		}
	})
	return users, seats
}
