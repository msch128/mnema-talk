// Package later holds the two features that act at a time the member chose:
// reminders ("Remind me" on a message) and scheduled messages ("Send later").
// Both are private to their owner. A reminder stores a reference, not a copy of
// the message; a scheduled message is sent by the server as its author through
// the normal send path, so every check applies at send time.
package later

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/push"
)

// Limits.
const (
	// MaxReminders is how many reminders (waiting or delivered and not yet
	// dismissed) one member can have.
	MaxReminders = 100
	// MaxScheduled is how many scheduled messages (waiting or failed) one
	// member can have.
	MaxScheduled = 50
	// MinLead is how far ahead a time must lie.
	MinLead = 10 * time.Second
	// MaxLead is the furthest a time can lie ahead.
	MaxLead = 366 * 24 * time.Hour
	// DeliveredRetention is how long a delivered reminder waits for its owner
	// before it is removed.
	DeliveredRetention = 30 * 24 * time.Hour

	changesPerMinute = 60
	sweepEvery       = 15 * time.Second
	// pruneEvery spaces the clean-up of delivered reminders, which has no index
	// to lean on: once an hour is plenty for a 30 day retention.
	pruneEvery = time.Hour
	batch      = 100
)

// Notifier delivers a push to one member; the push service implements it. It
// is nil when Web Push is off.
type Notifier interface {
	Direct(ctx context.Context, d push.Direct)
}

// Service owns reminders and scheduled messages.
type Service struct {
	DB     *db.Pool
	Perms  *perm.Service
	Events events.Publisher
	// Online resolves @here in a scheduled message; nil means nobody is online.
	Online chat.OnlineSource
	// Push is nil when Web Push is off.
	Push Notifier
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	changes *httpx.RateLimiter
	pruned  *pruneClock
}

// pruneClock remembers when delivered reminders were last pruned.
type pruneClock struct {
	mu   sync.Mutex
	last time.Time
}

// due reports whether a prune is due at now and, when it is, books it.
func (c *pruneClock) due(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.last.IsZero() && now.Sub(c.last) < pruneEvery {
		return false
	}
	c.last = now
	return true
}

// NewService returns s with its state.
func NewService(s Service) *Service {
	s.pruned = &pruneClock{}
	s.changes = httpx.NewRateLimiter(changesPerMinute, time.Minute).Named("later_change")
	if s.Now == nil {
		s.Now = time.Now
	}
	return &s
}

// Run delivers due reminders and sends due scheduled messages until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep(ctx)
		}
	}
}

// Sweep does one round of work: it is what the ticker runs, and what tests call.
func (s *Service) Sweep(ctx context.Context) {
	if s.DB == nil {
		return
	}
	s.deliverReminders(ctx)
	s.sendScheduled(ctx)
	if s.pruned.due(s.Now()) {
		s.pruneDelivered(ctx)
	}
}

// checkTime requires a time a client sent to lie in the future, within a year.
// The client works it out from the member's own time zone; the server only
// stores the absolute time.
func (s *Service) checkTime(at time.Time) error {
	now := s.Now()
	if at.Before(now.Add(MinLead)) {
		return httpx.ErrInvalidInput("the time must be in the future")
	}
	if at.After(now.Add(MaxLead)) {
		return httpx.ErrInvalidInput("the time is too far ahead (one year at most)")
	}
	return nil
}

// notify tells the member's own sessions that something changed.
func (s *Service) notify(user uuid.UUID, event string, payload any) {
	if s.Events != nil {
		s.Events.SendToUsers([]uuid.UUID{user}, event, payload)
	}
}
