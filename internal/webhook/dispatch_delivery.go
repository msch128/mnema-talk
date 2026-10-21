package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/ssrf"
)

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

func shouldDropWebhook(rec *outgoingRec, err error) bool {
	if errors.Is(err, errOutgoingNotFound) {
		return true
	}
	return err == nil && !rec.enabled
}

func (d *Dispatcher) runAttemptWithGrace(ctx context.Context, wctx context.Context, rec *outgoingRec, j job) (Delivery, time.Duration, bool) {
	actx, cancel := context.WithCancel(wctx)
	grace := shutdownGrace
	stop := context.AfterFunc(ctx, func() { time.AfterFunc(grace, cancel) })
	del, retryAfter := d.attempt(actx, rec, j.event, j.attempt)
	aborted := actx.Err() != nil
	stop()
	cancel()
	return del, retryAfter, aborted
}

func (d *Dispatcher) computeSettleStep(j job, del Delivery, retryAfter time.Duration) (step, bool) {
	wait := d.backoff(j.attempt)
	retry := !del.Success && retriable(del) && wait >= 0
	if !retry {
		return settleStep(j.seq), false
	}
	wait = max(wait, min(retryAfter, maxRetryAfter))
	return rescheduleStep(j.seq, j.attempt+1, wait), true
}

func (d *Dispatcher) loadActiveWebhook(ctx context.Context, j job) (*outgoingRec, bool) {
	rec, err := getOutgoing(ctx, d.DB, j.webhook)
	if shouldDropWebhook(rec, err) {
		if dropErr := dropRow(ctx, d.DB, j.seq); dropErr != nil {
			slog.Warn("outgoing webhooks: drop", "err", dropErr)
		}
		return nil, false
	}
	if err != nil {
		slog.Warn("outgoing webhooks: load webhook", "err", err) // the lease makes the row due again
		return nil, false
	}
	return rec, true
}

func (d *Dispatcher) recordDeliveryResult(ctx context.Context, rec *outgoingRec, j job, del Delivery, retryAfter time.Duration) {
	settle, retry := d.computeSettleStep(j, del, retryAfter)
	off, recErr := recordDelivery(ctx, d.DB, rec.ID, del, !retry, true, settle)
	if recErr != nil {
		slog.Warn("outgoing webhooks: record delivery", "err", recErr)
		return
	}
	if !del.Success && !retry {
		d.afterFinal(ctx, rec.ID, off)
	}
}

// process makes one attempt of a claimed row and, in the same transaction
// that logs it, deletes the row (settled) or schedules the next attempt.
// A crash between the request and that transaction repeats the delivery
// (at least once; receivers deduplicate on the delivery id).
func (d *Dispatcher) process(ctx context.Context, j job) {
	// Bookkeeping completes even while shutting down.
	wctx := context.WithoutCancel(ctx)
	rec, ok := d.loadActiveWebhook(wctx, j)
	if !ok {
		return
	}

	del, retryAfter, aborted := d.runAttemptWithGrace(ctx, wctx, rec, j)
	if aborted { // cut off by the shutdown: not an outcome, leave the row for the next start
		if relErr := releaseJob(wctx, d.DB, j.seq); relErr != nil {
			slog.Warn("outgoing webhooks: release", "err", relErr)
		}
		return
	}

	d.recordDeliveryResult(wctx, rec, j, del, retryAfter)
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
	body, err := json.Marshal(newEnvelope(rec, ev))
	if err != nil {
		code := "marshal_error"
		del.Error = &code
		return del, 0
	}
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
	if _, drainErr := io.Copy(io.Discard, io.LimitReader(resp.Body, readLimit)); drainErr != nil {
		slog.Debug("outgoing webhooks: drain body", "err", drainErr)
	}
	if secs, convErr := strconv.Atoi(resp.Header.Get("Retry-After")); convErr == nil && secs > 0 {
		retryAfter = time.Duration(secs) * time.Second
	}
	return resp.StatusCode, retryAfter, nil
}

// SendTest delivers a PING to the webhook once, right now, and returns the
// attempt. It neither retries nor counts towards switching the webhook off.
func (d *Dispatcher) SendTest(ctx context.Context, rec *outgoingRec) (Delivery, error) {
	data, err := json.Marshal(map[string]string{"message": "Test event from Mnema Talk"})
	if err != nil {
		return Delivery{}, err
	}
	ev := Event{ID: uuid.New(), Type: EventPing, Time: time.Now().UTC(), Data: data}
	del, retryAfter := d.attempt(ctx, rec, ev, 1)
	if retryAfter > 0 {
		slog.Debug("outgoing webhooks: test retry-after", "retry_after", retryAfter)
	}
	off, recErr := recordDelivery(ctx, d.DB, rec.ID, del, false, false)
	if off {
		slog.Debug("outgoing webhooks: test disabled webhook", "webhook", rec.ID)
	}
	return del, recErr
}
