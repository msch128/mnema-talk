package ws

import (
	"github.com/google/uuid"
)

// kickClient takes c out of its voice room and tells it so. It reports
// whether c was in a room (only chID's room when onlyCh is set).
func (h *Hub) kickClient(c *Client, onlyCh *uuid.UUID) bool {
	return h.kickClientReason(c, onlyCh, "")
}

// kickClientReason is kickClient telling the client why (voice_kicked
// reason), e.g. "missing_permission".
func (h *Hub) kickClientReason(c *Client, onlyCh *uuid.UUID, reason string) bool {
	c.voiceMu.Lock()
	cur := c.currentVoice()
	if cur == nil || (onlyCh != nil && *cur != *onlyCh) {
		c.voiceMu.Unlock()
		return false
	}
	ch := *cur
	h.leaveVoiceLocked(c, ch)
	c.voiceMu.Unlock()
	payload := map[string]any{"channel_id": ch}
	if reason != "" {
		payload["reason"] = reason
	}
	c.SendEvent("voice_kicked", payload)
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
	h.mu.Lock()
	var clients []*Client
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
	h.mu.Unlock()
	for _, c := range clients {
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
