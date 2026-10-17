package ws

import (
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
)

type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

func encode(eventType string, payload any) []byte {
	data, err := json.Marshal(Event{Type: eventType, Payload: payload})
	if err != nil {
		slog.Error("encode ws event", "type", eventType, "err", err)
		return nil
	}
	return data
}

// tapped shows a published event to the hub's Tap.
func (h *Hub) tapped(channel uuid.UUID, eventType string, payload any) {
	if h.Tap != nil {
		h.Tap(channel, eventType, payload)
	}
}

// Broadcast sends an event to every connected client.
func (h *Hub) Broadcast(eventType string, payload any) {
	data := encode(eventType, payload)
	if data == nil {
		return
	}
	h.tapped(uuid.Nil, eventType, payload)
	if eventType == "user_update" {
		h.applyUserUpdate(payload)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		c.deliver(data)
	}
}

// SendToUsers sends an event to all sessions of the given users.
func (h *Hub) SendToUsers(userIDs []uuid.UUID, eventType string, payload any) {
	data := encode(eventType, payload)
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
