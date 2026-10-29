package ws

import (
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// browserBroadcast sends a plain event through the established Broadcast or
// voice-room helpers. Scoped events (a view channel, excluding the sender)
// need emitApplicationEvent's own delivery and report false here.
func browserBroadcast(c *Client, eventType string, payload any, excludeSelf bool, voice *uuid.UUID, view uuid.UUID) bool {
	if view != uuid.Nil || excludeSelf {
		return false
	}
	if voice != nil {
		c.hub.sendToVoiceRoom(*voice, eventType, payload)
		return true
	}
	c.hub.Broadcast(eventType, payload)
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
func (c *Client) emitApplicationEvent(eventType string, payload any, excludeSelf bool, voice *uuid.UUID, view uuid.UUID) {
	h := c.hub
	if browserBroadcast(c, eventType, payload, excludeSelf, voice, view) {
		return
	}
	data := encode(eventType, payload)
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
	h.mu.RLock()
	defer h.mu.RUnlock()
	publishScoped(h, data, scope, view, eventType, payload)
}

func publishScoped(h *Hub, data []byte, scope eventScope, view uuid.UUID, eventType string, payload any) {
	// Outgoing webhooks only hear what is really published: after the
	// publication is authorized and live, never before.
	if view != uuid.Nil {
		if scope.snap != nil && scope.snap.IsDM(view) {
			h.dmTapped(view, eventType, payload)
		} else {
			h.tapped(view, eventType, payload)
		}
	}
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
	if s.snap != nil && s.snap.IsDM(s.view) && !recipient.dmAware {
		return false // a client from before direct messages does not know conversations
	}
	return recipient.can(s.snap, s.view, perm.ViewChannel)
}
