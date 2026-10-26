package communityevents

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type DueReminder struct {
	ID        uuid.UUID  `db:"id"`
	Title     string     `db:"title"`
	StartTime time.Time  `db:"start_time"`
	ChannelID *uuid.UUID `db:"channel_id"`
}

func (s *Store) ActivateScheduled(ctx context.Context, now time.Time) ([]uuid.UUID, error) {
	rows, err := s.DB.Query(ctx, `
		UPDATE events SET status = 'active', updated_at = NOW()
		WHERE status = 'scheduled' AND start_time <= $1
		RETURNING id`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) CompleteActive(ctx context.Context, now time.Time) ([]uuid.UUID, error) {
	rows, err := s.DB.Query(ctx, `
		UPDATE events SET status = 'completed', updated_at = NOW()
		WHERE status = 'active' AND end_time IS NOT NULL AND end_time <= $1
		RETURNING id`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) DueReminders(ctx context.Context, now time.Time) ([]DueReminder, error) {
	dueCutoff := now.Add(15 * time.Minute)
	pastCutoff := now.Add(-10 * time.Minute)
	rows, err := s.DB.Query(ctx, `
		SELECT id, title, start_time, channel_id
		FROM events
		WHERE status IN ('scheduled', 'active')
		  AND reminder_sent_at IS NULL
		  AND start_time <= $1
		  AND start_time > $2`, dueCutoff, pastCutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DueReminder
	for rows.Next() {
		var r DueReminder
		if err := rows.Scan(&r.ID, &r.Title, &r.StartTime, &r.ChannelID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) MarkReminderSent(ctx context.Context, id uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `UPDATE events SET reminder_sent_at = NOW() WHERE id = $1`, id)
	return err
}

func (s *Store) GetRSVPUserIDs(ctx context.Context, eventID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT user_id FROM event_rsvps WHERE event_id = $1 AND response IN ('going', 'interested')`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
