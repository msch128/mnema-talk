package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/ssrf"
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

// persist moves buffered events into the database in batches.
func (d *Dispatcher) persist(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.flush() // what is still buffered must not be lost with the process
			return
		case first := <-d.queue:
			d.store(ctx, d.gather(first))
		}
	}
}

// gather takes up to storeBatch buffered jobs, first included.
func (d *Dispatcher) gather(first job) []job {
	batch := []job{first}
	d.dequeued(first)
	for len(batch) < storeBatch {
		select {
		case j := <-d.queue:
			d.dequeued(j)
			batch = append(batch, j)
		default:
			return batch
		}
	}
	return batch
}

// flush writes the whole buffer (at shutdown).
func (d *Dispatcher) flush() {
	for {
		select {
		case first := <-d.queue:
			d.store(context.Background(), d.gather(first))
		default:
			return
		}
	}
}

func (d *Dispatcher) store(ctx context.Context, batch []job) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
	defer cancel()
	perWebhook, global, err := storeEvents(ctx, d.DB, batch)
	if err != nil {
		slog.Warn("outgoing webhooks: store events", "err", err, "events", len(batch))
		d.count("store_error", int64(len(batch)))
		return
	}
	d.count("webhook_cap", perWebhook)
	d.count("global_cap", global)
	d.signal()
}

// count adds n dropped events to the metric and the dropped total.
func (d *Dispatcher) count(reason string, n int64) {
	if n <= 0 {
		return
	}
	metrics.WebhookDropped.With(reason).Add(uint64(n))
	total := d.dropped.Add(n)
	if prev := total - n; prev == 0 || prev/100 != total/100 {
		slog.Warn("outgoing webhooks: events dropped", "reason", reason, "dropped_total", total)
	}
}

// prune gives up rows that outlived the last backoff step.
func (d *Dispatcher) prune(ctx context.Context) {
	n, err := pruneQueue(ctx, d.DB)
	if err != nil {
		slog.Warn("outgoing webhooks: prune", "err", err)
		return
	}
	d.count("expired", n)
}

// work claims due rows one at a time and attempts them, one attempt per tick
// of the global rate limiter.
func (d *Dispatcher) work(ctx context.Context, tick <-chan time.Time) {
	for ctx.Err() == nil {
		j, ok, err := claimJob(ctx, d.DB)
		if err != nil && ctx.Err() == nil {
			slog.Warn("outgoing webhooks: claim", "err", err)
		}
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-d.wake:
			}
			continue
		}
		d.signal() // there may be more: let an idle worker look too
		select {
		case <-ctx.Done():
			// Shutting down: hand the row back instead of holding it for the lease.
			if err := releaseJob(context.WithoutCancel(ctx), d.DB, j.seq); err != nil {
				slog.Warn("outgoing webhooks: release", "err", err)
			}
			return
		case <-tick: // one attempt per tick, shared by the workers
		}
		d.process(ctx, j)
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

func (d *Dispatcher) enqueue(j job) {
	d.qmu.Lock()
	if d.queued[j.event.Channel] >= perChannelQueue {
		d.qmu.Unlock()
		d.count("channel_share", 1)
		return
	}
	d.queued[j.event.Channel]++
	d.qmu.Unlock()
	select {
	case d.queue <- j:
	default:
		// A full queue means the receivers are too slow for the flow of
		// events; the oldest work is kept, the newest dropped.
		d.dequeued(j)
		d.count("buffer_full", 1)
	}
}

// dequeued releases j's place in its channel's share of the queue.
func (d *Dispatcher) dequeued(j job) {
	d.qmu.Lock()
	if d.queued[j.event.Channel]--; d.queued[j.event.Channel] <= 0 {
		delete(d.queued, j.event.Channel)
	}
	d.qmu.Unlock()
}

// translate maps a hub event to an outgoing one. Messages posted by a
// webhook do not leave again, so two webhooks cannot feed each other.
func translate(eventType string, payload any) (name string, data any, ok bool) {
	switch eventType {
	case "message_create", "message_update":
		var m chat.Message
		switch v := payload.(type) {
		case chat.Message:
			m = v
		case *chat.Message:
			m = *v
		default:
			return "", nil, false
		}
		if m.IsBot && eventType == "message_create" {
			return "", nil, false
		}
		return map[string]string{"message_create": EventMessageCreate, "message_update": EventMessageUpdate}[eventType], m, true
	case "message_delete":
		return EventMessageDelete, payload, true
	case "message_reaction":
		return EventReactionsUpdate, payload, true
	case "voice_state_update":
		return EventVoiceStateUpdate, payload, true
	}
	return "", nil, false
}

// Outcome codes stored in the delivery log: short, never the raw error.
func classify(err error, status int) string {
	switch {
	case err == nil && status >= 300 && status < 400:
		return "redirect_not_followed"
	case err == nil:
		return "http_" + strconv.Itoa(status)
	case errors.Is(err, ssrf.ErrBlocked):
		return "blocked_address"
	case errors.Is(err, ssrf.ErrResolve):
		return "dns_error"
	case errors.Is(err, ssrf.ErrConnect):
		return "connect_error"
	case errors.Is(err, ssrf.ErrBadURL):
		return "invalid_url"
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		return "timeout"
	case strings.Contains(err.Error(), "tls:") || strings.Contains(err.Error(), "x509:"):
		return "tls_error"
	}
	return "request_error"
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

func (d *Dispatcher) backoff(attempt int) time.Duration {
	d.mu.Lock()
	steps := d.waits
	d.mu.Unlock()
	if steps == nil {
		steps = defaultBackoff
	}
	if attempt > len(steps) {
		return -1
	}
	base := steps[attempt-1]
	return base + time.Duration(rand.Int64N(int64(base)/5+1)) - base/10 // +-10 %
}

// process makes one attempt of a claimed row and, in the same transaction
// that logs it, deletes the row (settled) or schedules the next attempt.
// A crash between the request and that transaction repeats the delivery
// (at least once; receivers deduplicate on the delivery id).
func (d *Dispatcher) process(ctx context.Context, j job) {
	// Bookkeeping completes even while shutting down.
	wctx := context.WithoutCancel(ctx)
	rec, err := getOutgoing(wctx, d.DB, j.webhook)
	switch {
	case errors.Is(err, errOutgoingNotFound) || (err == nil && !rec.enabled):
		if err := dropRow(wctx, d.DB, j.seq); err != nil {
			slog.Warn("outgoing webhooks: drop", "err", err)
		}
		return
	case err != nil:
		slog.Warn("outgoing webhooks: load webhook", "err", err) // the lease makes the row due again
		return
	}
	// Attempts already under way when shutdown begins get shutdownGrace to finish.
	actx, cancel := context.WithCancel(wctx)
	grace := shutdownGrace
	stop := context.AfterFunc(ctx, func() { time.AfterFunc(grace, cancel) })
	del, retryAfter := d.attempt(actx, rec, j.event, j.attempt)
	aborted := actx.Err() != nil
	stop()
	cancel()
	if aborted { // cut off by the shutdown: not an outcome, leave the row for the next start
		if err := releaseJob(wctx, d.DB, j.seq); err != nil {
			slog.Warn("outgoing webhooks: release", "err", err)
		}
		return
	}
	wait := d.backoff(j.attempt)
	retry := !del.Success && retriable(del) && wait >= 0
	settle := settleStep(j.seq)
	if retry {
		wait = max(wait, min(retryAfter, maxRetryAfter))
		settle = rescheduleStep(j.seq, j.attempt+1, wait)
	}
	off, err := recordDelivery(wctx, d.DB, rec.ID, del, !retry, true, settle)
	if err != nil {
		slog.Warn("outgoing webhooks: record delivery", "err", err)
		return
	}
	if !del.Success && !retry {
		d.afterFinal(wctx, rec.ID, off)
	}
}

// afterFinal refreshes the cached list after a failure for good, which may
// have switched the webhook off; then nothing more is sent to it.
func (d *Dispatcher) afterFinal(ctx context.Context, id uuid.UUID, off bool) {
	if err := d.Reload(ctx); err != nil {
		slog.Warn("outgoing webhooks: reload", "webhook", id, "err", err)
	}
	if off {
		if err := purgeWebhook(ctx, d.DB, id); err != nil {
			slog.Warn("outgoing webhooks: purge", "webhook", id, "err", err)
		}
	}
}

// retriable reports whether a failed attempt is worth repeating: network
// problems, timeouts, 5xx, 408 and 429, but not a refusal by the address
// guard or other client errors.
func retriable(d Delivery) bool {
	if d.Error == nil {
		return false
	}
	switch *d.Error {
	case "blocked_address", "invalid_url", "redirect_not_followed":
		return false
	}
	if d.StatusCode != nil {
		s := *d.StatusCode
		return s >= 500 || s == 408 || s == 429
	}
	return true
}

// attempt makes one HTTP request. retryAfter is the receiver's Retry-After.
func (d *Dispatcher) attempt(ctx context.Context, rec *outgoingRec, ev Event, n int) (Delivery, time.Duration) {
	del := Delivery{EventID: ev.ID, EventType: ev.Type, Attempt: n}
	body, _ := json.Marshal(newEnvelope(rec, ev))
	start := time.Now()
	status, retryAfter, err := d.post(ctx, rec, ev, body)
	del.DurationMS = int(time.Since(start).Milliseconds())
	if status != 0 {
		del.StatusCode = &status
	}
	if err == nil && status >= 200 && status < 300 {
		del.Success = true
		return del, 0
	}
	code := classify(err, status)
	del.Error = &code
	return del, retryAfter
}

func newEnvelope(rec *outgoingRec, ev Event) Envelope {
	env := Envelope{ID: ev.ID.String(), T: ev.Type, Timestamp: ev.Time, WebhookID: rec.ID.String(), D: ev.Data}
	if ev.Channel != uuid.Nil {
		c := ev.Channel.String()
		env.ChannelID = &c
	}
	return env
}

func (d *Dispatcher) post(ctx context.Context, rec *outgoingRec, ev Event, body []byte) (status int, retryAfter time.Duration, err error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rec.URL, bytes.NewReader(body))
	if err != nil {
		return 0, 0, ssrf.ErrBadURL
	}
	now := time.Now()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MnemaTalk-Webhook/"+d.Version)
	req.Header.Set("X-Mnema-Event", ev.Type)
	req.Header.Set("X-Mnema-Delivery", ev.ID.String())
	req.Header.Set("X-Mnema-Signature", Sign(rec.secret, now, body))
	resp, err := d.Client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, readLimit))
	if secs, convErr := strconv.Atoi(resp.Header.Get("Retry-After")); convErr == nil && secs > 0 {
		retryAfter = time.Duration(secs) * time.Second
	}
	return resp.StatusCode, retryAfter, nil
}

// SendTest delivers a PING to the webhook once, right now, and returns the
// attempt. It neither retries nor counts towards switching the webhook off.
func (d *Dispatcher) SendTest(ctx context.Context, rec *outgoingRec) (Delivery, error) {
	data, _ := json.Marshal(map[string]string{"message": "Test event from Mnema Talk"})
	ev := Event{ID: uuid.New(), Type: EventPing, Time: time.Now().UTC(), Data: data}
	del, _ := d.attempt(ctx, rec, ev, 1)
	_, err := recordDelivery(ctx, d.DB, rec.ID, del, false, false)
	return del, err
}
