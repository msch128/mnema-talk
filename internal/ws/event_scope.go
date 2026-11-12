package ws

import (
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

// browserBroadcast sends a plain event through the established Broadcast or
// voice-room helpers. Scoped events (a view channel, excluding the sender)
// need emitApplicationEvent's own delivery and report false here.
func browserBroadcast(c *Client, ev events.Event, excludeSelf bool, voice *uuid.UUID, view uuid.UUID) bool {
	if view != uuid.Nil || excludeSelf {
		return false
	}
	if voice != nil {
		c.hub.sendToVoiceRoom(*voice, ev)
		return true
	}
	c.hub.Broadcast(ev)
	return true
}

func attachView(scope *eventScope, h *Hub, view uuid.UUID) bool {
	if view == uuid.Nil {
		return true
	}
	snap, ok := h.permSnapshot(view)
	if !ok {
		return false
	}
	scope.snap = snap
	return true
}

// emitApplicationEvent publishes an event from this connection to its
// audience. view, when set, limits the event to the connections whose user may
// view that channel.
func (c *Client) emitApplicationEvent(ev events.Event, excludeSelf bool, voice *uuid.UUID, view uuid.UUID) {
	h := c.hub
	if browserBroadcast(c, ev, excludeSelf, voice, view) {
		return
	}
	data := encode(ev)
	if data == nil {
		return
	}
	scope := eventScope{excludeUser: uuid.Nil, voice: voice, view: view}
	if excludeSelf {
		scope.excludeUser = c.User.ID
	}
	if !attachView(&scope, h, view) {
		return
	}
	// Outgoing webhooks only hear what is really published: after the
	// publication is authorized, never before. The taps run before the lock
	// is taken, as in BroadcastChannel (see Hub.Tap), and before the frames
	// are handed out, so what a tap publishes reaches each connection first.
	if view != uuid.Nil {
		if scope.snap.IsPrivate(view) {
			h.dmTapped(view, ev)
		} else {
			h.tapped(view, ev)
		}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	deliverScoped(h, data, scope)
}

func deliverScoped(h *Hub, data []byte, scope eventScope) {
	for recipient := range h.clients {
		if scope.admits(recipient) {
			recipient.deliver(data)
		}
	}
}

// eventScope narrows an application event to its audience.
type eventScope struct {
	excludeUser uuid.UUID      // uuid.Nil: nobody is left out
	voice       *uuid.UUID     // only connections in this voice room
	view        uuid.UUID      // only users who may view this channel
	snap        *perm.Snapshot // permissions to check view against
}

func (s eventScope) admits(recipient *Client) bool {
	if s.excludeUser != uuid.Nil && recipient.User.ID == s.excludeUser {
		return false
	}
	if s.voice != nil {
		current := recipient.currentVoice()
		if current == nil || *current != *s.voice {
			return false
		}
	}
	if s.view == uuid.Nil {
		return true
	}
	if s.snap.IsDM(s.view) && !recipient.dmAware {
		return false // a client from before direct messages does not know conversations
	}
	return recipient.can(s.snap, s.view, perm.ViewChannel)
}
