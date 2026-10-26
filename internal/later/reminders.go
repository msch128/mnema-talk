package later

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/push"
)

var errTooManyReminders = httpx.ErrConflict(fmt.Sprintf("you can have at most %d reminders; remove one first", MaxReminders))

// Reminder is one entry of the caller's reminder list.
type Reminder struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	RemindAt  time.Time `json:"remind_at" format:"date-time"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
	// DeliveredAt is set once the reminder fell due; it then waits in the list
	// until the caller dismisses it.
	DeliveredAt *time.Time `json:"delivered_at" format:"date-time" extensions:"x-nullable"`
	// Available is false when the caller may no longer see the message (it was
	// deleted, or they lost access); then Message is absent.
	Available bool        `json:"available"`
	Message   *MessageRef `json:"message,omitempty" binding:"optional"`
}

// ReminderEvent is the payload of reminder_update, sent to the owner's own
// sessions only. State is "created", "due" or "removed".
type ReminderEvent struct {
	ID    uuid.UUID `json:"id" format:"uuid"`
	State string    `json:"state" enums:"created,due,removed"`
}

type reminderRow struct {
	id                  uuid.UUID
	message             *uuid.UUID
	remindAt, createdAt time.Time
	deliveredAt         *time.Time
}

// AddReminder stores a reminder of messageID for sub, due at at. The message
// must be visible to the member now; otherwise it does not exist for them.
func (s *Service) AddReminder(ctx context.Context, sub perm.Subject, messageID uuid.UUID, at time.Time) (Reminder, error) {
	if err := s.checkTime(at); err != nil {
		return Reminder{}, err
	}
	ref, err := s.visibleRef(ctx, sub, messageID)
	if err != nil {
		return Reminder{}, err
	}
	if ref == nil {
		return Reminder{}, httpx.ErrNotFound("message not found")
	}
	out := Reminder{RemindAt: at.UTC(), Available: true, Message: ref}
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		// Serialize one member's changes so the cap holds.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, sub.ID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM message_reminders WHERE user_id = $1`, sub.ID).Scan(&n); err != nil {
			return err
		}
		if n >= MaxReminders {
			return errTooManyReminders
		}
		return tx.QueryRow(ctx, `INSERT INTO message_reminders (user_id, message_id, remind_at) VALUES ($1, $2, $3) RETURNING id, created_at`,
			sub.ID, messageID, at).Scan(&out.ID, &out.CreatedAt)
	})
	if err != nil {
		return Reminder{}, err
	}
	s.notify(sub.ID, events.ReminderUpdate, ReminderEvent{ID: out.ID, State: "created"})
	return out, nil
}

// ListReminders returns the member's reminders, the ones due first and then by
// time. The message of each is checked against their access now.
func (s *Service) ListReminders(ctx context.Context, sub perm.Subject) ([]Reminder, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, message_id, remind_at, created_at, delivered_at FROM message_reminders
		WHERE user_id = $1 ORDER BY (delivered_at IS NULL), remind_at, id LIMIT $2`, sub.ID, MaxReminders)
	if err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}
	stored, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (reminderRow, error) {
		var r reminderRow
		return r, row.Scan(&r.id, &r.message, &r.remindAt, &r.createdAt, &r.deliveredAt)
	})
	if err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}
	out := make([]Reminder, 0, len(stored))
	for _, r := range stored {
		e := Reminder{ID: r.id, RemindAt: r.remindAt, CreatedAt: r.createdAt, DeliveredAt: r.deliveredAt}
		if r.message != nil {
			if e.Message, err = s.visibleRef(ctx, sub, *r.message); err != nil {
				return nil, err
			}
			e.Available = e.Message != nil
		}
		out = append(out, e)
	}
	return out, nil
}

// RemoveReminder cancels a waiting reminder or dismisses a delivered one. It
// reports false when the member has no such reminder.
func (s *Service) RemoveReminder(ctx context.Context, user, id uuid.UUID) (bool, error) {
	tag, err := s.DB.Exec(ctx, `DELETE FROM message_reminders WHERE id = $1 AND user_id = $2`, id, user)
	if err != nil {
		return false, fmt.Errorf("remove reminder: %w", err)
	}
	if tag.RowsAffected() == 1 {
		s.notify(user, events.ReminderUpdate, ReminderEvent{ID: id, State: "removed"})
	}
	return tag.RowsAffected() == 1, nil
}

type dueReminder struct {
	id, user uuid.UUID
	message  *uuid.UUID
}

// claimDue marks the reminders that fell due as delivered and returns them. A
// reminder is claimed once: if the server stops before telling the member, the
// reminder still waits in their list, only the push is lost.
func (s *Service) claimDue(ctx context.Context) ([]dueReminder, error) {
	rows, err := s.DB.Query(ctx, `
		UPDATE message_reminders SET delivered_at = clock_timestamp() WHERE id IN (
			SELECT id FROM message_reminders WHERE delivered_at IS NULL AND remind_at <= clock_timestamp()
			ORDER BY remind_at LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING id, user_id, message_id`, batch)
	if err != nil {
		return nil, fmt.Errorf("claim reminders: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (dueReminder, error) {
		var d dueReminder
		return d, row.Scan(&d.id, &d.user, &d.message)
	})
}

// deliverReminders tells the owners of the reminders that fell due: an event
// for their open sessions and a push. The message is checked against the
// owner's access at this moment; a message they cannot see is not described.
func (s *Service) deliverReminders(ctx context.Context) {
	due, err := s.claimDue(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("reminders: claim", "err", err)
		}
		return
	}
	for _, d := range due {
		s.deliverOne(ctx, d)
	}
}

func (s *Service) deliverOne(ctx context.Context, d dueReminder) {
	s.notify(d.user, events.ReminderUpdate, ReminderEvent{ID: d.id, State: "due"})
	if s.Push == nil {
		return
	}
	n := push.Direct{User: d.user, Kind: push.DirectReminder, ID: d.id, Gone: true}
	sub, ok, err := s.subjectOf(ctx, d.user)
	if err != nil {
		slog.Warn("reminders: load account", "err", err)
		return // fail closed: nothing is said about a message we cannot check
	}
	if !ok {
		return
	}
	if d.message != nil {
		ref, err := s.visibleRef(ctx, sub, *d.message)
		if err != nil {
			slog.Warn("reminders: check access", "err", err)
			return
		}
		if ref != nil {
			n.Gone, n.ChannelID, n.MessageID, n.ParentID = false, &ref.ChannelID, &ref.ID, ref.ParentID
			n.Sender, n.Content = ref.Author, ref.Snippet
		}
	}
	s.Push.Direct(ctx, n)
}

// pruneDelivered removes delivered reminders nobody dismissed for a month.
func (s *Service) pruneDelivered(ctx context.Context) {
	_, err := s.DB.Exec(ctx, `DELETE FROM message_reminders WHERE delivered_at < clock_timestamp() - make_interval(secs => $1)`, DeliveredRetention.Seconds())
	if err != nil && ctx.Err() == nil {
		slog.Warn("reminders: prune", "err", err)
	}
}
