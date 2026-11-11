package ws

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
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
	if h.closed || c.closed.Load() {
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
	c.logger().Info("ws connected", c.connectedAttrs()...)
	// server_info leads: the broadcast below can reach this very connection.
	c.SendEvent(h.serverInfo())
	h.announcePresence(c.User.ID, before, after)
	c.SendEvent(h.presenceSnapshot())
	h.sendBotActivities(c)
	// Talks the member may not view stay invisible: who sits in them, since
	// when and what they share.
	snap, ok := h.permSnapshot(uuid.Nil)
	if !ok {
		snap = deniedSnapshot
	}
	c.SendEvent(VoiceSnapshot(visibleTo(c, snap, h.voiceSnapshot())))
	c.SendEvent(h.voiceRooms(c, snap))
	if h.SFU == nil {
		return
	}
	pace := newPacer(c)
	defer pace.stop()
	for chID, states := range visibleTo(c, snap, h.SFU.AllMediaStates()) {
		for uid, st := range states {
			if !pace.send(mediaStatePayload(chID, uid, st)) {
				return
			}
		}
	}
}

// pacer queues the bulk part of a greeting without flooding the send buffer:
// it waits while the buffer is more than half full, so a large snapshot
// neither drops the client as too slow nor leaves it no room for the
// broadcasts that arrive meanwhile. One writeWait budget measures a stall, not
// the total time: it starts over whenever the client has drained some of its
// buffer. A client that stays stuck past it gets the ordinary slow-client
// treatment, and a client that is gone ends the greeting at once so readPump
// can unregister it.
type pacer struct {
	c        *Client
	deadline *time.Timer
	tick     *time.Ticker
	wait     time.Duration
	queued   int // length of the send buffer at the last look
}

func newPacer(c *Client) *pacer { return newPacerWait(c, writeWait) }

func newPacerWait(c *Client, wait time.Duration) *pacer {
	return &pacer{c: c, deadline: time.NewTimer(wait), tick: time.NewTicker(time.Millisecond), wait: wait}
}

// progressed restarts the stall budget when the client drained since the last look.
func (p *pacer) progressed(queued int) {
	if queued < p.queued {
		if !p.deadline.Stop() {
			select {
			case <-p.deadline.C:
			default:
			}
		}
		p.deadline.Reset(p.wait)
	}
	p.queued = queued
}

func (p *pacer) stop() {
	p.deadline.Stop()
	p.tick.Stop()
}

// send queues one event; false means the greeting must stop.
func (p *pacer) send(ev events.Event) bool {
	c := p.c
	for {
		queued := len(c.send)
		p.progressed(queued)
		if queued <= cap(c.send)/2 {
			break
		}
		if c.gone.Load() {
			return false
		}
		select {
		case <-c.done:
			return false
		case <-p.deadline.C:
			c.dropSlow()
			return false
		case <-p.tick.C:
		}
	}
	c.SendEvent(ev)
	p.queued = len(c.send)
	return !c.gone.Load()
}
