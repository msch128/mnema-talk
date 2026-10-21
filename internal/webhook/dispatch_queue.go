package webhook

import (
	"context"
	"log/slog"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

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

// waitWork pauses until more webhook jobs might be available or the context is cancelled.
func (d *Dispatcher) waitWork(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-d.wake:
		return true
	}
}

// paceWork respects the global rate-limit tick and releases the claimed job on shutdown.
func (d *Dispatcher) paceWork(ctx context.Context, j job, tick <-chan time.Time) bool {
	select {
	case <-ctx.Done():
		// Shutting down: hand the row back instead of holding it for the lease.
		if err := releaseJob(context.WithoutCancel(ctx), d.DB, j.seq); err != nil {
			slog.Warn("outgoing webhooks: release", "err", err)
		}
		return false
	case <-tick: // one attempt per tick, shared by the workers
		return true
	}
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
			if !d.waitWork(ctx) {
				return
			}
			continue
		}
		d.signal() // there may be more: let an idle worker look too
		if !d.paceWork(ctx, j, tick) {
			return
		}
		d.process(ctx, j)
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
