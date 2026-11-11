package ws

import (
	"log/slog"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

func encode(ev events.Event) []byte {
	data, err := events.Marshal(ev)
	if err != nil {
		slog.Error("encode ws event", "type", ev.EventName(), "err", err)
		return nil
	}
	return data
}

// tapped shows a published event to the hub's Tap.
func (h *Hub) tapped(channel uuid.UUID, ev events.Event) {
	if h.Tap != nil {
		h.Tap(channel, ev)
	}
}

// dmTapped shows an event of a direct message conversation to the hub's DMTap.
func (h *Hub) dmTapped(channel uuid.UUID, ev events.Event) {
	if h.DMTap != nil {
		h.DMTap(channel, ev)
	}
}

// Broadcast sends an event to every connected client.
func (h *Hub) Broadcast(ev events.Event) {
	data := encode(ev)
	if data == nil {
		return
	}
	h.tapped(uuid.Nil, ev)
	if ev.EventName() == events.UserUpdate {
		h.applyUserUpdate(events.PayloadOf(ev))
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		c.deliver(data)
	}
}

// SendToUsers sends an event to all sessions of the given users.
func (h *Hub) SendToUsers(userIDs []uuid.UUID, ev events.Event) {
	data := encode(ev)
	if data == nil {
		return
	}
	want := make(map[uuid.UUID]bool, len(userIDs))
	for _, id := range userIDs {
		want[id] = true
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if want[c.User.ID] {
			c.deliver(data)
		}
	}
}

// BroadcastPerm sends an event to every connection whose user holds all of need
// server-wide right now (the Owner always). A permission state that cannot be
// loaded fails closed: nobody receives the event.
func (h *Hub) BroadcastPerm(need perm.Set, ev events.Event) {
	data := encode(ev)
	if data == nil {
		return
	}
	snap, ok := h.permSnapshot(uuid.Nil)
	if !ok {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if snap.Global(c.subject()).Has(need) {
			c.deliver(data)
		}
	}
}
