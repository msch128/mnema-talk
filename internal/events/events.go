// Package events decouples feature packages from the WebSocket hub: handlers
// publish through this interface, the hub implements it, tests record it.
package events

import (
	"encoding/json"
	"runtime"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

type Publisher interface {
	// Broadcast sends an event to every connected client.
	Broadcast(eventType string, payload any)
	// BroadcastChannel sends an event only to the sessions of members who may
	// view the channel. Use it for everything that reveals what happens in
	// one channel (messages, reactions, typing, who is in a Talk).
	BroadcastChannel(channelID uuid.UUID, eventType string, payload any)
	// SendToUsers sends an event only to the given users' sessions.
	SendToUsers(userIDs []uuid.UUID, eventType string, payload any)
	// BroadcastPerm sends an event only to the sessions of members who hold
	// every permission of need server-wide, judged when it is sent. Use it for
	// what only moderators may know (the open-report count).
	BroadcastPerm(need perm.Set, eventType string, payload any)
}

// Event is one recorded publication (used by tests).
type Event struct {
	Type       string
	Payload    any
	Recipients []uuid.UUID // nil means broadcast
	// ChannelID is set for events limited to the viewers of a channel.
	ChannelID uuid.UUID
	// Need is set for events limited to holders of a server-wide permission.
	Need perm.Set
	// Site is the function that published the event, without the module path.
	Site string
}

// Wire is the frame a connection receives for the event: what the hub writes.
func (e Event) Wire() []byte {
	data, err := json.Marshal(struct {
		Type    string `json:"type"`
		Payload any    `json:"payload"`
	}{e.Type, e.Payload})
	if err != nil {
		panic("events: recorded payload does not marshal: " + err.Error())
	}
	return data
}

// site names the function that called the Recorder method above it.
func site() string {
	pc, _, _, ok := runtime.Caller(2)
	if !ok {
		return ""
	}
	name := runtime.FuncForPC(pc).Name()
	if i := strings.Index(name, "/internal/"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// Recorder is an in-memory Publisher for tests.
type Recorder struct {
	mu     sync.Mutex
	Events []Event
}

func (r *Recorder) Broadcast(eventType string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, Event{Type: eventType, Payload: payload, Site: site()})
}

func (r *Recorder) BroadcastChannel(channelID uuid.UUID, eventType string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, Event{Type: eventType, Payload: payload, ChannelID: channelID, Site: site()})
}

func (r *Recorder) BroadcastPerm(need perm.Set, eventType string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, Event{Type: eventType, Payload: payload, Need: need, Site: site()})
}

func (r *Recorder) SendToUsers(userIDs []uuid.UUID, eventType string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, Event{Type: eventType, Payload: payload, Recipients: append([]uuid.UUID(nil), userIDs...), Site: site()})
}

// Take returns the recorded events and forgets them.
func (r *Recorder) Take() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	taken := r.Events
	r.Events = nil
	return taken
}

// Snapshot returns a copy of the recorded events.
func (r *Recorder) Snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.Events...)
}
