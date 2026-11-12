package readstate

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A queue without its worker, to look at what Tell keeps.
func idleRecounter(ctx context.Context) *Recounter {
	return &Recounter{ctx: ctx, jobs: map[uuid.UUID]*recountJob{}, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

func TestTellMergesTheRequestsOfAWaitingChannel(t *testing.T) {
	r := idleRecounter(context.Background())
	ch := uuid.New()
	early, late := time.Now().Add(-time.Hour), time.Now()
	a, b := uuid.New(), uuid.New()
	r.Tell(ch, &early, []uuid.UUID{a})
	r.Tell(ch, &late, []uuid.UUID{b})
	job := r.jobs[ch]
	if len(r.order) != 1 || job == nil || !job.since.Equal(late) || !job.users[a] || !job.users[b] {
		t.Fatalf("not merged: order %v job %+v", r.order, job)
	}
	// Every member and now are wider than any list and any time.
	r.Tell(ch, nil, nil)
	if job.since != nil || job.users != nil {
		t.Fatalf("a request for everyone now did not widen the job: %+v", job)
	}
	r.Tell(ch, &early, []uuid.UUID{a})
	if job.since != nil || job.users != nil {
		t.Fatalf("a narrower request narrowed the job: %+v", job)
	}
}

func TestTellNeverGrowsTheQueuePastItsBoundAndRefusesAfterShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := idleRecounter(ctx)
	for range recountChannels + 10 {
		r.Tell(uuid.New(), nil, nil)
	}
	if len(r.jobs) != recountChannels || len(r.order) != recountChannels {
		t.Fatalf("%d jobs, %d in order; bound %d", len(r.jobs), len(r.order), recountChannels)
	}
	// A channel already waiting is still merged when the queue is full.
	waiting := r.order[0]
	r.Tell(waiting, nil, []uuid.UUID{uuid.New()})
	if len(r.jobs) != recountChannels {
		t.Fatalf("a merge grew the queue to %d", len(r.jobs))
	}
	cancel()
	r2 := idleRecounter(ctx)
	r2.Tell(uuid.New(), nil, nil)
	if len(r2.jobs) != 0 {
		t.Fatal("a recount was queued after shutdown began")
	}
	var none *Recounter
	none.Tell(uuid.New(), nil, nil) // a nil Recounter tells nobody
	if !none.Wait(time.Millisecond) {
		t.Fatal("a nil Recounter has nothing to wait for")
	}
}
