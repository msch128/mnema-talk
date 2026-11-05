package communityevents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/communityevents/recurrence"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	// MaxSeriesOccurrences is the most occurrences a series ever creates.
	MaxSeriesOccurrences = 200
	// seriesHorizon is how far ahead occurrences are created.
	seriesHorizon = 60 * 24 * time.Hour
	// seriesTick is how often the scheduler looks at the series.
	seriesTick = 10 * time.Minute
)

// seriesRow is a series as the transactions see it.
type seriesRow struct {
	ID          uuid.UUID
	Frequency   string
	Zone        *time.Location
	ZoneName    string
	Anchor      time.Time
	AnchorIndex int
	NextIndex   int
	EndIndex    *int
	Until       *time.Time
	Duration    *int
	Title       string
	Description string
	ChannelID   *uuid.UUID
	Location    *string
	Roles       []RoleInput
	Status      string
}

const seriesColumns = `id, frequency, timezone, anchor_start, anchor_index, next_index, end_index, until_at, duration_seconds, title, description, channel_id, location, roles, status`

func scanSeries(row pgx.Row) (seriesRow, error) {
	var r seriesRow
	var roles []byte
	err := row.Scan(&r.ID, &r.Frequency, &r.ZoneName, &r.Anchor, &r.AnchorIndex, &r.NextIndex, &r.EndIndex, &r.Until, &r.Duration, &r.Title, &r.Description, &r.ChannelID, &r.Location, &roles, &r.Status)
	if err != nil {
		return r, err
	}
	if r.Zone, err = time.LoadLocation(r.ZoneName); err != nil {
		r.Zone = time.UTC
	}
	if err := json.Unmarshal(roles, &r.Roles); err != nil {
		return r, fmt.Errorf("decode series roles: %w", err)
	}
	return r, nil
}

// start is the start of occurrence index i.
func (r seriesRow) start(i int) time.Time {
	return recurrence.OccurrenceStart(r.Anchor, r.Zone, r.Frequency, i-r.AnchorIndex)
}

func lockSeries(ctx context.Context, tx pgx.Tx, id uuid.UUID) (seriesRow, error) {
	r, err := scanSeries(tx.QueryRow(ctx, `SELECT `+seriesColumns+` FROM event_series WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, httpx.ErrNotFound("series not found")
	}
	return r, err
}

// CreateSeries creates a series whose first occurrence is the event req describes
// and returns that occurrence. The following ones are created by GenerateSeries.
func (s *Store) CreateSeries(ctx context.Context, req CreateEventRequest, rec RecurrenceRequest, creatorID uuid.UUID) (*CommunityEvent, uuid.UUID, error) {
	loc, err := recurrence.LoadZone(rec.Timezone)
	if err != nil {
		return nil, uuid.Nil, err
	}
	var duration *int
	if req.EndTime != nil {
		d := int(req.EndTime.Sub(req.StartTime).Seconds())
		duration = &d
	}
	zone := loc.String()
	var seriesID uuid.UUID
	var eventID uuid.UUID
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO event_series (frequency, timezone, anchor_start, until_at, duration_seconds, title, description, channel_id, location, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			rec.Frequency, zone, req.StartTime, rec.Until, duration, req.Title, req.Description, req.ChannelID, req.Location, creatorID).Scan(&seriesID); err != nil {
			return fmt.Errorf("create series: %w", err)
		}
		series, err := lockSeries(ctx, tx, seriesID)
		if err != nil {
			return err
		}
		eventID, err = insertOccurrence(ctx, tx, series, 0, time.Now())
		return err
	})
	if err != nil {
		return nil, uuid.Nil, err
	}
	ev, err := s.Get(ctx, eventID, nil)
	return ev, seriesID, err
}

// insertOccurrence stores occurrence i of the series as an event with the
// series' template and its roles (no sign-ups).
func insertOccurrence(ctx context.Context, tx pgx.Tx, r seriesRow, i int, now time.Time) (uuid.UUID, error) {
	start := r.start(i)
	var end *time.Time
	if r.Duration != nil {
		e := start.Add(time.Duration(*r.Duration) * time.Second)
		end = &e
	}
	status := StatusScheduled
	if !start.After(now) {
		status = StatusActive
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO events (title, description, start_time, end_time, channel_id, location, status, created_by, series_id, series_index)
		VALUES ($1, $2, $3, $4, $5, $6, $7, (SELECT created_by FROM event_series WHERE id = $8), $8, $9)
		RETURNING id`, r.Title, r.Description, start, end, r.ChannelID, r.Location, status, r.ID, i).Scan(&id)
	if err != nil {
		return id, fmt.Errorf("create occurrence: %w", err)
	}
	for pos, role := range r.Roles {
		if _, err := tx.Exec(ctx, `INSERT INTO event_roles (event_id, name, emoji, capacity, position) VALUES ($1, $2, $3, $4, $5)`, id, role.Name, role.Emoji, role.Capacity, pos); err != nil {
			return id, fmt.Errorf("copy role: %w", err)
		}
	}
	return id, nil
}

// GenerateSeries creates the occurrences of a series that start within the
// horizon after now, one transaction under the series' row lock (another
// server process waits or finds nothing left to do). Occurrences that start
// in the past are skipped, not created late. The series ends when its last
// index, its end date or the cap of MaxSeriesOccurrences is reached. It
// returns the ids of the events it created.
func (s *Store) GenerateSeries(ctx context.Context, seriesID uuid.UUID, now time.Time) ([]uuid.UUID, error) {
	var created []uuid.UUID
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		r, err := lockSeries(ctx, tx, seriesID)
		if err != nil || r.Status != "active" {
			return err
		}
		var next int
		if created, next, err = createDueOccurrences(ctx, tx, r, now); err != nil {
			return err
		}
		status := r.Status
		if r.finished(next) {
			status = "ended"
		}
		_, err = tx.Exec(ctx, `UPDATE event_series SET next_index = $2, status = $3 WHERE id = $1`, seriesID, next, status)
		return err
	})
	return created, err
}

// createDueOccurrences creates the occurrences from the series' next index on
// that start within the horizon and are not in the past, and returns them with
// the index to continue from.
func createDueOccurrences(ctx context.Context, tx pgx.Tx, r seriesRow, now time.Time) (created []uuid.UUID, next int, err error) {
	for next = r.NextIndex; !r.finished(next); next++ {
		start := r.start(next)
		if start.After(now.Add(seriesHorizon)) {
			break
		}
		if start.Before(now) {
			continue
		}
		id, err := insertOccurrence(ctx, tx, r, next, now)
		if err != nil {
			return created, next, err
		}
		created = append(created, id)
	}
	return created, next, nil
}

// finished says whether no occurrence with index i or later is ever created.
func (r seriesRow) finished(i int) bool {
	if r.EndIndex != nil && i > *r.EndIndex {
		return true
	}
	if i >= MaxSeriesOccurrences {
		return true
	}
	return r.Until != nil && r.start(i).After(*r.Until)
}

// ActiveSeries lists the series that may still create occurrences.
func (s *Store) ActiveSeries(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.DB.Query(ctx, `SELECT id FROM event_series WHERE status = 'active' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
