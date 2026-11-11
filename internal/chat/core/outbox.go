package core

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
)

// EventOutbox takes the events of a channel that outgoing webhooks receive,
// inside the transaction that causes them: the event and the change commit
// together or not at all, so a crash between the commit and the publication
// to the hub loses no delivery. The webhook dispatcher implements it; core
// only names it, so the packages that write messages need not import it.
type EventOutbox interface {
	// Wants reports whether any webhook of the channel receives eventType (a
	// name from internal/events). It reads a cached list and never blocks.
	Wants(channel uuid.UUID, eventType string) bool
	// Record stores the events in tx, in the order given and with one
	// statement however many they are: the very events the hub publishes.
	Record(ctx context.Context, tx pgx.Tx, channel uuid.UUID, evs ...events.Event) error
}

// outboxes maps a pool to its outbox, as automod.Of does for its engine.
var outboxes sync.Map

// AttachOutbox makes o the outbox of everything stored through p.
func AttachOutbox(p *db.Pool, o EventOutbox) {
	if p != nil {
		outboxes.Store(p, o)
	}
}

// DetachOutbox removes o as the outbox of p, unless another took its place.
func DetachOutbox(p *db.Pool, o EventOutbox) { outboxes.CompareAndDelete(p, o) }

// RecordEvent stores the event in tx when a webhook of the channel receives
// it. build makes the event the hub will publish; it only runs when somebody
// listens, so a channel without webhooks pays nothing.
func RecordEvent(ctx context.Context, p *db.Pool, tx pgx.Tx, channel uuid.UUID, eventType string, build func() (events.Event, error)) error {
	return RecordEvents(ctx, p, tx, channel, eventType, func() ([]events.Event, error) {
		ev, err := build()
		return []events.Event{ev}, err
	})
}

// RecordEvents is RecordEvent for several events of one type (a bulk delete,
// a sweep): they are stored together, in the order build returns them.
func RecordEvents(ctx context.Context, p *db.Pool, tx pgx.Tx, channel uuid.UUID, eventType string, build func() ([]events.Event, error)) error {
	v, ok := outboxes.Load(p)
	if !ok {
		return nil
	}
	o := v.(EventOutbox)
	if !o.Wants(channel, eventType) {
		return nil
	}
	evs, err := build()
	if err != nil || len(evs) == 0 {
		return err
	}
	return o.Record(ctx, tx, channel, evs...)
}
