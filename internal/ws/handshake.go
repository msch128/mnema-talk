package ws

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// authenticated writes the refusal and reports false when the cookie is not a session.
func (h *Hub) authenticated(w http.ResponseWriter, r *http.Request) (*auth.User, int, bool) {
	user, tv, err := h.Sessions.AuthenticateRequest(r)
	if err == nil {
		return user, tv, true
	}
	if _, api := httpx.AsAPIError(err); !api {
		err = httpx.ErrServer(err)
	}
	httpx.WriteError(w, err)
	return nil, 0, false
}

// upgradeAllowed applies the per-user upgrade limit. A nil limit allows the upgrade.
func (h *Hub) upgradeAllowed(w http.ResponseWriter, userID uuid.UUID) bool {
	if h.UpgradeLimit == nil {
		return true
	}
	ok, retry := h.UpgradeLimit.Allow(userID.String())
	if ok {
		return true
	}
	httpx.WriteRateLimited(w, retry)
	return false
}

// admitPending evicts overflow sockets, then drops c when the handshake is refused.
func (h *Hub) admitPending(c *Client) bool {
	ok, evicted := h.registerPending(c)
	for _, old := range evicted {
		old.evict()
	}
	if ok {
		return true
	}
	c.shutdown()
	c.close()
	return false
}

// confirmHandshake re-reads the session after the upgrade. Revocation can finish
// between the first check and the upgrade; the socket is already tracked, so a
// later revocation also cancels admission. The hub lock is never held while the
// database is consulted. registerVerified runs only when the same session remains.
func (h *Hub) confirmHandshake(r *http.Request, c *Client, userID uuid.UUID, tv int) bool {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	fresh, freshTV, err := h.Sessions.AuthenticateRequest(r.WithContext(ctx))
	cancel()
	if handshakeChanged(err, fresh, userID, freshTV, tv) {
		return false
	}
	return h.registerVerified(c, fresh)
}

// handshakeChanged reports that the session at the upgrade is not the one first
// accepted. A nil user is not dereferenced.
func handshakeChanged(err error, fresh *auth.User, userID uuid.UUID, freshTV, tv int) bool {
	if err != nil || fresh == nil {
		return true
	}
	if fresh.ID != userID {
		return true
	}
	return freshTV != tv
}

// admitVerified publishes a pending handshake. Callers do not hold h.mu.
func (h *Hub) admitVerified(c *Client, fresh *auth.User) (before, after string, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || c.closed.Load() || !c.commandsAllowed() {
		return "", "", false
	}
	if !h.takePending(c, fresh) {
		return "", "", false
	}
	before = h.statusLocked(c.User.ID)
	h.clients[c] = struct{}{}
	h.online[c.User.ID]++
	h.active[c.User.ID]++
	// The session was just loaded from the database: the freshest profile.
	h.profiles[c.User.ID] = c.User.Public()
	// The session was just loaded, so its presence is the latest choice.
	h.notePresenceLocked(c)
	after = h.statusLocked(c.User.ID)
	return before, after, true
}

// takePending claims a promoted handshake. Callers hold h.mu. A nil user is an
// in-memory client and has nothing pending to claim.
func (h *Hub) takePending(c *Client, fresh *auth.User) bool {
	if fresh == nil {
		return true
	}
	if _, pending := h.pending[c]; !pending {
		return false
	}
	delete(h.pending, c)
	c.User = *fresh
	return true
}

// notePresenceLocked records the account's presence. Callers hold h.mu.
func (h *Hub) notePresenceLocked(c *Client) {
	if auth.ValidPresence(c.User.Presence) {
		h.chosen[c.User.ID] = c.User.Presence
		return
	}
	if h.chosen[c.User.ID] == "" {
		h.chosen[c.User.ID] = auth.PresenceOnline
	}
}

// greet tells the member they are connected, outside the hub lock.
func (h *Hub) greet(c *Client, before, after string) {
	slog.Info("ws connected", "user", c.User.Username)
	h.announcePresence(c.User.ID, before, after)
	c.SendEvent("server_info", h.serverInfo())
	c.SendEvent("presence_snapshot", h.presenceSnapshot())
	h.sendBotActivities(c)
	// Talks the member may not view stay invisible: who sits in them, since
	// when and what they share.
	snap, ok := h.permSnapshot(uuid.Nil)
	if !ok {
		snap = deniedSnapshot
	}
	c.SendEvent("voice_snapshot", visibleTo(c, snap, h.voiceSnapshot()))
	c.SendEvent("voice_rooms", h.voiceRooms(c, snap))
	if h.SFU == nil {
		return
	}
	for chID, states := range visibleTo(c, snap, h.SFU.AllMediaStates()) {
		for uid, st := range states {
			c.SendEvent("webrtc_media_state", mediaStatePayload(chID, uid, st))
		}
	}
}
