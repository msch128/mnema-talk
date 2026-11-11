package ws

import (
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/safego"
)

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
	var wasIn, hadSince bool
	var since time.Time
	var class seatClass
	safego.Locked(&h.mu, func() {
		_, wasIn = h.voice[chID][userID]
		since, hadSince = h.voiceSince[key]
		class = h.seatClasses[key]
		if wasIn {
			delete(h.voiceSince, key)
			delete(h.seatClasses, key)
			delete(h.voiceMute, key)
			h.stage.forget(key) // under the lock: no grant can land after it
		}
		if users, ok := h.voice[chID]; ok {
			delete(users, userID)
			if len(users) == 0 {
				delete(h.voice, chID)
				delete(h.roomSince, chID)
			}
		}
	})
	if wasIn {
		slog.Info("voice leave", "user", userID, "channel", chID)
		if h.Presence != nil {
			h.Presence(userID, chID, false)
		}
		h.BroadcastChannel(chID, events.VoiceStateUpdate, map[string]any{"action": "leave", "channel_id": chID, "user_id": userID})
		if hadSince {
			h.addVoiceTime(userID, chID, time.Since(since), h.settleSeat(chID, class))
		}
	}
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
	g.timer = safego.AfterFunc("ws_voice_grace", h.VoiceGrace, safego.Crash, func() {
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

// releaseSeat ends what a panicking connection still holds of a Talk. The
// read loop's own exit normally did that already (dropVoiceForGrace: the
// media peer is gone and a grace leave runs), and then nothing is left to do
// here. When that clean-up panicked half way, the media peer is removed and
// the member's seats that no connection and no grace leave stand for are
// given up at once, so nobody stays listed in a Talk for good.
func (h *Hub) releaseSeat(c *Client) {
	func() {
		c.voiceMu.Lock()
		defer c.voiceMu.Unlock()
		if ch := c.currentVoice(); ch != nil {
			if h.SFU != nil {
				h.SFU.RemovePeer(*ch, c.peer())
			}
			c.setVoice(nil)
		}
	}()
	for _, ch := range h.seatsWithoutGrace(c.User.ID) {
		h.removePresenceUnlessPresent(c.User.ID, ch, c)
	}
}

// seatsWithoutGrace lists the Talks userID is listed in with no grace leave
// pending.
func (h *Hub) seatsWithoutGrace(userID uuid.UUID) []uuid.UUID {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []uuid.UUID
	for ch, users := range h.voice {
		if _, seated := users[userID]; seated && h.grace[voiceKey{userID, ch}] == nil {
			out = append(out, ch)
		}
	}
	return out
}
