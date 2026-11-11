package ws

import (
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/safego"
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
		h.Broadcast(PresenceUpdate{UserID: userID, Status: after})
	}
}

// SetPresence applies a newly chosen presence to userID's live status.
func (h *Hub) SetPresence(userID uuid.UUID, presence string) {
	if !auth.ValidPresence(presence) {
		return
	}
	var before, after string
	safego.Locked(&h.mu, func() {
		before = h.statusLocked(userID)
		if h.online[userID] > 0 {
			h.chosen[userID] = presence
		}
		after = h.statusLocked(userID)
	})
	h.announcePresence(userID, before, after)
}

// setIdle records whether c has gone idle and announces a resulting change.
func (h *Hub) setIdle(c *Client, idle bool) {
	var before, after string
	safego.Locked(&h.mu, func() {
		if _, ok := h.clients[c]; !ok {
			return
		}
		before = h.statusLocked(c.User.ID)
		if c.idle != idle {
			c.idle = idle
			if idle {
				h.active[c.User.ID]--
			} else {
				h.active[c.User.ID]++
			}
		}
		after = h.statusLocked(c.User.ID)
	})
	if before != after {
		c.emitApplicationEvent(PresenceUpdate{UserID: c.User.ID, Status: after}, false, nil, uuid.Nil)
	}
}

// BotActivity is what a bot says it is doing (Discord's activity types).
type BotActivity struct {
	Type int    `json:"type"`
	Name string `json:"name"`
}

// botState is the status a bot set itself: online, idle, dnd or invisible
// (Discord's names), and an optional activity.
type botState struct {
	status   string
	activity *BotActivity
}

// botVisibleLocked is the live status members see for a bot, and its
// activity: offline without a gateway connection and when the bot is
// invisible; idle is shown as away. Callers hold h.mu.
func (h *Hub) botVisibleLocked(id uuid.UUID) (string, *BotActivity) {
	status := h.botVisibleStatusLocked(id)
	if status == StatusOffline {
		return StatusOffline, nil
	}
	return status, h.botState[id].activity
}

func (h *Hub) botVisibleStatusLocked(id uuid.UUID) string {
	if !h.botLive[id] {
		return StatusOffline
	}
	st := h.botState[id]
	switch st.status {
	case "invisible":
		return StatusOffline
	case "idle":
		return auth.PresenceAway
	case auth.PresenceDND:
		return auth.PresenceDND
	}
	return auth.PresenceOnline
}

func botPresenceEvent(id uuid.UUID, status string, activity *BotActivity) PresenceUpdate {
	return PresenceUpdate{UserID: id, Status: status, Activity: activity}
}

// SetBotPresence records whether a bot has a live gateway connection and
// announces the change. A bot is online unless it set another status; it is
// not counted by OnlineCount or OnlineUserIDs, so @here and the user count
// stay about people. The status it set ends with its last connection.
func (h *Hub) SetBotPresence(userID uuid.UUID, online bool) {
	var before, after string
	var beforeAct, act *BotActivity
	safego.Locked(&h.mu, func() {
		before, beforeAct = h.botVisibleLocked(userID)
		if online {
			h.botLive[userID] = true
		} else {
			delete(h.botLive, userID)
			delete(h.botState, userID)
		}
		after, act = h.botVisibleLocked(userID)
	})
	if before != after || !sameActivity(beforeAct, act) {
		h.Broadcast(botPresenceEvent(userID, after, act))
	}
}

// SetBotStatus applies the status a bot set itself (online, idle, dnd or
// invisible) with an optional activity, and announces what members see. It
// may arrive before the bot counts as connected; the state is then kept.
func (h *Hub) SetBotStatus(userID uuid.UUID, status string, activity *BotActivity) {
	var before, after string
	var beforeAct, act *BotActivity
	safego.Locked(&h.mu, func() {
		before, beforeAct = h.botVisibleLocked(userID)
		h.botState[userID] = botState{status: status, activity: activity}
		after, act = h.botVisibleLocked(userID)
	})
	if before != after || !sameActivity(beforeAct, act) {
		h.Broadcast(botPresenceEvent(userID, after, act))
	}
}

func sameActivity(a, b *BotActivity) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// sendBotActivities tells a new connection what the connected bots are doing:
// the presence snapshot only carries statuses.
func (h *Hub) sendBotActivities(c *Client) {
	var presences []PresenceUpdate
	safego.Locked(h.mu.RLocker(), func() {
		for id := range h.botState {
			if status, act := h.botVisibleLocked(id); act != nil {
				presences = append(presences, botPresenceEvent(id, status, act))
			}
		}
	})
	for _, ev := range presences {
		c.SendEvent(ev)
	}
}

// OnlineCount returns the number of distinct online users.
func (h *Hub) OnlineCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.online)
}

// presenceSnapshot maps every connected user to their live status.
func (h *Hub) presenceSnapshot() PresenceSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(PresenceSnapshot, len(h.online)+len(h.botLive))
	for id := range h.online {
		out[id] = h.statusLocked(id)
	}
	for id := range h.botLive {
		if status := h.botVisibleStatusLocked(id); status != StatusOffline {
			out[id] = status
		}
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

// The fields of the events below are in the alphabetical order of their JSON
// keys: these events were maps once, and their JSON must not change.

// PresenceUpdate says what a member's live status is now (presence_update).
// A bot that set an activity carries it; nobody else has one.
type PresenceUpdate struct {
	Activity *BotActivity `json:"activity,omitempty"`
	Status   string       `json:"status"`
	UserID   uuid.UUID    `json:"user_id" format:"uuid"`
}

func (PresenceUpdate) EventName() string { return events.PresenceUpdate }

// PresenceSnapshot is the live status of everyone who is online, by user ID
// (presence_snapshot): part of the greeting of a new connection.
type PresenceSnapshot map[uuid.UUID]string

func (PresenceSnapshot) EventName() string { return events.PresenceSnapshot }
