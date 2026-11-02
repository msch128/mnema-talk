package communityevents

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// fillSeries completes the series reference of the events that belong to one.
func (s *Store) fillSeries(ctx context.Context, list []*CommunityEvent) error {
	var ids []uuid.UUID
	for _, ev := range list {
		if ev.Series != nil {
			ids = append(ids, ev.Series.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	type info struct {
		freq, zone string
		until      *time.Time
		ended      bool
	}
	rows, err := s.DB.Query(ctx, `SELECT id, frequency, timezone, until_at, status = 'ended' FROM event_series WHERE id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := map[uuid.UUID]info{}
	for rows.Next() {
		var id uuid.UUID
		var i info
		if err := rows.Scan(&id, &i.freq, &i.zone, &i.until, &i.ended); err != nil {
			return err
		}
		byID[id] = i
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, ev := range list {
		if ev.Series == nil {
			continue
		}
		i := byID[ev.Series.ID]
		ev.Series.Frequency, ev.Series.Timezone, ev.Series.Until, ev.Series.Ended = i.freq, i.zone, i.until, i.ended
	}
	return nil
}
