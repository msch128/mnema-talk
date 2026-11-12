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

// NewStore returns a Store that reads and writes through q.
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
	SeriesID     *uuid.UUID `db:"series_id"`
	SeriesIndex  *int       `db:"series_index"`
}

func (r eventRow) toEvent() *CommunityEvent {
	var coverURL *string
	if r.CoverMediaID != nil {
		u := auth.AvatarURL(ptrString(r.CoverMediaID.String()))
		if u != "" {
			coverURL = &u
		}
	}
	var series *SeriesInfo
	if r.SeriesID != nil && r.SeriesIndex != nil {
		series = &SeriesInfo{ID: *r.SeriesID, Index: *r.SeriesIndex}
	}
	return &CommunityEvent{
		Series:      series,
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
		Roles:       []EventRole{},
	}
}

func ptrString(s string) *string { return &s }

const eventColumns = `id, title, description, start_time, end_time, channel_id, location, cover_media_id, status, created_by, created_at, updated_at, series_id, series_index`

// scanEvent reads one row selected with eventColumns.
func scanEvent(row pgx.Row) (eventRow, error) {
	var r eventRow
	err := row.Scan(&r.ID, &r.Title, &r.Description, &r.StartTime, &r.EndTime, &r.ChannelID, &r.Location, &r.CoverMediaID, &r.Status, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt, &r.SeriesID, &r.SeriesIndex)
	return r, err
}

// newUserRef builds the participant reference of a user row.
func newUserRef(id uuid.UUID, username, displayName string, avatarKey *string) UserRef {
	ref := UserRef{ID: id, Username: username, DisplayName: displayName}
	if avatarKey != nil {
		if u := auth.AvatarURL(avatarKey); u != "" {
			ref.AvatarURL = &u
		}
	}
	return ref
}

// Create inserts an event for creatorID. An event that already started (the
// start is not in the future) is created active, so it never waits for the
// scheduler to show up as running.
func (s *Store) Create(ctx context.Context, req CreateEventRequest, creatorID uuid.UUID) (*CommunityEvent, error) {
	return s.create(ctx, req, creatorID, nil)
}

// create is Create with audited, if set, run in the same transaction with
// the new event (the audit entry).
func (s *Store) create(ctx context.Context, req CreateEventRequest, creatorID uuid.UUID, audited func(pgx.Tx, *CommunityEvent) error) (*CommunityEvent, error) {
	status := StatusScheduled
	now := time.Now()
	if !req.StartTime.After(now) {
		status = StatusActive
	}

	var ev *CommunityEvent
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		row, err := scanEvent(tx.QueryRow(ctx, `
			INSERT INTO events (title, description, start_time, end_time, channel_id, location, status, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
			RETURNING `+eventColumns,
			req.Title, req.Description, req.StartTime, req.EndTime, req.ChannelID, req.Location, status, creatorID,
		))
		if err != nil {
			return fmt.Errorf("create event: %w", err)
		}
		if ev = row.toEvent(); audited == nil {
			return nil
		}
		return audited(tx, ev)
	})
	if err != nil {
		return nil, err
	}
	return ev, nil
}

// Get loads one event with its RSVP counts, the caller's own answer (when
// callerID is set) and its roles and sign-ups; an unknown id is a 404.
func (s *Store) Get(ctx context.Context, id uuid.UUID, callerID *uuid.UUID) (*CommunityEvent, error) {
	row, err := scanEvent(s.DB.QueryRow(ctx, `SELECT `+eventColumns+` FROM events WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("event not found")
	}
	if err != nil {
		return nil, err
	}
	ev := row.toEvent()
	if err := s.fillRSVPsBatch(ctx, []*CommunityEvent{ev}, callerID); err != nil {
		return nil, err
	}
	if err := s.fillSignups(ctx, ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// Update applies the fields req names in one statement, so a concurrent
// cancellation, edit or scheduler step is never overwritten with a stale
// copy. A changed start or end re-derives the status from the new times
// (cancelled events stay cancelled) and re-arms the reminder.
func (s *Store) Update(ctx context.Context, id uuid.UUID, req UpdateEventRequest) (*CommunityEvent, error) {
	var row eventRow
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		row, err = updateEvent(ctx, tx, id, req)
		if err != nil || (req.ChannelID == nil && req.Location == nil) {
			return err
		}
		// The cover follows the event's place in the same transaction, so a
		// failure here never leaves the event moved and the cover behind.
		if _, err := tx.Exec(ctx, `UPDATE media SET channel_id = $2 WHERE id = $1`, row.CoverMediaID, row.ChannelID); err != nil {
			return fmt.Errorf("move event cover: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id, nil)
}

// updateEvent runs the update statement of Update inside tx.
func updateEvent(ctx context.Context, tx pgx.Tx, id uuid.UUID, req UpdateEventRequest) (eventRow, error) {
	row, err := scanEvent(tx.QueryRow(ctx, `
		UPDATE events SET
		    title = COALESCE($2, title),
		    description = COALESCE($3, description),
		    start_time = COALESCE($4, start_time),
		    end_time = CASE WHEN $5 THEN NULL ELSE COALESCE($6, end_time) END,
		    channel_id = CASE WHEN $7::uuid IS NOT NULL THEN $7 WHEN $8::text IS NOT NULL THEN NULL ELSE channel_id END,
		    location = CASE WHEN $7::uuid IS NOT NULL THEN NULL WHEN $8::text IS NOT NULL THEN $8 ELSE location END,
		    reminder_sent_at = CASE WHEN $4::timestamptz IS NULL OR start_time = $4 THEN reminder_sent_at ELSE NULL END,
		    status = CASE
		        WHEN status = 'cancelled' OR ($4::timestamptz IS NULL AND NOT $5 AND $6::timestamptz IS NULL) THEN status
		        WHEN COALESCE($4, start_time) > NOW() THEN 'scheduled'
		        WHEN COALESCE(CASE WHEN $5 THEN NULL ELSE COALESCE($6, end_time) END, COALESCE($4, start_time) + `+defaultEventDuration+`) <= NOW() THEN 'completed'
		        ELSE 'active' END,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING `+eventColumns,
		id, req.Title, req.Description, req.StartTime, req.ClearEndTime, req.EndTime, req.ChannelID, req.Location,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return row, httpx.ErrNotFound("event not found")
	}
	if err != nil {
		return row, fmt.Errorf("update event: %w", err)
	}
	return row, nil
}

// Cancel marks a scheduled or active event cancelled. An event that is
// cancelled or over already is refused with a conflict, so nothing is
// re-announced or re-audited.
func (s *Store) Cancel(ctx context.Context, id uuid.UUID) (*CommunityEvent, error) {
	_, err := scanEvent(s.DB.QueryRow(ctx, `
		UPDATE events SET status = 'cancelled', updated_at = NOW()
		WHERE id = $1 AND status IN ('scheduled', 'active')
		RETURNING `+eventColumns, id))
	if errors.Is(err, pgx.ErrNoRows) {
		current, getErr := s.Get(ctx, id, nil)
		if getErr != nil {
			return nil, getErr
		}
		return nil, httpx.ErrConflict("this event is " + current.Status + " and cannot be cancelled")
	}
	if err != nil {
		return nil, fmt.Errorf("cancel event: %w", err)
	}
	return s.Get(ctx, id, nil)
}

// ReplaceCover makes mediaID the event's cover and returns the cover it
// replaced, in one statement that locks the event row.
func (s *Store) ReplaceCover(ctx context.Context, id uuid.UUID, mediaID uuid.UUID) (*uuid.UUID, error) {
	return s.swapCover(ctx, id, &mediaID)
}

// ClearCover removes the event's cover and returns the one it had.
func (s *Store) ClearCover(ctx context.Context, id uuid.UUID) (*uuid.UUID, error) {
	return s.swapCover(ctx, id, nil)
}

func (s *Store) swapCover(ctx context.Context, id uuid.UUID, mediaID *uuid.UUID) (*uuid.UUID, error) {
	var prev *uuid.UUID
	// A new cover takes the channel of the event row locked here, in the same
	// statement: an event moved while the file was uploading must not leave its
	// cover in the old channel.
	err := s.DB.QueryRow(ctx, `
		WITH old AS (SELECT id, cover_media_id FROM events WHERE id = $1 FOR UPDATE),
		upd AS (
		    UPDATE events e SET cover_media_id = $2::uuid, updated_at = NOW()
		    FROM old WHERE e.id = old.id
		    RETURNING old.cover_media_id AS prev, e.channel_id),
		mv AS (
		    UPDATE media SET channel_id = upd.channel_id FROM upd WHERE media.id = $2::uuid)
		SELECT prev FROM upd`, id, mediaID).Scan(&prev)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("event not found")
	}
	return prev, err
}

// SetRSVP records the answer. A sign-up counts as going, so any other answer
// gives up the member's role slot and moves that role's waitlist up; the
// members who moved up are returned. The slot goes first: sign-ups take the
// event and role locks before they write the RSVP, and so must this, or the
// two wait for each other.
func (s *Store) SetRSVP(ctx context.Context, eventID, userID uuid.UUID, response string) ([]Promotion, error) {
	var promoted []Promotion
	err := s.inTx(ctx, func(tx pgx.Tx) (err error) {
		promoted, err = setRSVP(ctx, tx, eventID, userID, response)
		return err
	})
	return promoted, err
}

// AnswerRSVP is SetRSVP that also returns the event as the member sees it
// afterwards, read in the same transaction: an error means that the answer
// was not stored (and nobody moved up unannounced).
func (s *Store) AnswerRSVP(ctx context.Context, eventID, userID uuid.UUID, response string) (*CommunityEvent, []Promotion, error) {
	var promoted []Promotion
	var ev *CommunityEvent
	err := s.inTx(ctx, func(tx pgx.Tx) (err error) {
		if promoted, err = setRSVP(ctx, tx, eventID, userID, response); err != nil {
			return err
		}
		ev, err = (&Store{DB: tx}).Get(ctx, eventID, &userID)
		return err
	})
	return ev, promoted, err
}

func setRSVP(ctx context.Context, tx pgx.Tx, eventID, userID uuid.UUID, response string) ([]Promotion, error) {
	var promoted []Promotion
	if response != RSVPGoing {
		var err error
		if _, promoted, err = removeSignup(ctx, tx, eventID, userID); err != nil {
			return nil, err
		}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO event_rsvps (event_id, user_id, response, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (event_id, user_id)
		DO UPDATE SET response = EXCLUDED.response, updated_at = NOW()`,
		eventID, userID, response,
	)
	return promoted, err
}

// DeleteRSVP withdraws the answer and, like any answer but going, gives up the
// member's role slot (see SetRSVP).
func (s *Store) DeleteRSVP(ctx context.Context, eventID, userID uuid.UUID) ([]Promotion, error) {
	var promoted []Promotion
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		if _, promoted, err = removeSignup(ctx, tx, eventID, userID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM event_rsvps WHERE event_id = $1 AND user_id = $2`, eventID, userID)
		return err
	})
	return promoted, err
}

// ListRSVPs returns everyone who answered an event, the latest answer first.
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
		item.User = newUserRef(item.User.ID, item.User.Username, item.User.DisplayName, avatarKey)
		items = append(items, item)
	}
	return items, rows.Err()
}
