package communityevents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// followingEvents lists the open occurrences from index i on (the first being
// occurrence i itself), oldest first. Cancelled and finished ones are left alone.
func followingEvents(ctx context.Context, tx pgx.Tx, seriesID uuid.UUID, i int) ([]followingEvent, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, series_index, start_time, cover_media_id FROM events
		WHERE series_id = $1 AND series_index >= $2 AND status IN ('scheduled', 'active')
		ORDER BY series_index`, seriesID, i)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (followingEvent, error) {
		var f followingEvent
		return f, r.Scan(&f.ID, &f.Index, &f.Start, &f.Cover)
	})
}

type followingEvent struct {
	ID    uuid.UUID
	Index int
	Start time.Time
	Cover *uuid.UUID
}

// UpdateFollowing applies an edit to occurrence ev and every open occurrence
// after it, and to the series' template (so the occurrences created later
// follow it as well). A new start_time becomes the new anchor: the target
// takes it, the later occurrences keep their distance in the series' frequency
// and wall-clock time zone. Without a new start each occurrence keeps its own.
// The occurrences before ev are not touched. It returns the changed events.
func (s *Store) UpdateFollowing(ctx context.Context, ev *CommunityEvent, req UpdateEventRequest) ([]uuid.UUID, error) {
	req = withoutUnchanged(ev, req)
	var changed []uuid.UUID
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		r, err := lockSeries(ctx, tx, ev.Series.ID)
		if err != nil {
			return err
		}
		editTemplate(&r, ev, req)
		if err := saveTemplate(ctx, tx, r); err != nil {
			return err
		}
		changed, err = rewriteFollowing(ctx, tx, r, ev.Series.Index, req)
		return err
	})
	return changed, err
}

// editTemplate applies an edit to the series' template: the texts and the
// place, a new start as the new anchor (at the edited occurrence) and a
// changed end as the new duration.
func editTemplate(r *seriesRow, ev *CommunityEvent, req UpdateEventRequest) {
	m := mergeUpdate(ev, req)
	r.Title, r.Description, r.ChannelID, r.Location = m.title, m.desc, m.channel, m.location
	if req.StartTime != nil {
		r.Anchor, r.AnchorIndex = m.start, ev.Series.Index
	}
	if req.StartTime != nil || req.EndTime != nil || req.ClearEndTime {
		r.Duration = durationOf(m.start, m.end)
	}
}

// rewriteFollowing writes the template into the open occurrences from index i on; each keeps its own start unless the edit moved the anchor.
func rewriteFollowing(ctx context.Context, tx pgx.Tx, r seriesRow, i int, req UpdateEventRequest) ([]uuid.UUID, error) {
	list, err := followingEvents(ctx, tx, r.ID, i)
	if err != nil {
		return nil, err
	}
	changed := make([]uuid.UUID, 0, len(list))
	for _, f := range list {
		start := f.Start
		if req.StartTime != nil {
			start = r.start(f.Index)
		}
		if err := rewriteOccurrence(ctx, tx, r, f, start, req); err != nil {
			return nil, err
		}
		changed = append(changed, f.ID)
	}
	return changed, nil
}

func durationOf(start time.Time, end *time.Time) *int {
	if end == nil {
		return nil
	}
	d := int(end.Sub(start).Seconds())
	return &d
}

func saveTemplate(ctx context.Context, tx pgx.Tx, r seriesRow) error {
	_, err := tx.Exec(ctx, `
		UPDATE event_series SET title = $2, description = $3, channel_id = $4, location = $5,
		       anchor_start = $6, anchor_index = $7, duration_seconds = $8 WHERE id = $1`,
		r.ID, r.Title, r.Description, r.ChannelID, r.Location, r.Anchor, r.AnchorIndex, r.Duration)
	return err
}

// rewriteOccurrence writes into one occurrence what the edit changed and
// nothing else: an occurrence that was renamed, moved to another place or to
// another time by hand keeps that unless this edit touches the same field.
func rewriteOccurrence(ctx context.Context, tx pgx.Tx, r seriesRow, f followingEvent, start time.Time, req UpdateEventRequest) error {
	var end *time.Time
	if r.Duration != nil {
		e := start.Add(time.Duration(*r.Duration) * time.Second)
		end = &e
	}
	timeChanged := req.StartTime != nil || req.EndTime != nil || req.ClearEndTime
	placeChanged := req.ChannelID != nil || req.Location != nil
	_, err := tx.Exec(ctx, `
		UPDATE events SET
		       title = CASE WHEN $8 THEN $2 ELSE title END,
		       description = CASE WHEN $9 THEN $3 ELSE description END,
		       channel_id = CASE WHEN $10 THEN $6::uuid ELSE channel_id END,
		       location = CASE WHEN $10 THEN $7::text ELSE location END,
		       start_time = CASE WHEN $11 THEN $4::timestamptz ELSE start_time END,
		       end_time = CASE WHEN $11 THEN $5::timestamptz ELSE end_time END,
		       reminder_sent_at = CASE WHEN $11 AND start_time <> $4::timestamptz THEN NULL ELSE reminder_sent_at END,
		       status = CASE WHEN NOT $11 THEN status
		                     WHEN $4::timestamptz > NOW() THEN 'scheduled'
		                     WHEN COALESCE($5::timestamptz, $4::timestamptz + `+defaultEventDuration+`) <= NOW() THEN 'completed'
		                     ELSE 'active' END,
		       updated_at = NOW()
		WHERE id = $1`, f.ID, r.Title, r.Description, start, end, r.ChannelID, r.Location,
		req.Title != nil, req.Description != nil, placeChanged, timeChanged)
	if err != nil {
		return fmt.Errorf("update occurrence: %w", err)
	}
	if f.Cover != nil && placeChanged {
		if _, err := tx.Exec(ctx, `UPDATE media SET channel_id = $2 WHERE id = $1`, f.Cover, r.ChannelID); err != nil {
			return fmt.Errorf("move occurrence cover: %w", err)
		}
	}
	return nil
}

// withoutUnchanged drops the fields of an edit that name what the occurrence
// already has (a form that always sends its start must not re-anchor the series).
func withoutUnchanged(ev *CommunityEvent, req UpdateEventRequest) UpdateEventRequest {
	if req.StartTime != nil && req.StartTime.Equal(ev.StartTime) {
		req.StartTime = nil
	}
	if req.EndTime != nil && ev.EndTime != nil && req.EndTime.Equal(*ev.EndTime) {
		req.EndTime = nil
	}
	if req.ClearEndTime && ev.EndTime == nil {
		req.ClearEndTime = false
	}
	if req.ChannelID != nil && ev.ChannelID != nil && *req.ChannelID == *ev.ChannelID {
		req.ChannelID = nil
	}
	if req.Location != nil && ev.Location != nil && *req.Location == *ev.Location {
		req.Location = nil
	}
	if req.Title != nil && *req.Title == ev.Title {
		req.Title = nil
	}
	if req.Description != nil && *req.Description == ev.Description {
		req.Description = nil
	}
	return req
}

// CancelFollowing cancels occurrence ev and every open occurrence after it and
// ends the series there (nothing after it is created). The occurrences before
// ev stay as they are. It returns the cancelled events.
func (s *Store) CancelFollowing(ctx context.Context, ev *CommunityEvent) ([]uuid.UUID, error) {
	var cancelled []uuid.UUID
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		r, err := lockSeries(ctx, tx, ev.Series.ID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			UPDATE events SET status = 'cancelled', updated_at = NOW()
			WHERE series_id = $1 AND series_index >= $2 AND status IN ('scheduled', 'active')
			RETURNING id`, r.ID, ev.Series.Index)
		if err != nil {
			return err
		}
		if cancelled, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return err
		}
		if len(cancelled) == 0 {
			return errNothingOpen // roll back: the series is not ended by a cancellation that cancelled nothing
		}
		_, err = tx.Exec(ctx, `UPDATE event_series SET end_index = $2, status = 'ended' WHERE id = $1`, r.ID, ev.Series.Index-1)
		return err
	})
	if errors.Is(err, errNothingOpen) {
		return nil, nil
	}
	return cancelled, err
}

// errNothingOpen is the sentinel that rolls back a cancellation with nothing to cancel.
var errNothingOpen = errors.New("no open occurrence")

// SetSeriesRoles makes the role list the series' template, so occurrences
// created later start with it, and gives it to the open occurrences after ev
// that have no sign-ups. An occurrence with sign-ups keeps its roles (they are
// counted in skipped); ev's own roles are set by SetRoles.
func (s *Store) SetSeriesRoles(ctx context.Context, ev *CommunityEvent, in []RoleInput) (updated, skipped int, err error) {
	roles, err := validateRoles(stripIDs(in))
	if err != nil {
		return 0, 0, err
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		updated, skipped, err = setSeriesRolesTx(ctx, tx, ev, roles)
		return err
	})
	return updated, skipped, err
}

func setSeriesRolesTx(ctx context.Context, tx pgx.Tx, ev *CommunityEvent, roles []RoleInput) (updated, skipped int, err error) {
	r, err := lockSeries(ctx, tx, ev.Series.ID)
	if err != nil {
		return 0, 0, err
	}
	raw, _ := json.Marshal(roles)
	if _, err := tx.Exec(ctx, `UPDATE event_series SET roles = $2 WHERE id = $1`, r.ID, raw); err != nil {
		return 0, 0, err
	}
	list, err := followingEvents(ctx, tx, r.ID, ev.Series.Index+1)
	if err != nil {
		return 0, 0, err
	}
	for _, f := range list {
		ok, err := replaceRoles(ctx, tx, f.ID, roles)
		if err != nil {
			return 0, 0, err
		}
		if ok {
			updated++
		} else {
			skipped++
		}
	}
	return updated, skipped, nil
}

// replaceRoles swaps the roles of an event for roles when nobody has signed up.
func replaceRoles(ctx context.Context, tx pgx.Tx, eventID uuid.UUID, roles []RoleInput) (bool, error) {
	if _, err := openEvent(ctx, tx, eventID, true); err != nil {
		return false, err
	}
	if _, err := lockRoles(ctx, tx, eventID); err != nil {
		return false, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM event_signups WHERE event_id = $1`, eventID).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM event_roles WHERE event_id = $1`, eventID); err != nil {
		return false, err
	}
	for pos, role := range roles {
		if _, err := tx.Exec(ctx, `INSERT INTO event_roles (event_id, name, emoji, capacity, position) VALUES ($1, $2, $3, $4, $5)`, eventID, role.Name, role.Emoji, role.Capacity, pos); err != nil {
			return false, err
		}
	}
	return true, nil
}
