package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// Delivery limits. Pending deliveries live in Postgres (queue.go); the memory
// queue below is only the buffer between the event hub and the database.
const (
	queueSize = 1000
	// perChannelQueue is how much of the queue one channel may hold, so a
	// channel with a flood of events cannot starve the webhooks of others.
	perChannelQueue = queueSize / 4
	workers         = 4
	requestTimeout  = 10 * time.Second
	// readLimit is how much of a response is read (and then dropped). The
	// response is never stored or shown: it could come from an internal service.
	readLimit = 64 << 10
	// ratePerSecond is the global ceiling of delivery attempts.
	ratePerSecond = 20
	maxRetryAfter = 10 * time.Minute
	reloadEvery   = time.Minute // how often the list of webhooks is read again
)

// defaultBackoff is the wait before each retry: an event is tried up to six
// times in about 45 minutes.
var defaultBackoff = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute}

// Event is one thing that happened, ready to be delivered.
type Event struct {
	ID   uuid.UUID
	Type string
	// Channel is the channel it happened in (uuid.Nil for server events).
	Channel uuid.UUID
	Time    time.Time
	Data    json.RawMessage
}

// Envelope is the JSON body of a delivery.
type Envelope struct {
	// ID is unique per event; retries repeat it (use it to deduplicate).
	ID string `json:"id" format:"uuid"`
	// T is the event type, as in Discord's gateway.
	T string `json:"t"`
	// Timestamp is when the event happened.
	Timestamp time.Time `json:"timestamp" format:"date-time"`
	// WebhookID is the outgoing webhook receiving it.
	WebhookID string `json:"webhook_id" format:"uuid"`
	// ChannelID is set for events of a channel.
	ChannelID *string `json:"channel_id,omitempty" binding:"optional" format:"uuid"`
	// D is the event data: the same object the WebSocket sends.
	D json.RawMessage `json:"d" swaggertype:"object"`
}

// Sign returns the X-Mnema-Signature header value for body at time t:
// "t=<unix>,v1=<hex hmac-sha256(secret, "<unix>.<body>")>".
func Sign(secret string, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

type job struct {
	webhook uuid.UUID
	event   Event
	attempt int
	// seq identifies the row in outgoing_queue (0 while only buffered).
	seq int64
}

// route is the cached part of a webhook needed to decide whether an event
// concerns it.
type route struct {
	id      uuid.UUID
	channel uuid.UUID
	events  []string
}

// Dispatcher delivers events to outgoing webhooks. Observe buffers events in
// memory (fast, never blocking); a writer moves them into the outgoing_queue
// table, which is the source of truth; workers claim due rows from there
// (FOR UPDATE SKIP LOCKED), attempt them and settle or reschedule them. A
// restart therefore loses at most the few events still in the buffer.
type Dispatcher struct {
	DB      *db.Pool
	Client  *http.Client
	Version string
	// ReloadEvery replaces reloadEvery, PollEvery pollEvery, PruneEvery pruneEvery (tests); set them
	// before Run.
	ReloadEvery time.Duration
	PollEvery   time.Duration
	PruneEvery  time.Duration

	mu    sync.Mutex
	waits []time.Duration // nil: defaultBackoff

	routes atomic.Pointer[[]route]
	queue  chan job
	// queued counts the jobs in queue per channel; guarded by qmu.
	qmu     sync.Mutex
	queued  map[uuid.UUID]int
	dropped atomic.Int64
	wg      sync.WaitGroup
	// wake tells an idle worker to look for due rows.
	wake    chan struct{}
	started atomic.Bool
	done    chan struct{}
}

// NewDispatcher returns a Dispatcher; call Run to start it.
func NewDispatcher(p *db.Pool, client *http.Client, version string) *Dispatcher {
	d := &Dispatcher{DB: p, Client: client, Version: version, queue: make(chan job, queueSize), queued: map[uuid.UUID]int{}, ReloadEvery: reloadEvery, PollEvery: pollEvery, PruneEvery: pruneEvery, wake: make(chan struct{}, 1), done: make(chan struct{})}
	empty := []route{}
	d.routes.Store(&empty)
	return d
}

// SetBackoff replaces the waits before each retry. For tests.
func (d *Dispatcher) SetBackoff(steps []time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.waits = steps
}

// Reload refreshes the cached list of webhooks. It runs after every change
// made through the API, and every minute.
func (d *Dispatcher) Reload(ctx context.Context) error {
	if d.DB == nil { // router built without a database (tests)
		return nil
	}
	rows, err := d.DB.Query(ctx, `SELECT id, channel_id, events FROM outgoing_webhooks WHERE enabled`)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (r route, err error) {
		err = row.Scan(&r.id, &r.channel, &r.events)
		return
	})
	if err != nil {
		return err
	}
	d.routes.Store(&list)
	return nil
}

// Run reloads the webhook list regularly, moves buffered events into the
// database and works the queue until ctx ends. On shutdown it stops claiming,
// writes what is still buffered, lets attempts in flight finish (for at most
// shutdownGrace) and leaves everything else in the database for the next
// start.
func (d *Dispatcher) Run(ctx context.Context) {
	d.started.Store(true)
	defer close(d.done)
	if d.DB == nil { // router built without a database (tests): nothing to deliver
		<-ctx.Done()
		return
	}
	if err := d.Reload(ctx); err != nil {
		slog.Warn("outgoing webhooks: load", "err", err)
	}
	d.prune(ctx)
	tick := time.NewTicker(time.Second / ratePerSecond)
	defer tick.Stop()
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.persist(ctx)
	}()
	for range workers {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.work(ctx, tick.C)
		}()
	}
	reload := time.NewTicker(d.ReloadEvery)
	defer reload.Stop()
	poll := time.NewTicker(d.PollEvery)
	defer poll.Stop()
	pruneT := time.NewTicker(d.PruneEvery)
	defer pruneT.Stop()
	for {
		select {
		case <-ctx.Done():
			d.wg.Wait()
			return
		case <-reload.C:
			if err := d.Reload(ctx); err != nil {
				slog.Warn("outgoing webhooks: reload", "err", err)
			}
		case <-poll.C:
			d.signal()
		case <-pruneT.C:
			d.prune(ctx)
		}
	}
}

// Wait blocks until Run has finished (at most max); it returns at once when
// Run never started. The server calls it before closing the database.
func (d *Dispatcher) Wait(max time.Duration) {
	if !d.started.Load() {
		return
	}
	select {
	case <-d.done:
	case <-time.After(max):
	}
}

// signal wakes a worker; it never blocks.
func (d *Dispatcher) signal() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Observe is the hook for the event hub: it receives every event the hub
// publishes (channel is uuid.Nil for events to everybody) and queues the
// events of a channel for the webhooks of that channel that subscribed to
// them. It never blocks.
func (d *Dispatcher) Observe(channel uuid.UUID, eventType string, payload any) {
	name, data, ok := translate(eventType, payload)
	if !ok || channel == uuid.Nil {
		return
	}
	var targets []uuid.UUID
	for _, r := range *d.routes.Load() {
		if !slices.Contains(r.events, name) {
			continue
		}
		if r.channel != channel {
			continue
		}
		targets = append(targets, r.id)
	}
	if len(targets) == 0 {
		return
	}
	raw, _ := json.Marshal(data) // hub payloads are plain data: cannot fail
	ev := Event{ID: uuid.New(), Type: name, Channel: channel, Time: time.Now().UTC(), Data: raw}
	for _, id := range targets {
		d.enqueue(job{webhook: id, event: ev, attempt: 1})
	}
}
