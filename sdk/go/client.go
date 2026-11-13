package mnema

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

const defaultHandlerConcurrency = 64

// Options configure a Client (and NewREST).
type Options struct {
	// BaseURL is the server's address, e.g. https://talk.example.org.
	BaseURL string
	// Token is the bot token (mnb_...).
	Token string
	// Intents are the event classes the gateway session asks for.
	Intents Intents
	// Presence is the status and activity to identify with (optional).
	Presence *Presence
	// Logger receives the SDK's logs; nil means slog.Default().
	Logger *slog.Logger
	// HTTPClient sends the REST requests; nil means a client with a 60 s
	// timeout.
	HTTPClient *http.Client
	// MaxRetries bounds how often a request answered with 429 is sent again
	// (0 means 3, a negative value none).
	MaxRetries int
	// GatewayURL overrides the address GET /gateway answers (optional).
	GatewayURL string
	// HandlerConcurrency bounds how many dispatches are handled at once
	// (0 means 64).
	HandlerConcurrency int
}

// Client is a bot: the REST client and a gateway session that delivers events
// to the registered handlers. Register handlers before Run.
type Client struct {
	// REST is the REST client of the bot.
	REST *REST

	opts    Options
	logger  *slog.Logger
	running atomic.Bool

	mu       sync.Mutex
	handlers handlers
	presence *Presence
	conn     *conn // the live connection, nil between connections

	sessionMu sync.Mutex
	session   session

	// backoff policy; tests shorten it.
	minBackoff, maxBackoff, rateLimitedBackoff time.Duration
	helloTimeout                               time.Duration
}

type handlers struct {
	ready           []func(context.Context, *Ready)
	resumed         []func(context.Context)
	messageCreate   []func(context.Context, *Message)
	messageUpdate   []func(context.Context, *Message)
	messageDelete   []func(context.Context, *MessageDelete)
	reactionsUpdate []func(context.Context, *ReactionsUpdate)
	channelsUpdate  []func(context.Context, *ChannelsUpdate)
	memberAdd       []func(context.Context, *MemberAdd)
	interaction     []func(context.Context, *Interaction)
	raw             []func(context.Context, string, json.RawMessage)
}

// New returns a client for the bot. It does not connect; Run does.
func New(o Options) (*Client, error) {
	rest, err := NewREST(o)
	if err != nil {
		return nil, err
	}
	if o.Presence != nil {
		if err := o.Presence.Validate(); err != nil {
			return nil, err
		}
	}
	if o.HandlerConcurrency <= 0 {
		o.HandlerConcurrency = defaultHandlerConcurrency
	}
	return &Client{
		REST: rest, opts: o, logger: loggerOr(o.Logger), presence: o.Presence,
		minBackoff: time.Second, maxBackoff: 60 * time.Second, rateLimitedBackoff: 30 * time.Second,
		helloTimeout: 30 * time.Second,
	}, nil
}

func add[T any](c *Client, list *[]T, h T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	*list = append(*list, h)
}

// OnReady registers a handler for READY (also after a new session).
func (c *Client) OnReady(h func(context.Context, *Ready)) { add(c, &c.handlers.ready, h) }

// OnResumed registers a handler for RESUMED: a dropped session continued.
func (c *Client) OnResumed(h func(context.Context)) { add(c, &c.handlers.resumed, h) }

// OnMessageCreate registers a handler for MESSAGE_CREATE (GUILD_MESSAGES).
func (c *Client) OnMessageCreate(h func(context.Context, *Message)) {
	add(c, &c.handlers.messageCreate, h)
}

// OnMessageUpdate registers a handler for MESSAGE_UPDATE (GUILD_MESSAGES).
func (c *Client) OnMessageUpdate(h func(context.Context, *Message)) {
	add(c, &c.handlers.messageUpdate, h)
}

// OnMessageDelete registers a handler for MESSAGE_DELETE (GUILD_MESSAGES).
func (c *Client) OnMessageDelete(h func(context.Context, *MessageDelete)) {
	add(c, &c.handlers.messageDelete, h)
}

// OnReactionsUpdate registers a handler for MESSAGE_REACTIONS_UPDATE
// (GUILD_MESSAGE_REACTIONS).
func (c *Client) OnReactionsUpdate(h func(context.Context, *ReactionsUpdate)) {
	add(c, &c.handlers.reactionsUpdate, h)
}

// OnChannelsUpdate registers a handler for CHANNELS_UPDATE (GUILDS).
func (c *Client) OnChannelsUpdate(h func(context.Context, *ChannelsUpdate)) {
	add(c, &c.handlers.channelsUpdate, h)
}

// OnMemberAdd registers a handler for GUILD_MEMBER_ADD (GUILD_MEMBERS).
func (c *Client) OnMemberAdd(h func(context.Context, *MemberAdd)) { add(c, &c.handlers.memberAdd, h) }

// OnInteraction registers a handler for INTERACTION_CREATE (no intent
// needed). The interaction's Reply and other methods use c.REST.
func (c *Client) OnInteraction(h func(context.Context, *Interaction)) {
	add(c, &c.handlers.interaction, h)
}

// OnDispatch registers a handler for every dispatch, with its name and raw
// data, including events this SDK does not know yet.
func (c *Client) OnDispatch(h func(ctx context.Context, name string, data json.RawMessage)) {
	add(c, &c.handlers.raw, h)
}

func (c *Client) snapshotHandlers() handlers {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.handlers
}

// dispatcher runs handlers in goroutines of their own, bounded by a
// semaphore, and waits for them on close.
type dispatcher struct {
	c   *Client
	ctx context.Context
	sem chan struct{}
	// queued bounds the events that wait for a slot; beyond it events are
	// shed (see dispatch).
	queued chan struct{}
	// unshed bounds the events of neverShed in flight; beyond it the
	// connection is closed and resumed instead (see dispatch).
	unshed chan struct{}
	// dropped counts the events shed because the queue was full.
	dropped atomic.Int64
	wg      sync.WaitGroup
}

func (c *Client) newDispatcher(ctx context.Context) *dispatcher {
	n := c.opts.HandlerConcurrency
	return &dispatcher{c: c, ctx: ctx, sem: make(chan struct{}, n), queued: make(chan struct{}, n*dispatchQueueFactor),
		unshed: make(chan struct{}, maxUnshedInFlight)}
}

// dispatchQueueFactor times HandlerConcurrency events may wait for a slot.
const dispatchQueueFactor = 16

// neverShed are the events that are never dropped: the session's own
// (READY, RESUMED) and interactions, where a drop would leave a member's
// command or click unanswered. They wait for a handler slot like any event
// but outside the queue's bound; the server bounds them (a few per member a
// minute, at most 2000 pending per bot).
var neverShed = map[string]bool{"READY": true, "RESUMED": true, "INTERACTION_CREATE": true}

// maxUnshedInFlight bounds the events of neverShed whose handlers have not
// returned: a little above the server's 2000 pending interactions per bot,
// so only a server that floods the bot reaches it.
const maxUnshedInFlight = 2048

// errUnshedOverflow ends a connection whose server sent more interactions
// than the bot's handlers hold: the client resumes before the event, so it is
// sent again rather than lost.
var errUnshedOverflow = errors.New("mnema: more interactions in flight than the server allows; reconnecting")

// dispatch starts the handlers of one event in a goroutine and returns at
// once, so the read loop, and with it the heartbeat ACKs, never waits for a
// handler. When the queue in front of the handler slots is full (handlers
// that block far longer than events arrive), the event is shed with a
// warning instead (never one of neverShed): blocking the read loop would
// leave ACKs unread, turn the connection into a zombie and replay the same
// flood after the resume.
//
// The events of neverShed are bounded on their own (maxUnshedInFlight); one
// beyond it is not dropped either: dispatch answers errUnshedOverflow, the
// connection closes, and the resume sends it again.
func (d *dispatcher) dispatch(name string, raw json.RawMessage) error {
	run := d.c.handlerFunc(name, raw)
	if run == nil {
		return nil
	}
	lane := d.queued
	if neverShed[name] {
		lane = d.unshed
	}
	select {
	case lane <- struct{}{}:
	default:
		if lane == d.unshed {
			d.c.logger.Error(errUnshedOverflow.Error(), "event", name, "in_flight", cap(d.unshed))
			return errUnshedOverflow
		}
		d.shed(name)
		return nil
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer func() { <-lane }()
		select {
		case d.sem <- struct{}{}:
		case <-d.ctx.Done():
			return
		}
		defer func() { <-d.sem }()
		defer func() {
			if p := recover(); p != nil {
				d.c.logger.Error("mnema: handler panicked", "event", name, "panic", p, "stack", string(debug.Stack()))
			}
		}()
		run(d.ctx)
	}()
	return nil
}

func (d *dispatcher) wait() { d.wg.Wait() }

// shed logs a dropped event: the first one, then every 1000th, with the total.
func (d *dispatcher) shed(name string) {
	if n := d.dropped.Add(1); n == 1 || n%1000 == 0 {
		d.c.logger.Warn("mnema: handlers are too slow, events dropped", "event", name, "dropped", n,
			"queue", cap(d.queued), "concurrency", cap(d.sem))
	}
}

// handlerFunc returns the work for one event: decoding it and calling its
// handlers in order, or nil when no handler wants it.
func (c *Client) handlerFunc(name string, raw json.RawMessage) func(context.Context) {
	h := c.snapshotHandlers()
	typed := c.typedHandler(&h, name, raw)
	if typed == nil && len(h.raw) == 0 {
		return nil
	}
	return func(ctx context.Context) {
		for _, f := range h.raw {
			f(ctx, name, raw)
		}
		if typed != nil {
			typed(ctx)
		}
	}
}

func (c *Client) typedHandler(h *handlers, name string, raw json.RawMessage) func(context.Context) {
	switch name {
	case "READY":
		return typed(c, name, raw, h.ready, nil)
	case "RESUMED":
		return resumedHandler(h.resumed)
	case "MESSAGE_CREATE":
		return typed(c, name, raw, h.messageCreate, nil)
	case "MESSAGE_UPDATE":
		return typed(c, name, raw, h.messageUpdate, nil)
	case "MESSAGE_DELETE":
		return typed(c, name, raw, h.messageDelete, nil)
	case "MESSAGE_REACTIONS_UPDATE":
		return typed(c, name, raw, h.reactionsUpdate, nil)
	case "CHANNELS_UPDATE":
		return typed(c, name, raw, h.channelsUpdate, nil)
	case "GUILD_MEMBER_ADD":
		return typed(c, name, raw, h.memberAdd, nil)
	case "INTERACTION_CREATE":
		return typed(c, name, raw, h.interaction, func(i *Interaction) { i.rest = c.REST })
	}
	return nil
}

func resumedHandler(list []func(context.Context)) func(context.Context) {
	if len(list) == 0 {
		return nil
	}
	return func(ctx context.Context) {
		for _, f := range list {
			f(ctx)
		}
	}
}

// typed decodes raw into a fresh T per handler call site and runs the
// handlers with it; prepare fills unexported fields.
func typed[T any](c *Client, name string, raw json.RawMessage, list []func(context.Context, *T), prepare func(*T)) func(context.Context) {
	if len(list) == 0 {
		return nil
	}
	return func(ctx context.Context) {
		v := new(T)
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, v); err != nil {
				c.logger.Warn("mnema: undecodable dispatch", "event", name, "err", err)
				return
			}
		}
		if prepare != nil {
			prepare(v)
		}
		for _, f := range list {
			f(ctx, v)
		}
	}
}
