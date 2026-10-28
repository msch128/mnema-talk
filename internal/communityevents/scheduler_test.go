package communityevents

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// slowPusher blocks every send until release is closed and records how many
// ran at once.
type slowPusher struct {
	release chan struct{}
	active  atomic.Int32
	peak    atomic.Int32
	mu      sync.Mutex
	sent    map[uuid.UUID]bool
}

func (p *slowPusher) SendEventReminder(ctx context.Context, userID, _ uuid.UUID, _ string, _ time.Time, _ *uuid.UUID) error {
	n := p.active.Add(1)
	defer p.active.Add(-1)
	for {
		peak := p.peak.Load()
		if n <= peak || p.peak.CompareAndSwap(peak, n) {
			break
		}
	}
	select {
	case <-p.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	p.mu.Lock()
	p.sent[userID] = true
	p.mu.Unlock()
	return nil
}

func TestRemindersDoNotHoldUpTheTickAndRunWithBoundedWorkers(t *testing.T) {
	pusher := &slowPusher{release: make(chan struct{}), sent: map[uuid.UUID]bool{}}
	s := &Scheduler{Handler: &Handler{}, pusher: pusher}
	recipients := make([]uuid.UUID, 50)
	for i := range recipients {
		recipients[i] = uuid.New()
	}

	returned := make(chan struct{})
	go func() {
		s.remind(context.Background(), recipients, DueReminder{ID: uuid.New(), Title: "x", StartTime: time.Now()})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("remind must return while the pushes are still on their way")
	}

	deadline := time.Now().Add(2 * time.Second)
	for pusher.active.Load() < reminderPushWorkers && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	close(pusher.release)
	s.Wait()

	if peak := pusher.peak.Load(); peak > reminderPushWorkers {
		t.Fatalf("%d sends ran at once, at most %d are allowed", peak, reminderPushWorkers)
	}
	if len(pusher.sent) != len(recipients) {
		t.Fatalf("%d of %d members were pushed", len(pusher.sent), len(recipients))
	}
}

func TestReminderPushesStopWhenTheContextEnds(t *testing.T) {
	pusher := &slowPusher{release: make(chan struct{}), sent: map[uuid.UUID]bool{}}
	s := &Scheduler{Handler: &Handler{}, pusher: pusher}
	ctx, cancel := context.WithCancel(context.Background())
	recipients := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}

	s.remind(ctx, recipients, DueReminder{ID: uuid.New()})
	cancel()
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled context must end the pending pushes")
	}
}
