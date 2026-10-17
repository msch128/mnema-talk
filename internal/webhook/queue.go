package webhook

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// The pending deliveries live in the outgoing_queue table (migration 0023);
// this file is everything that touches it.
const (
	// maxPendingPerWebhook and maxPendingTotal bound the table: past them the
	// oldest rows go (counted in mnema_webhook_events_dropped_total).
	maxPendingPerWebhook = 500
	maxPendingTotal      = 5000
	// claimLease is how long a claimed row is invisible to other claimers. A
	// claimer that dies leaves its rows due again once the lease ends.
	claimLease = 30 * time.Second
	// pendingTTL is when a row that was never settled is given up: past the
	// last backoff step (about 45 minutes, up to about 70 with Retry-After).
	pendingTTL = 2 * time.Hour
	// pollEvery is how often an idle dispatcher looks for due rows (retries
	// and rows written by another process); new events wake it at once.
	pollEvery  = time.Second
	pruneEvery = time.Minute
	// storeTimeout bounds the final write of the buffer at shutdown.
	storeTimeout = 5 * time.Second
	// storeBatch is how many buffered events one insert takes.
	storeBatch = 100
)

// shutdownGrace is how long attempts in flight may finish after shutdown began;
// later ones are abandoned and their rows left for the next start. A variable
// for tests.
var shutdownGrace = 5 * time.Second

// step is part of the transaction that records an attempt.
type step func(context.Context, pgx.Tx) error

// storeEvents persists new events (first attempt, due now) and then applies
// the caps. It returns how many rows the per-webhook and global caps removed.
func storeEvents(ctx context.Context, p *db.Pool, jobs []job) (perWebhook, global int64, err error) {
	n := len(jobs)
	webhooks, channels := make([]uuid.UUID, n), make([]uuid.UUID, n)
	ids, times := make([]uuid.UUID, n), make([]time.Time, n)
	types, data := make([]string, n), make([][]byte, n)
	for i, j := range jobs {
		webhooks[i], channels[i], ids[i], times[i] = j.webhook, j.event.Channel, j.event.ID, j.event.Time
		types[i], data[i] = j.event.Type, j.event.Data
	}
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO outgoing_queue (webhook_id, channel_id, event_id, event_type, event_time, data)
			SELECT * FROM unnest($1::uuid[], $2::uuid[], $3::uuid[], $4::text[], $5::timestamptz[], $6::bytea[])`,
			webhooks, channels, ids, types, times, data); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			DELETE FROM outgoing_queue WHERE seq IN (
			    SELECT seq FROM (
			        SELECT seq, row_number() OVER (PARTITION BY webhook_id ORDER BY seq DESC) AS rn
			        FROM outgoing_queue WHERE webhook_id = ANY($1)) s
			    WHERE rn > $2)`, webhooks, maxPendingPerWebhook)
		if err != nil {
			return err
		}
		perWebhook = tag.RowsAffected()
		tag, err = tx.Exec(ctx, `DELETE FROM outgoing_queue WHERE seq IN (SELECT seq FROM outgoing_queue ORDER BY seq DESC OFFSET $1)`, maxPendingTotal)
		if err != nil {
			return err
		}
		global = tag.RowsAffected()
		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("store outgoing events: %w", err)
	}
	return perWebhook, global, nil
}

// claimJob takes the oldest due row under a lease; ok is false when none is
// due. FOR UPDATE SKIP LOCKED lets several processes claim at once without
// ever getting the same row.
func claimJob(ctx context.Context, p *db.Pool) (j job, ok bool, err error) {
	err = p.QueryRow(ctx, `
		UPDATE outgoing_queue SET claimed_until = NOW() + make_interval(secs => $1)
		WHERE seq = (
		    SELECT seq FROM outgoing_queue
		    WHERE next_attempt_at <= NOW() AND (claimed_until IS NULL OR claimed_until < NOW())
		    ORDER BY next_attempt_at, seq LIMIT 1
		    FOR UPDATE SKIP LOCKED)
		RETURNING seq, webhook_id, channel_id, event_id, event_type, event_time, data, attempt`, claimLease.Seconds()).
		Scan(&j.seq, &j.webhook, &j.event.Channel, &j.event.ID, &j.event.Type, &j.event.Time, &j.event.Data, &j.attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return job{}, false, nil
	}
	if err != nil {
		return job{}, false, fmt.Errorf("claim outgoing event: %w", err)
	}
	return j, true, nil
}

// settleStep removes a settled row (delivered, or failed for good).
func settleStep(seq int64) step {
	return func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM outgoing_queue WHERE seq = $1`, seq)
		return err
	}
}

// rescheduleStep keeps a row for the next attempt after wait.
func rescheduleStep(seq int64, attempt int, wait time.Duration) step {
	return func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE outgoing_queue SET attempt = $2, claimed_until = NULL,
			    next_attempt_at = NOW() + make_interval(secs => $3) WHERE seq = $1`, seq, attempt, wait.Seconds())
		return err
	}
}

// releaseJob gives a claimed row back so it is due again at once.
func releaseJob(ctx context.Context, p *db.Pool, seq int64) error {
	_, err := p.Exec(ctx, `UPDATE outgoing_queue SET claimed_until = NULL WHERE seq = $1`, seq)
	return err
}

// dropRow removes a row without a delivery (its webhook is gone or off).
func dropRow(ctx context.Context, p *db.Pool, seq int64) error {
	_, err := p.Exec(ctx, `DELETE FROM outgoing_queue WHERE seq = $1`, seq)
	return err
}

// purgeWebhook removes everything pending for a webhook that was switched off.
func purgeWebhook(ctx context.Context, p *db.Pool, id uuid.UUID) error {
	_, err := p.Exec(ctx, `DELETE FROM outgoing_queue WHERE webhook_id = $1`, id)
	return err
}

// pruneQueue removes rows older than pendingTTL and returns how many.
func pruneQueue(ctx context.Context, p *db.Pool) (int64, error) {
	tag, err := p.Exec(ctx, `DELETE FROM outgoing_queue WHERE created_at < NOW() - make_interval(secs => $1)`, pendingTTL.Seconds())
	if err != nil {
		return 0, fmt.Errorf("prune outgoing queue: %w", err)
	}
	return tag.RowsAffected(), nil
}
