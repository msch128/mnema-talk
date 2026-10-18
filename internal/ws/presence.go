package ws

import (
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// StatusOffline is the live status of a user without an open connection.
const StatusOffline = "offline"

// statusLocked is userID's live status: offline without a connection, else
// the chosen presence, where "online" reads as "away" while every connection
// of the user is idle. Callers hold h.mu.
func (h *Hub) statusLocked(userID uuid.UUID) string {
	if h.online[userID] <= 0 {
		return StatusOffline
	}
	chosen := h.chosen[userID]
	if chosen == "" {
		chosen = auth.PresenceOnline
	}
	if chosen != auth.PresenceOnline {
		return chosen
	}
	if h.active[userID] > 0 {
		return auth.PresenceOnline
	}
	return auth.PresenceAway
}

// announcePresence tells everyone about a changed live status.
func (h *Hub) announcePresence(userID uuid.UUID, before, after string) {
	if before != after {
		h.Broadcast("presence_update", map[string]any{"user_id": userID, "status": after})
	}
}

// SetPresence applies a newly chosen presence to userID's live status.
func (h *Hub) SetPresence(userID uuid.UUID, presence string) {
	if !auth.ValidPresence(presence) {
		return
	}
	h.mu.Lock()
	before := h.statusLocked(userID)
	if h.online[userID] > 0 {
		h.chosen[userID] = presence
	}
	after := h.statusLocked(userID)
	h.mu.Unlock()
	h.announcePresence(userID, before, after)
}

// setIdle records whether c has gone idle and announces a resulting change.
func (h *Hub) setIdle(c *Client, idle bool) {
	h.mu.Lock()
	if _, ok := h.clients[c]; !ok || !c.nativeLive() {
		h.mu.Unlock()
		return
	}
	before := h.statusLocked(c.User.ID)
	if c.idle != idle {
		c.idle = idle
		if idle {
			h.active[c.User.ID]--
		} else {
			h.active[c.User.ID]++
		}
	}
	after := h.statusLocked(c.User.ID)
	h.mu.Unlock()
	if before != after {
		c.emitApplicationEvent("presence_update", map[string]any{"user_id": c.User.ID, "status": after}, false, nil, uuid.Nil)
	}
}

// SetBotPresence records whether a bot has a live gateway connection and
// announces the change. Bots are online or offline only; they are not counted
// by OnlineCount or OnlineUserIDs, so @here and the user count stay about
// people.
func (h *Hub) SetBotPresence(userID uuid.UUID, online bool) {
	h.mu.Lock()
	had := h.botLive[userID]
	if online {
		h.botLive[userID] = true
	} else {
		delete(h.botLive, userID)
	}
	h.mu.Unlock()
	if had != online {
		status := StatusOffline
		if online {
			status = auth.PresenceOnline
		}
		h.Broadcast("presence_update", map[string]any{"user_id": userID, "status": status})
	}
}

// OnlineCount returns the number of distinct online users.
func (h *Hub) OnlineCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.online)
}

// presenceSnapshot maps every connected user to their live status.
func (h *Hub) presenceSnapshot() map[uuid.UUID]string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[uuid.UUID]string, len(h.online)+len(h.botLive))
	for id := range h.online {
		out[id] = h.statusLocked(id)
	}
	for id := range h.botLive {
		out[id] = auth.PresenceOnline
	}
	return out
}

// OnlineUserIDs lists every user with an open connection (for @here).
func (h *Hub) OnlineUserIDs() []uuid.UUID {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]uuid.UUID, 0, len(h.online))
	for id := range h.online {
		ids = append(ids, id)
	}
	return ids
}
