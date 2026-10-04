// Package events decouples feature packages from the WebSocket hub: handlers
// publish through this interface, the hub implements it, tests record it.
package events

import (
	"sync"

	"github.com/google/uuid"
)

type Publisher interface {
	// Broadcast sends an event to every connected client.
	Broadcast(eventType string, payload any)
	// SendToUsers sends an event only to the given users' sessions.
	SendToUsers(userIDs []uuid.UUID, eventType string, payload any)
}

// Event is one recorded publication (used by tests).
type Event struct {
	Type       string
	Payload    any
	Recipients []uuid.UUID // nil means broadcast
}

// Recorder is an in-memory Publisher for tests.
type Recorder struct {
	mu     sync.Mutex
	Events []Event
}

func (r *Recorder) Broadcast(eventType string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, Event{Type: eventType, Payload: payload})
}

func (r *Recorder) SendToUsers(userIDs []uuid.UUID, eventType string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, Event{Type: eventType, Payload: payload, Recipients: append([]uuid.UUID(nil), userIDs...)})
}

// Snapshot returns a copy of the recorded events.
func (r *Recorder) Snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.Events...)
}
