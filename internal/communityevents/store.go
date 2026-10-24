package communityevents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Store handles database operations for community events and RSVPs.
type Store struct {
	DB db.Querier
}

func NewStore(q db.Querier) *Store {
	return &Store{DB: q}
}

type eventRow struct {
	ID           uuid.UUID  `db:"id"`
	Title        string     `db:"title"`
	Description  string     `db:"description"`
	StartTime    time.Time  `db:"start_time"`
	EndTime      *time.Time `db:"end_time"`
	ChannelID    *uuid.UUID `db:"channel_id"`
	Location     *string    `db:"location"`
	CoverMediaID *uuid.UUID `db:"cover_media_id"`
	Status       string     `db:"status"`
	CreatedBy    *uuid.UUID `db:"created_by"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
}

func (r eventRow) toEvent() *CommunityEvent {
	var coverURL *string
	if r.CoverMediaID != nil {
		u := auth.AvatarURL(ptrString(r.CoverMediaID.String()))
		if u != "" {
			coverURL = &u
		}
	}
	return &CommunityEvent{
		ID:          r.ID,
		Title:       r.Title,
		Description: r.Description,
		StartTime:   r.StartTime,
		EndTime:     r.EndTime,
		ChannelID:   r.ChannelID,
		Location:    r.Location,
		CoverURL:    coverURL,
		Status:      r.Status,
		CreatedBy:   r.CreatedBy,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func ptrString(s string) *string { return &s }

func (s *Store) Create(ctx context.Context, req CreateEventRequest, creatorID uuid.UUID) (*CommunityEvent, error) {
	status := StatusScheduled
	now := time.Now()
	if !req.StartTime.After(now) {
		status = StatusActive
	}

	var row eventRow
	err := s.DB.QueryRow(ctx, `
		INSERT INTO events (title, description, start_time, end_time, channel_id, location, status, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		RETURNING id, title, description, start_time, end_time, channel_id, location, cover_media_id, status, created_by, created_at, updated_at`,
		req.Title, req.Description, req.StartTime, req.EndTime, req.ChannelID, req.Location, status, creatorID,
	).Scan(&row.ID, &row.Title, &row.Description, &row.StartTime, &row.EndTime, &row.ChannelID, &row.Location, &row.CoverMediaID, &row.Status, &row.CreatedBy, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create event: %w", err)
	}
	ev := row.toEvent()
	return ev, nil
}

func (s *Store) Get(ctx context.Context, id uuid.UUID, callerID *uuid.UUID) (*CommunityEvent, error) {
	var row eventRow
	err := s.DB.QueryRow(ctx, `
		SELECT id, title, description, start_time, end_time, channel_id, location, cover_media_id, status, created_by, created_at, updated_at
		FROM events WHERE id = $1`, id,
	).Scan(&row.ID, &row.Title, &row.Description, &row.StartTime, &row.EndTime, &row.ChannelID, &row.Location, &row.CoverMediaID, &row.Status, &row.CreatedBy, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("event not found")
	}
	if err != nil {
		return nil, err
	}
	ev := row.toEvent()
	if err := s.fillRSVPs(ctx, ev, callerID); err != nil {
		return nil, err
	}
	return ev, nil
}

func (s *Store) List(ctx context.Context, callerID *uuid.UUID) ([]*CommunityEvent, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, title, description, start_time, end_time, channel_id, location, cover_media_id, status, created_by, created_at, updated_at
		FROM events
		ORDER BY start_time ASC`)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var events []*CommunityEvent
	for rows.Next() {
		var row eventRow
		if err := rows.Scan(&row.ID, &row.Title, &row.Description, &row.StartTime, &row.EndTime, &row.ChannelID, &row.Location, &row.CoverMediaID, &row.Status, &row.CreatedBy, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		events = append(events, row.toEvent())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, ev := range events {
		if err := s.fillRSVPs(ctx, ev, callerID); err != nil {
			return nil, err
		}
	}
	return events, nil
}

func (s *Store) fillRSVPs(ctx context.Context, ev *CommunityEvent, callerID *uuid.UUID) error {
	if err := s.fillCounts(ctx, ev); err != nil {
		return err
	}
	if err := s.fillCallerRSVP(ctx, ev, callerID); err != nil {
		return err
	}
	return s.fillGoingMembers(ctx, ev)
}

func (s *Store) fillCounts(ctx context.Context, ev *CommunityEvent) error {
	countRows, err := s.DB.Query(ctx, `
		SELECT response, COUNT(*) FROM event_rsvps WHERE event_id = $1 GROUP BY response`, ev.ID)
	if err != nil {
		return err
	}
	defer countRows.Close()
	for countRows.Next() {
		var resp string
		var count int
		if err := countRows.Scan(&resp, &count); err != nil {
			return err
		}
		switch resp {
		case RSVPGoing:
			ev.RSVPCounts.Going = count
		case RSVPInterested:
			ev.RSVPCounts.Interested = count
		case RSVPNotGoing:
			ev.RSVPCounts.NotGoing = count
		}
	}
	return countRows.Err()
}

func (s *Store) fillCallerRSVP(ctx context.Context, ev *CommunityEvent, callerID *uuid.UUID) error {
	if callerID == nil {
		return nil
	}
	var myResp string
	err := s.DB.QueryRow(ctx, `SELECT response FROM event_rsvps WHERE event_id = $1 AND user_id = $2`, ev.ID, *callerID).Scan(&myResp)
	if err == nil {
		ev.MyRSVP = &myResp
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func (s *Store) fillGoingMembers(ctx context.Context, ev *CommunityEvent) error {
	goingRows, err := s.DB.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_s3_key
		FROM event_rsvps r
		JOIN users u ON u.id = r.user_id
		WHERE r.event_id = $1 AND r.response = 'going'
		ORDER BY r.updated_at ASC
		LIMIT 20`, ev.ID)
	if err != nil {
		return err
	}
	defer goingRows.Close()

	for goingRows.Next() {
		var member UserRef
		var avatarKey *string
		if err := goingRows.Scan(&member.ID, &member.Username, &member.DisplayName, &avatarKey); err != nil {
			return err
		}
		if avatarKey != nil {
			if u := auth.AvatarURL(avatarKey); u != "" {
				member.AvatarURL = &u
			}
		}
		ev.GoingMembers = append(ev.GoingMembers, member)
	}
	return goingRows.Err()
}

func (s *Store) Update(ctx context.Context, id uuid.UUID, req UpdateEventRequest) (*CommunityEvent, error) {
	current, err := s.Get(ctx, id, nil)
	if err != nil {
		return nil, err
	}

	title := current.Title
	if req.Title != nil {
		title = *req.Title
	}
	desc := current.Description
	if req.Description != nil {
		desc = *req.Description
	}
	startTime := current.StartTime
	if req.StartTime != nil {
		startTime = *req.StartTime
	}
	endTime := current.EndTime
	if req.EndTime != nil {
		endTime = req.EndTime
	}
	status := current.Status
	if req.Status != nil {
		status = *req.Status
	}
	channelID := current.ChannelID
	location := current.Location
	if req.ChannelID != nil {
		channelID = req.ChannelID
		location = nil
	} else if req.Location != nil {
		location = req.Location
		channelID = nil
	}

	var row eventRow
	err = s.DB.QueryRow(ctx, `
		UPDATE events
		SET title = $1, description = $2, start_time = $3, end_time = $4, channel_id = $5, location = $6, status = $7, updated_at = NOW()
		WHERE id = $8
		RETURNING id, title, description, start_time, end_time, channel_id, location, cover_media_id, status, created_by, created_at, updated_at`,
		title, desc, startTime, endTime, channelID, location, status, id,
	).Scan(&row.ID, &row.Title, &row.Description, &row.StartTime, &row.EndTime, &row.ChannelID, &row.Location, &row.CoverMediaID, &row.Status, &row.CreatedBy, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update event: %w", err)
	}
	return s.Get(ctx, id, nil)
}

func (s *Store) Cancel(ctx context.Context, id uuid.UUID) (*CommunityEvent, error) {
	var row eventRow
	err := s.DB.QueryRow(ctx, `
		UPDATE events SET status = 'cancelled', updated_at = NOW()
		WHERE id = $1
		RETURNING id, title, description, start_time, end_time, channel_id, location, cover_media_id, status, created_by, created_at, updated_at`,
		id,
	).Scan(&row.ID, &row.Title, &row.Description, &row.StartTime, &row.EndTime, &row.ChannelID, &row.Location, &row.CoverMediaID, &row.Status, &row.CreatedBy, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("event not found")
	}
	if err != nil {
		return nil, fmt.Errorf("cancel event: %w", err)
	}
	return s.Get(ctx, id, nil)
}

func (s *Store) SetCover(ctx context.Context, id uuid.UUID, mediaID uuid.UUID) error {
	tag, err := s.DB.Exec(ctx, `UPDATE events SET cover_media_id = $1, updated_at = NOW() WHERE id = $2`, mediaID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound("event not found")
	}
	return nil
}

func (s *Store) ClearCover(ctx context.Context, id uuid.UUID) (*uuid.UUID, error) {
	var prev *uuid.UUID
	err := s.DB.QueryRow(ctx, `
		UPDATE events SET cover_media_id = NULL, updated_at = NOW()
		WHERE id = $1
		RETURNING cover_media_id`, id).Scan(&prev)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("event not found")
	}
	return prev, err
}

func (s *Store) SetRSVP(ctx context.Context, eventID, userID uuid.UUID, response string) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO event_rsvps (event_id, user_id, response, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (event_id, user_id)
		DO UPDATE SET response = EXCLUDED.response, updated_at = NOW()`,
		eventID, userID, response,
	)
	return err
}

func (s *Store) DeleteRSVP(ctx context.Context, eventID, userID uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM event_rsvps WHERE event_id = $1 AND user_id = $2`, eventID, userID)
	return err
}

func (s *Store) ListRSVPs(ctx context.Context, eventID uuid.UUID) ([]RSVPItem, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_s3_key, r.response, r.updated_at
		FROM event_rsvps r
		JOIN users u ON u.id = r.user_id
		WHERE r.event_id = $1
		ORDER BY r.updated_at DESC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []RSVPItem
	for rows.Next() {
		var item RSVPItem
		var avatarKey *string
		if err := rows.Scan(&item.User.ID, &item.User.Username, &item.User.DisplayName, &avatarKey, &item.Response, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if avatarKey != nil {
			u := auth.AvatarURL(avatarKey)
			if u != "" {
				item.User.AvatarURL = &u
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
