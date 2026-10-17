package webhook

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Outgoing event types. Names follow Discord's gateway where an equivalent
// exists.
const (
	EventMessageCreate    = "MESSAGE_CREATE"
	EventMessageUpdate    = "MESSAGE_UPDATE"
	EventMessageDelete    = "MESSAGE_DELETE"
	EventReactionsUpdate  = "MESSAGE_REACTIONS_UPDATE"
	EventVoiceStateUpdate = "VOICE_STATE_UPDATE"
	// EventPing is the test event; it cannot be subscribed to.
	EventPing = "PING"
)

// channelEvents are the events a channel's webhook can subscribe to.
var channelEvents = []string{EventMessageCreate, EventMessageUpdate, EventMessageDelete, EventReactionsUpdate, EventVoiceStateUpdate}

const (
	// maxOutgoingPerChannel bounds the outgoing webhooks of one channel.
	maxOutgoingPerChannel = 10
	// deliveryLogSize is how many deliveries are kept per webhook.
	deliveryLogSize = 50
	// disableAfter is the number of events in a row that failed for good
	// (after their retries) before a webhook is switched off.
	disableAfter = 5
)

var errOutgoingNotFound = httpx.ErrNotFound("outgoing webhook not found")

// Outgoing is an outgoing webhook as the management API shows it. The secret
// is only ever shown once, in CreatedOutgoing.
type Outgoing struct {
	ID uuid.UUID `json:"id" format:"uuid"`
	// ChannelID is the channel the webhook belongs to; it receives that
	// channel's events only.
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Name      string    `json:"name" maxLength:"80"`
	URL       string    `json:"url" maxLength:"2048"`
	// Events are the subscribed event types (see doc/webhooks.md).
	Events []string `json:"events"`
	// Status is "active", "disabled" (switched off by a member) or
	// "auto_disabled" (switched off after repeated failures).
	Status              string `json:"status" enums:"active,disabled,auto_disabled"`
	ConsecutiveFailures int    `json:"consecutive_failures" minimum:"0"`
	// LastStatus is the HTTP status of the latest attempt (null: none yet, or
	// no response).
	LastStatus *int `json:"last_status" extensions:"x-nullable"`
	// LastError is a short code for the latest failure ("timeout",
	// "blocked_address", "http_500", ...); null after a success.
	LastError     *string    `json:"last_error" extensions:"x-nullable"`
	LastAttemptAt *time.Time `json:"last_attempt_at" format:"date-time" extensions:"x-nullable"`
	LastSuccessAt *time.Time `json:"last_success_at" format:"date-time" extensions:"x-nullable"`
	// CreatedBy is the member who created it, kept for the record (null once
	// that account is gone). It grants nothing.
	CreatedBy *uuid.UUID `json:"created_by" format:"uuid" extensions:"x-nullable"`
	CreatedAt time.Time  `json:"created_at" format:"date-time"`
	// URLSetBy and URLSetAt record who last set the URL and when, so stale
	// webhooks are easy to spot. The URL receives the channel's events until an
	// administrator changes or deletes the webhook.
	URLSetBy *uuid.UUID `json:"url_set_by" format:"uuid" extensions:"x-nullable"`
	URLSetAt time.Time  `json:"url_set_at" format:"date-time"`
}

// CreatedOutgoing is the answer to creating a webhook or rotating its secret:
// the only time the secret is shown.
type CreatedOutgoing struct {
	Outgoing
	// Secret signs every delivery (see X-Mnema-Signature in doc/webhooks.md).
	Secret string `json:"secret"`
}

// Delivery is one attempt to deliver an event.
type Delivery struct {
	ID         uuid.UUID `json:"id" format:"uuid"`
	EventID    uuid.UUID `json:"event_id" format:"uuid"`
	EventType  string    `json:"event_type"`
	Attempt    int       `json:"attempt" minimum:"1"`
	Success    bool      `json:"success"`
	StatusCode *int      `json:"status_code" extensions:"x-nullable"`
	// Error is a short code, never the response or the raw network error.
	Error      *string   `json:"error" extensions:"x-nullable"`
	DurationMS int       `json:"duration_ms" minimum:"0"`
	CreatedAt  time.Time `json:"created_at" format:"date-time"`
}

// outgoingRec is an outgoing webhook with what only the server needs.
type outgoingRec struct {
	Outgoing
	secret  string
	enabled bool
	autoOff bool
}

const outgoingSelect = `
	SELECT id, channel_id, created_by, name, url, secret, events, enabled, auto_disabled, consecutive_failures,
	       last_status, last_error, last_attempt_at, last_success_at, created_at, url_set_by, url_set_at
	FROM outgoing_webhooks`

func scanOutgoing(row pgx.Row) (*outgoingRec, error) {
	var rec outgoingRec
	err := row.Scan(&rec.ID, &rec.ChannelID, &rec.CreatedBy, &rec.Name, &rec.URL, &rec.secret, &rec.Events, &rec.enabled, &rec.autoOff,
		&rec.ConsecutiveFailures, &rec.LastStatus, &rec.LastError, &rec.LastAttemptAt, &rec.LastSuccessAt, &rec.CreatedAt, &rec.URLSetBy, &rec.URLSetAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errOutgoingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load outgoing webhook: %w", err)
	}
	switch {
	case rec.enabled:
		rec.Status = "active"
	case rec.autoOff:
		rec.Status = "auto_disabled"
	default:
		rec.Status = "disabled"
	}
	return &rec, nil
}

func getOutgoing(ctx context.Context, q db.Querier, id uuid.UUID) (*outgoingRec, error) {
	return scanOutgoing(q.QueryRow(ctx, outgoingSelect+` WHERE id = $1`, id))
}

// listOutgoing lists the webhooks of a channel.
func listOutgoing(ctx context.Context, p *db.Pool, channel uuid.UUID) ([]*outgoingRec, error) {
	rows, err := p.Query(ctx, outgoingSelect+` WHERE channel_id = $1 ORDER BY created_at, id`, channel)
	if err != nil {
		return nil, fmt.Errorf("list outgoing webhooks: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*outgoingRec, error) { return scanOutgoing(row) })
}

// newSecret returns a fresh signing secret.
func newSecret() string {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw) // never fails: Go aborts the program if the system has no randomness
	return "whsec_" + base64.RawURLEncoding.EncodeToString(raw)
}

// validEvents checks a subscription list against the event types and returns
// it sorted and without duplicates.
func validEvents(events []string) ([]string, error) {
	if len(events) == 0 {
		return nil, httpx.ErrInvalidInput("events must name at least one event")
	}
	allowed := channelEvents
	out := make([]string, 0, len(events))
	for _, e := range events {
		if !slices.Contains(allowed, e) {
			return nil, httpx.ErrInvalidInput("unknown event for this webhook: " + e)
		}
		if !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	slices.Sort(out)
	return out, nil
}

func createOutgoing(ctx context.Context, p *db.Pool, channel, creator uuid.UUID, name, url string, events []string) (*outgoingRec, string, error) {
	secret := newSecret()
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM outgoing_webhooks WHERE channel_id = $1`, channel).Scan(&n); err != nil {
			return err
		}
		if n >= maxOutgoingPerChannel {
			return httpx.ErrConflict(fmt.Sprintf("at most %d outgoing webhooks are allowed here", maxOutgoingPerChannel))
		}
		return tx.QueryRow(ctx, `
			INSERT INTO outgoing_webhooks (channel_id, created_by, url_set_by, name, url, secret, events)
			VALUES ($1, $2, $2, $3, $4, $5, $6) RETURNING id`, channel, creator, name, url, secret, events).Scan(&id)
	})
	if err != nil {
		if _, ok := httpx.AsAPIError(err); ok {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("create outgoing webhook: %w", err)
	}
	rec, err := getOutgoing(ctx, p, id)
	return rec, secret, err
}

// outgoingChange is a partial update; nil fields stay as they are.
type outgoingChange struct {
	name    *string
	url     *string
	events  []string
	enabled *bool
	// urlBy is who changes the URL, recorded with the change.
	urlBy *uuid.UUID
}

func updateOutgoing(ctx context.Context, p *db.Pool, id uuid.UUID, c outgoingChange) error {
	// Switching a webhook on again clears the failure state, so an
	// auto-disabled one starts over.
	_, err := p.Exec(ctx, `
		UPDATE outgoing_webhooks SET
		    name = COALESCE($2, name),
		    url = COALESCE($3, url),
		    url_set_by = CASE WHEN $3::text IS NULL THEN url_set_by ELSE $6 END,
		    url_set_at = CASE WHEN $3::text IS NULL THEN url_set_at ELSE NOW() END,
		    events = COALESCE($4, events),
		    enabled = COALESCE($5, enabled),
		    auto_disabled = CASE WHEN $5 IS NULL THEN auto_disabled ELSE FALSE END,
		    consecutive_failures = CASE WHEN $5 IS TRUE THEN 0 ELSE consecutive_failures END,
		    updated_at = NOW()
		WHERE id = $1`, id, c.name, c.url, c.events, c.enabled, c.urlBy)
	if err != nil {
		return fmt.Errorf("update outgoing webhook: %w", err)
	}
	return nil
}

// rotateSecret makes a new secret and changes nothing else.
func rotateSecret(ctx context.Context, p *db.Pool, id uuid.UUID) (string, error) {
	secret := newSecret()
	if _, err := p.Exec(ctx, `UPDATE outgoing_webhooks SET secret = $2, updated_at = NOW() WHERE id = $1`, id, secret); err != nil {
		return "", fmt.Errorf("rotate secret: %w", err)
	}
	return secret, nil
}

func deleteOutgoing(ctx context.Context, p *db.Pool, id uuid.UUID) error {
	if _, err := p.Exec(ctx, `DELETE FROM outgoing_webhooks WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete outgoing webhook: %w", err)
	}
	return nil
}

func listDeliveries(ctx context.Context, p *db.Pool, id uuid.UUID) ([]Delivery, error) {
	rows, err := p.Query(ctx, `
		SELECT id, event_id, event_type, attempt, success, status_code, error, duration_ms, created_at
		FROM outgoing_deliveries WHERE webhook_id = $1 ORDER BY created_at DESC, id LIMIT $2`, id, deliveryLogSize)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Delivery])
}

// recordDelivery logs an attempt and updates the webhook's state. final marks
// the end of an event's attempts (success, or failure for good): only a final
// failure counts towards switching the webhook off. It reports whether the
// webhook was switched off by this call.
func recordDelivery(ctx context.Context, p *db.Pool, id uuid.UUID, d Delivery, final, count bool) (switchedOff bool, err error) {
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO outgoing_deliveries (webhook_id, event_id, event_type, attempt, success, status_code, error, duration_ms)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, d.EventID, d.EventType, d.Attempt, d.Success, d.StatusCode, d.Error, d.DurationMS); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM outgoing_deliveries WHERE webhook_id = $1 AND id NOT IN (
			    SELECT id FROM outgoing_deliveries WHERE webhook_id = $1 ORDER BY created_at DESC, id LIMIT $2)`, id, deliveryLogSize); err != nil {
			return err
		}
		if !count {
			return nil
		}
		return tx.QueryRow(ctx, `
			UPDATE outgoing_webhooks SET
			    last_status = $2, last_error = $3, last_attempt_at = NOW(),
			    last_success_at = CASE WHEN $4 THEN NOW() ELSE last_success_at END,
			    consecutive_failures = CASE WHEN $4 THEN 0 WHEN $5 THEN consecutive_failures + 1 ELSE consecutive_failures END,
			    enabled = CASE WHEN NOT $4 AND $5 AND consecutive_failures + 1 >= $6 THEN FALSE ELSE enabled END,
			    auto_disabled = CASE WHEN NOT $4 AND $5 AND consecutive_failures + 1 >= $6 THEN TRUE ELSE auto_disabled END
			WHERE id = $1
			RETURNING auto_disabled AND NOT enabled AND NOT $4 AND $5`,
			id, d.StatusCode, d.Error, d.Success, final, disableAfter).Scan(&switchedOff)
	})
	if err != nil {
		return false, fmt.Errorf("record delivery: %w", err)
	}
	return switchedOff, nil
}
