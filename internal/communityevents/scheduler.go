package communityevents

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Scheduler runs periodic checks for event state transitions and reminders.
type Scheduler struct {
	Handler  *Handler
	Interval time.Duration
}

func NewScheduler(h *Handler) *Scheduler {
	return &Scheduler{
		Handler:  h,
		Interval: 10 * time.Second,
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	go s.run(ctx)
}

func (s *Scheduler) run(ctx context.Context) {
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.Tick(ctx, now)
		}
	}
}

// Tick executes a single scheduler cycle for activations, completions and reminders.
func (s *Scheduler) Tick(ctx context.Context, now time.Time) {
	s.processActivations(ctx, now)
	s.processCompletions(ctx, now)
	s.processReminders(ctx, now)
}

func (s *Scheduler) processActivations(ctx context.Context, now time.Time) {
	ids, err := s.Handler.Store.ActivateScheduled(ctx, now)
	if err != nil {
		slog.ErrorContext(ctx, "activate events failed", "err", err)
		return
	}
	for _, id := range ids {
		if ev, err := s.Handler.Store.Get(ctx, id, nil); err == nil {
			s.Handler.broadcast(ev)
		}
	}
}

func (s *Scheduler) processCompletions(ctx context.Context, now time.Time) {
	ids, err := s.Handler.Store.CompleteActive(ctx, now)
	if err != nil {
		slog.ErrorContext(ctx, "complete events failed", "err", err)
		return
	}
	for _, id := range ids {
		if ev, err := s.Handler.Store.Get(ctx, id, nil); err == nil {
			s.Handler.broadcast(ev)
		}
	}
}

func (s *Scheduler) processReminders(ctx context.Context, now time.Time) {
	reminders, err := s.Handler.Store.DueReminders(ctx, now)
	if err != nil {
		slog.ErrorContext(ctx, "check due event reminders failed", "err", err)
		return
	}
	for _, r := range reminders {
		s.dispatchReminder(ctx, r)
	}
}

func (s *Scheduler) dispatchReminder(ctx context.Context, r DueReminder) {
	if err := s.Handler.Store.MarkReminderSent(ctx, r.ID); err != nil {
		slog.ErrorContext(ctx, "mark event reminder sent failed", "event_id", r.ID, "err", err)
		return
	}
	userIDs, err := s.Handler.Store.GetRSVPUserIDs(ctx, r.ID)
	if err != nil {
		slog.ErrorContext(ctx, "get rsvp users for reminder failed", "event_id", r.ID, "err", err)
		return
	}

	snap, _ := s.Handler.Perms.Snapshot(ctx)
	payload := EventReminderPayload{
		EventID:   r.ID,
		Title:     r.Title,
		StartTime: r.StartTime,
		ChannelID: r.ChannelID,
	}

	for _, uid := range userIDs {
		if r.ChannelID != nil && snap != nil && !snap.Channel(perm.Subject{ID: uid}, *r.ChannelID).Has(perm.ViewChannel) {
			continue
		}
		if s.Handler.Push != nil {
			if err := s.Handler.Push.SendEventReminder(ctx, uid, r.ID, r.Title, r.StartTime, r.ChannelID); err != nil {
				slog.DebugContext(ctx, "send push event reminder failed", "user_id", uid, "err", err)
			}
		}
		if s.Handler.Events != nil {
			s.Handler.Events.SendToUsers([]uuid.UUID{uid}, events.EventReminder, payload)
		}
	}
}
