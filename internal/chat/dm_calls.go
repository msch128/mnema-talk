package chat

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
)

// DefaultRingDuration is how long a call from a direct message rings before it
// counts as missed.
const DefaultRingDuration = 45 * time.Second

// Why a call stopped ringing (DMCallEnd.Reason).
const (
	// CallAnswered: somebody other than the caller entered the Talk.
	CallAnswered = "answered"
	// CallCancelled: the caller left the Talk before anybody joined.
	CallCancelled = "cancelled"
	// CallTimeout: nobody joined within the ring duration.
	CallTimeout = "timeout"
	// CallEnded: the last member left the Talk.
	CallEnded = "ended"
)

// callMissed is the Embed.Call value of the message that records a call nobody
// answered.
const callMissed = "missed"

// DMCallRing is the payload of the dm_call_ring event: a call to the
// conversation's members started.
type DMCallRing struct {
	ConversationID uuid.UUID `json:"conversation_id" format:"uuid"`
	TalkID         uuid.UUID `json:"talk_id" format:"uuid"`
	// MessageID is the invitation card posted into the conversation.
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
	CallerID  uuid.UUID `json:"caller_id" format:"uuid"`
	TalkName  string    `json:"talk_name"`
	// RingSeconds is how long it rings from now on.
	RingSeconds int `json:"ring_seconds"`
}

// DMCallEnd is the payload of the dm_call_end event: the call stopped ringing
// (Active tells whether it goes on) or is over.
type DMCallEnd struct {
	ConversationID uuid.UUID `json:"conversation_id" format:"uuid"`
	TalkID         uuid.UUID `json:"talk_id" format:"uuid"`
	Reason         string    `json:"reason" enums:"answered,cancelled,timeout,ended"`
	// Active is true while somebody is still in the Talk.
	Active bool `json:"active"`
	// UserID is who answered (reason answered).
	UserID *uuid.UUID `json:"user_id,omitempty" binding:"optional" format:"uuid"`
	// MessageID is the "missed call" message the unanswered call left behind.
	MessageID *uuid.UUID `json:"message_id,omitempty" binding:"optional" format:"uuid"`
}

// DMCall is the call of a conversation as GET /api/dms shows it.
type DMCall struct {
	TalkID    uuid.UUID `json:"talk_id" format:"uuid"`
	CallerID  uuid.UUID `json:"caller_id" format:"uuid"`
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
	TalkName  string    `json:"talk_name"`
	Ringing   bool      `json:"ringing"`
	// RingSeconds is how long it keeps ringing (0 when it does not).
	RingSeconds int `json:"ring_seconds"`
}

// DMCalls follows the calls started from direct messages ("Start a Talk"): who
// is being rung, whether somebody answered, and when the call is over. It
// learns about members from the hub's join/leave hook (OnPresence) and tells
// the conversation's members with dm_call_ring and dm_call_end. A call nobody
// answers leaves a "missed call" message behind. All of it is in memory: a
// restart ends every call, and the Talks go with it.
type DMCalls struct {
	DB     *db.Pool
	Events events.Publisher
	// Online resolves @here for the messages it posts; nil means nobody.
	Online OnlineSource
	// Members lists who is in a Talk now (the hub's voice room); nil means nobody.
	Members func(talk uuid.UUID) []uuid.UUID
	// Ring is how long a call rings; zero means the default.
	Ring time.Duration

	mu    sync.Mutex
	calls map[uuid.UUID]*dmCall // by conversation
	// queue holds the announcements in the order the calls changed, so the
	// subscribers hear a call ring before it ends.
	queue []func(context.Context)
	wake  chan struct{}
}

// dmCall is what DMCalls knows of one call.
type dmCall struct {
	conversation, talk, caller, message uuid.UUID
	name                                string
	ringing                             bool
	until                               time.Time
	// seen is set once somebody has been in the Talk; an empty Talk is only
	// over after that.
	seen  bool
	here  map[uuid.UUID]struct{}
	timer *time.Timer
}

// NewDMCalls readies c for use; Run starts it.
func NewDMCalls(c *DMCalls) *DMCalls {
	c.calls = map[uuid.UUID]*dmCall{}
	c.wake = make(chan struct{}, 1)
	if c.Ring <= 0 {
		c.Ring = DefaultRingDuration
	}
	return c
}

// Run announces what the calls do until ctx ends.
func (m *DMCalls) Run(ctx context.Context) {
	defer m.stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
			for m.announceNext(ctx) {
			}
		}
	}
}

// stop cancels the ring timers.
func (m *DMCalls) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.calls {
		if c.timer != nil {
			c.timer.Stop()
		}
	}
}

// push queues an announcement; the caller holds the lock.
func (m *DMCalls) push(announce func(context.Context)) {
	m.queue = append(m.queue, announce)
	select {
	case m.wake <- struct{}{}:
	default: // Run is already woken and drains everything queued
	}
}

func (m *DMCalls) announceNext(ctx context.Context) bool {
	m.mu.Lock()
	if len(m.queue) == 0 {
		m.mu.Unlock()
		return false
	}
	announce := m.queue[0]
	m.queue = m.queue[1:]
	m.mu.Unlock()
	announce(ctx)
	return true
}

// Begin starts ringing the conversation's other members: caller opened talk
// (or shared it) and posted the invitation message. A conversation already
// ringing for the same Talk is not rung again, and a Talk somebody else is in
// already is tracked but not rung: they are in the call, there is nobody to
// miss it and no record to leave.
func (m *DMCalls) Begin(conversation, talk, caller, message uuid.UUID, name string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if old := m.calls[conversation]; old != nil {
		if old.talk == talk && old.ringing {
			m.mu.Unlock()
			return
		}
		if old.ringing {
			m.supersedeLocked(old) // its card goes away
		} else {
			m.dropLocked(old) // a call with people in it is not announced as over
		}
	}
	c := &dmCall{
		conversation: conversation, talk: talk, caller: caller, message: message, name: name,
		until: time.Now().Add(m.Ring), here: map[uuid.UUID]struct{}{}, ringing: true,
	}
	c.timer = time.AfterFunc(m.Ring, func() { m.expire(c) })
	m.calls[conversation] = c
	m.mu.Unlock()
	// Who is in the Talk already is read outside the lock (the hub has its own);
	// whoever joins or leaves meanwhile reaches the call through OnPresence.
	var present []uuid.UUID
	if m.Members != nil {
		present = m.Members(talk)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls[conversation] != c || !c.ringing {
		return // it already ended or was answered
	}
	for _, u := range present {
		c.here[u] = struct{}{}
		c.seen = true
		if u != caller {
			c.ringing = false // somebody is in already: nobody to ring
		}
	}
	if !c.ringing {
		c.timer.Stop()
		return
	}
	ring := DMCallRing{
		ConversationID: conversation, TalkID: talk, MessageID: message, CallerID: caller,
		TalkName: name, RingSeconds: int(m.Ring.Seconds()),
	}
	m.push(func(context.Context) { m.Events.BroadcastChannel(conversation, events.DMCallRing, ring) })
}

// supersedeLocked ends the call of a conversation that is being rung for another
// Talk: the old card goes away, and no message is left behind.
func (m *DMCalls) supersedeLocked(c *dmCall) {
	end := DMCallEnd{ConversationID: c.conversation, TalkID: c.talk, Reason: CallCancelled}
	m.dropLocked(c)
	m.push(func(context.Context) { m.Events.BroadcastChannel(end.ConversationID, events.DMCallEnd, end) })
}

// dropLocked forgets c without announcing anything.
func (m *DMCalls) dropLocked(c *dmCall) {
	if c.timer != nil {
		c.timer.Stop()
	}
	delete(m.calls, c.conversation)
}

// Active returns the call of the conversation, or nil.
func (m *DMCalls) Active(conversation uuid.UUID) *DMCall {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.calls[conversation]
	if c == nil {
		return nil
	}
	left := 0
	if c.ringing {
		left = max(1, int(time.Until(c.until).Round(time.Second)/time.Second))
	}
	return &DMCall{TalkID: c.talk, CallerID: c.caller, MessageID: c.message, TalkName: c.name, Ringing: c.ringing, RingSeconds: left}
}

// OnPresence is the hub's Presence hook; it must not block, so it only
// updates the calls and queues what to announce.
func (m *DMCalls) OnPresence(user, talk uuid.UUID, joined bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.calls {
		if c.talk != talk {
			continue
		}
		if joined {
			m.joinedLocked(c, user)
		} else {
			m.leftLocked(c, user)
		}
	}
}

func (m *DMCalls) joinedLocked(c *dmCall, user uuid.UUID) {
	c.here[user] = struct{}{}
	c.seen = true
	if c.ringing && user != c.caller {
		m.stopRingingLocked(c, CallAnswered, &user)
	}
}

func (m *DMCalls) leftLocked(c *dmCall, user uuid.UUID) {
	delete(c.here, user)
	switch {
	case c.ringing && user == c.caller:
		m.stopRingingLocked(c, CallCancelled, nil)
	case c.seen && len(c.here) == 0:
		m.finishLocked(c, DMCallEnd{Reason: CallEnded})
	}
}

// expire ends the ring of c when nobody joined in time.
func (m *DMCalls) expire(c *dmCall) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls[c.conversation] != c || !c.ringing {
		return
	}
	m.stopRingingLocked(c, CallTimeout, nil)
}

// stopRingingLocked ends the ringing of c for reason. A call nobody answered
// leaves a "missed call" message; it is over unless somebody is in the Talk.
func (m *DMCalls) stopRingingLocked(c *dmCall, reason string, answeredBy *uuid.UUID) {
	c.ringing = false
	if c.timer != nil {
		c.timer.Stop()
	}
	end := DMCallEnd{Reason: reason, UserID: answeredBy}
	if reason == CallAnswered {
		m.finishLocked(c, end)
		return
	}
	m.finishLocked(c, end, func(ctx context.Context) *uuid.UUID { return m.recordMissed(ctx, c) })
}

// finishLocked announces that c stopped ringing or ended. The call is
// forgotten once nobody is in its Talk; missed, when given, runs first and
// yields the message to point to.
func (m *DMCalls) finishLocked(c *dmCall, end DMCallEnd, missed ...func(context.Context) *uuid.UUID) {
	end.ConversationID, end.TalkID, end.Active = c.conversation, c.talk, len(c.here) > 0
	if !end.Active {
		m.dropLocked(c)
	}
	m.push(func(ctx context.Context) {
		if len(missed) > 0 {
			end.MessageID = missed[0](ctx)
		}
		m.Events.BroadcastChannel(end.ConversationID, events.DMCallEnd, end)
	})
}

// recordMissed posts the "missed call" message of c, written by the caller,
// and returns its id (nil when it could not be posted).
func (m *DMCalls) recordMissed(ctx context.Context, c *dmCall) *uuid.UUID {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	id, err := InsertMessage(ctx, m.DB, m.Online, NewMessage{
		ChannelID: c.conversation, UserID: c.caller, Embeds: []Embed{{Title: c.name, Call: callMissed}}, Record: true,
	}, nil)
	if err != nil {
		slog.Warn("dm calls: record missed call", "conversation", c.conversation, "err", err)
		return nil
	}
	msg, err := GetMessage(ctx, m.DB, id)
	if err != nil {
		slog.Warn("dm calls: load missed call", "message", id, "err", err)
		return &id
	}
	m.Events.BroadcastChannel(c.conversation, events.MessageCreate, msg)
	return &id
}
