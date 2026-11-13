package mnema

import (
	"context"
	"encoding/json"
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
	wg  sync.WaitGroup
}

func (c *Client) newDispatcher(ctx context.Context) *dispatcher {
	return &dispatcher{c: c, ctx: ctx, sem: make(chan struct{}, c.opts.HandlerConcurrency)}
}

// dispatch starts the handlers of one event in a goroutine and returns at
// once, so the read loop (and with it the heartbeat ACKs) never waits for a
// handler. The goroutine waits for a slot of the concurrency bound first.
func (d *dispatcher) dispatch(name string, raw json.RawMessage) {
	run := d.c.handlerFunc(name, raw)
	if run == nil {
		return
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
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
}

func (d *dispatcher) wait() { d.wg.Wait() }

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
