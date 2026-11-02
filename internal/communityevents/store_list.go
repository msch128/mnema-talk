package communityevents

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Defaults and bounds of GET /api/events.
const (
	DefaultListLimit = 50
	MaxListLimit     = 200
	listLookBehind   = 24 * time.Hour
	listLookAhead    = 90 * 24 * time.Hour
)

// defaultEventDuration is how long an event without an end lasts: the list's
// past window and the scheduler's completion both use it (eventEnd).
const (
	defaultEventDuration = `INTERVAL '2 hours'`
	eventEnd             = `COALESCE(end_time, start_time + ` + defaultEventDuration + `)`
)

// ListQuery selects the events a member sees: those that start before Before
// and have not ended before After (an event without an end counts as ended
// when it starts). Allow decides which Talk channels the caller may view.
//
// With Past set the window is instead every event that is over (completed,
// cancelled, or ended before Now), newest first. Before and BeforeID then act
// as a keyset cursor: only events sorting strictly after (Before, BeforeID)
// in that order are returned, and After is ignored.
type ListQuery struct {
	After    time.Time
	Before   time.Time
	BeforeID uuid.UUID
	Limit    int
	Past     bool
	Allow    func(channelID uuid.UUID) bool
}

const (
	eventWindow = `COALESCE(end_time, start_time) >= $1 AND start_time < $2`
	// pastWindow takes the cursor as $1 (time) and $2 (id), now as $3.
	pastWindow = `(status IN ('completed', 'cancelled') OR ` + eventEnd + ` < $3)
		AND (start_time, id) < ($1, $2)`
)

// List returns the events in the window ordered by start, at most q.Limit.
// Events in Talks the caller may not view are filtered in SQL, so they never
// use up the limit. RSVP data for all returned events is loaded in three
// queries, whatever their number.
func (s *Store) List(ctx context.Context, callerID *uuid.UUID, q ListQuery) ([]*CommunityEvent, error) {
	allowed, err := s.allowedChannels(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, err := s.queryWindow(ctx, q, allowed)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	list := []*CommunityEvent{}
	for rows.Next() {
		row, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, row.toEvent())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, s.fillRSVPsBatch(ctx, list, callerID)
}

// queryWindow selects the rows of the window q describes, restricted to events
// without a Talk or in one of the allowed Talks.
func (s *Store) queryWindow(ctx context.Context, q ListQuery, allowed []uuid.UUID) (pgx.Rows, error) {
	if q.Past {
		return s.DB.Query(ctx, `SELECT `+eventColumns+` FROM events
			WHERE `+pastWindow+` AND (channel_id IS NULL OR channel_id = ANY($4))
			ORDER BY start_time DESC, id DESC
			LIMIT $5`, q.Before, q.BeforeID, time.Now(), allowed, q.Limit)
	}
	return s.DB.Query(ctx, `SELECT `+eventColumns+` FROM events
		WHERE `+eventWindow+` AND (channel_id IS NULL OR channel_id = ANY($3))
		ORDER BY start_time ASC, id ASC
		LIMIT $4`, q.After, q.Before, allowed, q.Limit)
}

// allowedChannels returns the Talks used by events in the window that the
// caller may view.
func (s *Store) allowedChannels(ctx context.Context, q ListQuery) ([]uuid.UUID, error) {
	var rows pgx.Rows
	var err error
	if q.Past {
		rows, err = s.DB.Query(ctx, `SELECT DISTINCT channel_id FROM events WHERE channel_id IS NOT NULL AND `+pastWindow, q.Before, q.BeforeID, time.Now())
	} else {
		rows, err = s.DB.Query(ctx, `SELECT DISTINCT channel_id FROM events WHERE channel_id IS NOT NULL AND `+eventWindow, q.After, q.Before)
	}
	if err != nil {
		return nil, fmt.Errorf("list event channels: %w", err)
	}
	defer rows.Close()
	allowed := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if q.Allow == nil || q.Allow(id) {
			allowed = append(allowed, id)
		}
	}
	return allowed, rows.Err()
}

func (s *Store) fillRSVPsBatch(ctx context.Context, list []*CommunityEvent, callerID *uuid.UUID) error {
	if len(list) == 0 {
		return nil
	}
	byID := make(map[uuid.UUID]*CommunityEvent, len(list))
	ids := make([]uuid.UUID, len(list))
	for i, ev := range list {
		byID[ev.ID] = ev
		ids[i] = ev.ID
	}
	if err := s.batchCounts(ctx, byID, ids); err != nil {
		return err
	}
	if err := s.batchCallerRSVP(ctx, byID, ids, callerID); err != nil {
		return err
	}
	if err := s.fillRoles(ctx, byID, ids, callerID); err != nil {
		return err
	}
	return s.batchGoing(ctx, byID, ids)
}

func (s *Store) batchCounts(ctx context.Context, byID map[uuid.UUID]*CommunityEvent, ids []uuid.UUID) error {
	rows, err := s.DB.Query(ctx, `
		SELECT event_id, response, COUNT(*) FROM event_rsvps WHERE event_id = ANY($1) GROUP BY event_id, response`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var resp string
		var n int
		if err := rows.Scan(&id, &resp, &n); err != nil {
			return err
		}
		c := &byID[id].RSVPCounts
		switch resp {
		case RSVPGoing:
			c.Going = n
		case RSVPInterested:
			c.Interested = n
		case RSVPNotGoing:
			c.NotGoing = n
		}
	}
	return rows.Err()
}

func (s *Store) batchCallerRSVP(ctx context.Context, byID map[uuid.UUID]*CommunityEvent, ids []uuid.UUID, callerID *uuid.UUID) error {
	if callerID == nil {
		return nil
	}
	rows, err := s.DB.Query(ctx, `SELECT event_id, response FROM event_rsvps WHERE event_id = ANY($1) AND user_id = $2`, ids, *callerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var resp string
		if err := rows.Scan(&id, &resp); err != nil {
			return err
		}
		byID[id].MyRSVP = &resp
	}
	return rows.Err()
}

// batchGoing loads the first 20 members going to each event.
func (s *Store) batchGoing(ctx context.Context, byID map[uuid.UUID]*CommunityEvent, ids []uuid.UUID) error {
	rows, err := s.DB.Query(ctx, `
		SELECT event_id, id, username, display_name, avatar_s3_key FROM (
			SELECT r.event_id, u.id, u.username, u.display_name, u.avatar_s3_key,
			       ROW_NUMBER() OVER (PARTITION BY r.event_id ORDER BY r.updated_at ASC) AS rn
			FROM event_rsvps r JOIN users u ON u.id = r.user_id
			WHERE r.event_id = ANY($1) AND r.response = 'going') g
		WHERE rn <= 20
		ORDER BY event_id, rn`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var eventID uuid.UUID
		var m UserRef
		var avatarKey *string
		if err := rows.Scan(&eventID, &m.ID, &m.Username, &m.DisplayName, &avatarKey); err != nil {
			return err
		}
		byID[eventID].GoingMembers = append(byID[eventID].GoingMembers, newUserRef(m.ID, m.Username, m.DisplayName, avatarKey))
	}
	return rows.Err()
}
