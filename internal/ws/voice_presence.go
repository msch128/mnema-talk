package ws

import (
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events/wire"
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
	h.startGrace(voiceKey{c.User.ID, *ch}, c)
}

// removePresenceUnlessPresent ends userID's presence in chID unless another
// connection than except (a second tab) is in the room. except is the
// connection that left, when there is one: the leave is logged under its id.
func (h *Hub) removePresenceUnlessPresent(userID, chID uuid.UUID, except *Client) {
	h.endPresence(userID, chID, except.logger(), func() bool { return h.seatedLocked(userID, chID, except) })
}

// removePresence drops userID from chID's room, whoever of their connections
// is in it, and announces it if they were there.
func (h *Hub) removePresence(userID, chID uuid.UUID) {
	h.endPresence(userID, chID, slog.Default(), func() bool { return false })
}

// seatedLocked reports whether a connection of userID other than except is
// in chID's room. Callers hold h.mu.
func (h *Hub) seatedLocked(userID, chID uuid.UUID, except *Client) bool {
	for c := range h.rooms[chID] {
		if c != except && c.User.ID == userID {
			return true
		}
	}
	return false
}

// endPresence is the removal behind removePresence. keep is asked under the
// lock that removes the seat: a join that takes the seat (takeSeat, under the
// same lock) either comes first and keeps it, or comes after and is a new
// join. Asked before, it would let that join lose a seat it believes it has.
func (h *Hub) endPresence(userID, chID uuid.UUID, log *slog.Logger, keep func() bool) {
	key := voiceKey{userID, chID}
	var wasIn, hadSince bool
	var since time.Time
	var class seatClass
	safego.Locked(&h.mu, func() {
		if keep() {
			return
		}
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
	if !wasIn {
		return
	}
	log.Info("voice leave", "user", userID, "channel", chID)
	// The seat is gone already. Deferred, so a panic in a listener (Presence,
	// Tap) loses neither the stay's voice time nor the others' "leave".
	if hadSince {
		stay, counts := time.Since(since), h.settleSeat(chID, class)
		defer h.addVoiceTime(userID, chID, stay, counts)
	}
	defer h.BroadcastChannel(chID, wire.VoiceLeave{Action: "leave", ChannelID: chID, UserID: userID})
	if h.Presence != nil {
		h.Presence(userID, chID, false)
	}
}

// startGrace keeps a user listed in a room for VoiceGrace, then removes them
// unless takeGrace claimed the slot first or a connection of theirs is in the
// room by then. No grace starts while a connection other than except is in
// the room: the user never left. except is the connection that went away, and
// the leave the grace ends in is logged under its id.
func (h *Hub) startGrace(key voiceKey, except *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.seatedLocked(key.user, key.ch, except) {
		return
	}
	if old := h.grace[key]; old != nil {
		old.timer.Stop()
	}
	g := &graceLeave{channelID: key.ch}
	log := except.logger() // not the client: the timer outlives its connection
	var conn safego.Conn   // stays "" when a refused join restores the grace: no connection is leaving
	if except != nil {
		conn = except.panics()
	}
	g.timer = safego.AfterFuncConn(conn, "ws_voice_grace", h.VoiceGrace, safego.Crash, func() {
		h.mu.Lock()
		current := h.grace[key] == g
		if current {
			delete(h.grace, key)
		}
		h.mu.Unlock()
		if current {
			h.endPresence(key.user, key.ch, log, func() bool { return h.seatedLocked(key.user, key.ch, nil) })
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
	// A closed hub has settled every stay and told Presence nothing more:
	// its listeners (the audit log) are closing with the server.
	if h.isClosed() {
		return
	}
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
