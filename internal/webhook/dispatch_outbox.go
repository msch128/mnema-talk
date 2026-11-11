package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/events"
)

// The events of a database change (a message posted, edited or deleted, a
// reaction) are written to outgoing_queue by the transaction that makes the
// change: Wants and Record are the core.EventOutbox the message code calls.
// The hub's publication after the commit only wakes the workers. A crash
// between the commit and the publication therefore loses nothing: the rows
// are found by the next poll or the next start.

// transactional reports whether the hub event is written by its own
// transaction and must not be queued again when the hub publishes it.
func transactional(eventType string) bool {
	switch eventType {
	case events.MessageCreate, events.MessageUpdate, events.MessageDelete, events.MessageReaction:
		return true
	}
	return false
}

// outgoingName is the name an outgoing webhook subscribes to for a hub event.
var outgoingName = map[string]string{
	events.MessageCreate:    EventMessageCreate,
	events.MessageUpdate:    EventMessageUpdate,
	events.MessageDelete:    EventMessageDelete,
	events.MessageReaction:  EventReactionsUpdate,
	events.VoiceStateUpdate: EventVoiceStateUpdate,
}

// Wants reports whether a webhook of the channel receives the hub event.
func (d *Dispatcher) Wants(channel uuid.UUID, eventType string) bool {
	name, ok := outgoingName[eventType]
	return ok && channel != uuid.Nil && len(matchingWebhookTargets(*d.routes.Load(), channel, name)) > 0
}

// recordBatch is the events of one Record call as the columns of its insert.
type recordBatch struct {
	ids   []uuid.UUID
	names []string
	times []time.Time
	data  [][]byte
}

func (b *recordBatch) add(ev events.Event) {
	name, ok := translate(ev)
	if !ok {
		return
	}
	raw, err := json.Marshal(ev)
	if err != nil { // hub payloads are plain data; never fail a message for a webhook
		slog.Warn("outgoing webhooks: encode event", "event", name, "err", err)
		return
	}
	b.ids, b.names = append(b.ids, uuid.New()), append(b.names, name)
	b.times, b.data = append(b.times, time.Now().UTC()), append(b.data, raw)
}

// Record writes one row per event and receiving webhook in tx, with one
// statement however many events there are (a sweep removes 200 messages at a
// time while it holds their row locks). The rows are in the order of evs. The
// webhooks are read from the table under a key-share lock, not from the
// cached list, so a webhook deleted a moment ago cannot fail the caller's
// transaction.
//
// A row is due from the moment it is written (clock_timestamp), not from the
// start of its transaction (the column's default, NOW()): the callers record
// last, holding the row lock of the message they changed, so two changes of
// one message are claimed in the order they took effect, whichever
// transaction began first.
func (d *Dispatcher) Record(ctx context.Context, tx pgx.Tx, channel uuid.UUID, evs ...events.Event) error {
	var b recordBatch
	for _, ev := range evs {
		b.add(ev)
	}
	if len(b.ids) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outgoing_queue (webhook_id, channel_id, event_id, event_type, event_time, data, next_attempt_at)
		SELECT w.id, $1, e.id, e.name, e.at, e.data, clock_timestamp()
		FROM unnest($2::uuid[], $3::text[], $4::timestamptz[], $5::bytea[]) WITH ORDINALITY AS e(id, name, at, data, ord)
		JOIN outgoing_webhooks w ON w.channel_id = $1 AND w.enabled AND e.name = ANY(w.events)
		ORDER BY e.ord, w.id FOR KEY SHARE OF w`,
		channel, b.ids, b.names, b.times, b.data); err != nil {
		return fmt.Errorf("record outgoing event: %w", err)
	}
	return nil
}

// recorded is the hub's publication of an event its transaction already
// wrote: wake a worker, and have the caps looked at.
func (d *Dispatcher) recorded() {
	d.capsDue.Store(true)
	d.signal()
}

// applyCaps drops the oldest pending rows past the bounds, when rows were
// written since the last look. The transactions that write them stay free of
// it: a message must not wait for the queue's housekeeping.
func (d *Dispatcher) applyCaps(ctx context.Context) {
	if !d.capsDue.Swap(false) {
		return
	}
	perWebhook, global, err := capQueue(ctx, d.DB)
	if err != nil {
		d.capsDue.Store(true)
		slog.Warn("outgoing webhooks: apply caps", "err", err)
		return
	}
	d.count("webhook_cap", perWebhook)
	d.count("global_cap", global)
}
