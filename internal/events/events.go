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

// Event is one realtime event: a value that names itself and marshals to the
// payload of its frame. Every event is a named type, so a publisher cannot
// send a shape its consumers do not know. A type whose fields are plain (IDs,
// text, times) lives in this package; one that carries a domain type lives in
// the package that owns that type.
type Event interface {
	// EventName is the name on the wire, one of the constants of types.go.
	EventName() string
}

// Marshal renders the frame a connection receives for ev.
func Marshal(ev Event) ([]byte, error) {
	return json.Marshal(frame{Type: ev.EventName(), Payload: ev})
}

type frame struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// Untyped is an event whose payload has no type of its own yet.
//
// Deprecated: a stopgap while the events are converted family by family.
func Untyped(name string, payload any) Event { return untyped{name: name, payload: payload} }

type untyped struct {
	name    string
	payload any
}

func (u untyped) EventName() string { return u.name }

func (u untyped) MarshalJSON() ([]byte, error) { return json.Marshal(u.payload) }

// PayloadOf returns what ev marshals: ev itself, or the wrapped value of an
// Untyped event.
//
// Deprecated: goes with Untyped.
func PayloadOf(ev Event) any {
	if u, ok := ev.(untyped); ok {
		return u.payload
	}
	return ev
}

type Publisher interface {
	// Broadcast sends an event to every connected client.
	Broadcast(ev Event)
	// BroadcastChannel sends an event only to the sessions of members who may
	// view the channel. Use it for everything that reveals what happens in
	// one channel (messages, reactions, typing, who is in a Talk).
	BroadcastChannel(channelID uuid.UUID, ev Event)
	// SendToUsers sends an event only to the given users' sessions.
	SendToUsers(userIDs []uuid.UUID, ev Event)
	// BroadcastPerm sends an event only to the sessions of members who hold
	// every permission of need server-wide, judged when it is sent. Use it for
	// what only moderators may know (the open-report count).
	BroadcastPerm(need perm.Set, ev Event)
}

// Recorded is one recorded publication (used by tests).
type Recorded struct {
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
func (e Recorded) Wire() []byte {
	data, err := json.Marshal(frame{Type: e.Type, Payload: e.Payload})
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
	Events []Recorded
}

func (r *Recorder) Broadcast(ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, Recorded{Type: ev.EventName(), Payload: PayloadOf(ev), Site: site()})
}

func (r *Recorder) BroadcastChannel(channelID uuid.UUID, ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, Recorded{Type: ev.EventName(), Payload: PayloadOf(ev), ChannelID: channelID, Site: site()})
}

func (r *Recorder) BroadcastPerm(need perm.Set, ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, Recorded{Type: ev.EventName(), Payload: PayloadOf(ev), Need: need, Site: site()})
}

func (r *Recorder) SendToUsers(userIDs []uuid.UUID, ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, Recorded{Type: ev.EventName(), Payload: PayloadOf(ev), Recipients: append([]uuid.UUID(nil), userIDs...), Site: site()})
}

// Take returns the recorded events and forgets them.
func (r *Recorder) Take() []Recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	taken := r.Events
	r.Events = nil
	return taken
}

// Snapshot returns a copy of the recorded events.
func (r *Recorder) Snapshot() []Recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Recorded(nil), r.Events...)
}
