package webhook

import (
	"testing"

	"github.com/google/uuid"
)

// One channel's flood fills only its share of the queue; the others keep
// their places, and a worker taking a job frees its channel's share.
func TestOneChannelCannotFillTheQueue(t *testing.T) {
	d := NewDispatcher(nil, nil, "test")
	noisy, quiet := uuid.New(), uuid.New()
	for range queueSize {
		ev := Event{ID: uuid.New(), Type: EventMessageCreate}
		ev.Channel = noisy
		d.enqueue(job{webhook: uuid.New(), event: ev, attempt: 1})
	}
	if got := len(d.queue); got != perChannelQueue {
		t.Fatalf("queued %d of the noisy channel, want %d", got, perChannelQueue)
	}
	if got := d.dropped.Load(); got != int64(queueSize-perChannelQueue) {
		t.Fatalf("dropped %d", got)
	}
	ev := Event{ID: uuid.New(), Type: EventMessageCreate}
	ev.Channel = quiet
	d.enqueue(job{webhook: uuid.New(), event: ev, attempt: 1})
	if got := len(d.queue); got != perChannelQueue+1 {
		t.Fatalf("quiet channel's event not queued: %d", got)
	}
	// Taking a job releases its channel's place.
	j := <-d.queue
	d.dequeued(j)
	d.enqueue(job{webhook: uuid.New(), event: j.event, attempt: 1})
	if got := len(d.queue); got != perChannelQueue+1 {
		t.Fatalf("queue length %d after a take and a new event", got)
	}
}
