package communityevents

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// queryIDs runs a query that returns one uuid column and collects it.
func (s *Store) queryIDs(ctx context.Context, sql string, args ...any) ([]uuid.UUID, error) {
	rows, err := s.DB.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// DueReminder is an event whose reminder is due and has not been sent yet.
type DueReminder struct {
	ID        uuid.UUID  `db:"id"`
	Title     string     `db:"title"`
	StartTime time.Time  `db:"start_time"`
	ChannelID *uuid.UUID `db:"channel_id"`
}

// ActivateScheduled starts every scheduled event whose start time has come and
// returns their ids.
func (s *Store) ActivateScheduled(ctx context.Context, now time.Time) ([]uuid.UUID, error) {
	return s.queryIDs(ctx, `
		UPDATE events SET status = 'active', updated_at = NOW()
		WHERE status = 'scheduled' AND start_time <= $1
		RETURNING id`, now)
}

// CompleteActive completes every active event whose end has passed and returns
// their ids.
func (s *Store) CompleteActive(ctx context.Context, now time.Time) ([]uuid.UUID, error) {
	return s.queryIDs(ctx, `
		UPDATE events SET status = 'completed', updated_at = NOW()
		WHERE status = 'active' AND `+eventEnd+` <= $1
		RETURNING id`, now)
}

// DueReminders lists events that start within 15 minutes and whose reminder is
// unsent. Events that started more than 10 minutes ago are skipped, so a server
// that was down does not remind about something long under way.
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

// ClaimReminder marks the reminder of a due event as sent and reports whether
// this call did it, so of several scheduler passes or server processes only
// one sends it.
func (s *Store) ClaimReminder(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.DB.Exec(ctx, `
		UPDATE events SET reminder_sent_at = NOW()
		WHERE id = $1 AND reminder_sent_at IS NULL AND status IN ('scheduled', 'active')`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// GetRSVPUserIDs returns the members who are going to or interested in an
// event, the ones its reminder is for.
func (s *Store) GetRSVPUserIDs(ctx context.Context, eventID uuid.UUID) ([]uuid.UUID, error) {
	return s.queryIDs(ctx, `
		SELECT user_id FROM event_rsvps WHERE event_id = $1 AND response IN ('going', 'interested')`, eventID)
}
