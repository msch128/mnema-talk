package communityevents

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Scheduler runs periodic checks for event state transitions and reminders.
type Scheduler struct {
	Handler  *Handler
	Interval time.Duration
	pushes   sync.WaitGroup
	// pusher overrides Handler.Push (tests).
	pusher reminderPusher
}

// reminderPusher sends the push reminder of an event to one member.
type reminderPusher interface {
	SendEventReminder(ctx context.Context, userID, eventID uuid.UUID, title string, startTime time.Time, channelID *uuid.UUID) error
}

func (s *Scheduler) reminderPusher() reminderPusher {
	if s.pusher != nil {
		return s.pusher
	}
	if s.Handler.Push != nil {
		return s.Handler.Push
	}
	return nil
}

const (
	reminderPushWorkers = 8
	reminderPushTimeout = 2 * time.Minute
)

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
	s.processPromotions(ctx)
}

func (s *Scheduler) processActivations(ctx context.Context, now time.Time) {
	ids, err := s.Handler.Store.ActivateScheduled(ctx, now)
	if err != nil {
		slog.ErrorContext(ctx, "activate events failed", "err", err)
		return
	}
	s.broadcastEvents(ctx, ids)
}

func (s *Scheduler) processCompletions(ctx context.Context, now time.Time) {
	ids, err := s.Handler.Store.CompleteActive(ctx, now)
	if err != nil {
		slog.ErrorContext(ctx, "complete events failed", "err", err)
		return
	}
	s.broadcastEvents(ctx, ids)
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
	snap, err := s.reminderSnapshot(ctx)
	if err != nil {
		// Without the permission state nobody may be told about a Talk event;
		// the reminder stays due and the next tick tries again.
		slog.ErrorContext(ctx, "load permissions for event reminder failed", "event_id", r.ID, "err", err)
		return
	}
	userIDs, err := s.Handler.Store.GetRSVPUserIDs(ctx, r.ID)
	if err != nil {
		slog.ErrorContext(ctx, "get rsvp users for reminder failed", "event_id", r.ID, "err", err)
		return
	}
	// Claim after the recipients are known, so a failure above leaves the
	// reminder due, and only one pass or server process sends it.
	claimed, err := s.Handler.Store.ClaimReminder(ctx, r.ID)
	if err != nil {
		slog.ErrorContext(ctx, "mark event reminder sent failed", "event_id", r.ID, "err", err)
		return
	}
	if !claimed {
		return
	}
	recipients := userIDs[:0]
	for _, uid := range userIDs {
		if r.ChannelID != nil && snap != nil && !snap.Channel(perm.Subject{ID: uid}, *r.ChannelID).Has(perm.ViewChannel) {
			continue
		}
		recipients = append(recipients, uid)
	}
	s.remind(ctx, recipients, r)
}

func (s *Scheduler) reminderSnapshot(ctx context.Context) (*perm.Snapshot, error) {
	if s.Handler.Perms == nil {
		return nil, nil
	}
	return s.Handler.Perms.Snapshot(ctx)
}

// remind tells the recipients over WebSocket right away; the pushes, which
// can be slow, go out in the background so the tick is not held up.
func (s *Scheduler) remind(ctx context.Context, recipients []uuid.UUID, r DueReminder) {
	if s.Handler.Events != nil {
		for _, uid := range recipients {
			s.Handler.Events.SendToUsers([]uuid.UUID{uid}, events.EventReminder, EventReminderPayload{
				EventID: r.ID, Title: r.Title, StartTime: r.StartTime, ChannelID: r.ChannelID,
			})
		}
	}
	pusher := s.reminderPusher()
	if pusher == nil || len(recipients) == 0 {
		return
	}
	s.pushes.Add(1)
	go func() {
		defer s.pushes.Done()
		s.pushAll(ctx, pusher, recipients, r)
	}()
}

// pushAll sends the push reminders with a few workers, giving up after
// reminderPushTimeout.
func (s *Scheduler) pushAll(ctx context.Context, pusher reminderPusher, recipients []uuid.UUID, r DueReminder) {
	ctx, cancel := context.WithTimeout(ctx, reminderPushTimeout)
	defer cancel()
	todo := make(chan uuid.UUID)
	var wg sync.WaitGroup
	for range min(reminderPushWorkers, len(recipients)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for uid := range todo {
				if err := pusher.SendEventReminder(ctx, uid, r.ID, r.Title, r.StartTime, r.ChannelID); err != nil {
					slog.DebugContext(ctx, "send push event reminder failed", "user_id", uid, "err", err)
				}
			}
		}()
	}
	for _, uid := range recipients {
		if ctx.Err() != nil {
			break
		}
		todo <- uid
	}
	close(todo)
	wg.Wait()
}

// Wait blocks until the push reminders already started are done.
func (s *Scheduler) Wait() { s.pushes.Wait(); s.Handler.WaitNotices() }

// broadcastEvents announces the current state of each event; an event that
// vanished meanwhile is skipped.
func (s *Scheduler) broadcastEvents(ctx context.Context, ids []uuid.UUID) {
	for _, id := range ids {
		if ev, err := s.Handler.Store.Get(ctx, id, nil); err == nil {
			s.Handler.broadcast(ev)
		}
	}
}

// processPromotions moves waitlisted members up wherever a role has room that
// no sign-up route filled (a member was erased, say) and tells them.
func (s *Scheduler) processPromotions(ctx context.Context) {
	promoted, err := s.Handler.Store.PromoteOpenSlots(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "promote waitlists failed", "err", err)
	}
	byEvent := map[uuid.UUID][]Promotion{}
	var order []uuid.UUID
	for _, p := range promoted {
		if _, ok := byEvent[p.EventID]; !ok {
			order = append(order, p.EventID)
		}
		byEvent[p.EventID] = append(byEvent[p.EventID], p)
	}
	for _, id := range order {
		ev, err := s.Handler.Store.Get(ctx, id, nil)
		if err != nil {
			continue
		}
		s.Handler.broadcast(ev)
		s.Handler.notifyMembers(ctx, ev, SignupKindPromoted, "", byEvent[id])
	}
}
