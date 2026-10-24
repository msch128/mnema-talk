package later

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

var (
	errTooManyScheduled = httpx.ErrConflict(fmt.Sprintf("you can have at most %d scheduled messages; remove one first", MaxScheduled))
	errScheduledGone    = httpx.ErrNotFound("scheduled message not found")
)

// Scheduled message states.
const (
	StatePending = "pending"
	StateFailed  = "failed"
)

// Failure reasons: codes the client words itself, so the server says no more
// than "refused" (what a refusal rests on can concern another member, for
// example a block).
const (
	ReasonSlowMode    = "slow_mode"
	ReasonNotAllowed  = "not_allowed"
	ReasonUnavailable = "unavailable"
	ReasonRefused     = "refused"
)

// Scheduled is one scheduled message of the caller.
type Scheduled struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	// ChannelName is empty for a conversation and for a channel the caller can
	// no longer see.
	ChannelName string     `json:"channel_name"`
	DM          bool       `json:"dm"`
	ParentID    *uuid.UUID `json:"parent_id" format:"uuid" extensions:"x-nullable"`
	ReplyToID   *uuid.UUID `json:"reply_to_id" format:"uuid" extensions:"x-nullable"`
	Content     string     `json:"content"`
	SendAt      time.Time  `json:"send_at" format:"date-time"`
	// State is "pending" (waiting for send_at) or "failed" (the server refused
	// to send it; it stays as a draft the caller can edit, reschedule or delete).
	State string `json:"state" enums:"pending,failed"`
	// FailureReason is set when State is failed: slow_mode, not_allowed,
	// unavailable or refused.
	FailureReason string    `json:"failure_reason" enums:",slow_mode,not_allowed,unavailable,refused"`
	CreatedAt     time.Time `json:"created_at" format:"date-time"`
	UpdatedAt     time.Time `json:"updated_at" format:"date-time"`
}

// ScheduledEvent is the payload of scheduled_message_update, sent to the
// author's own sessions only. State is "created", "updated", "sent", "failed"
// or "deleted"; Reason is set for "failed".
type ScheduledEvent struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	State     string    `json:"state" enums:"created,updated,sent,failed,deleted"`
	Reason    string    `json:"reason,omitempty" binding:"optional"`
}

const scheduledColumns = `s.id, s.channel_id, c.name, c.type = 'dm', s.parent_id, s.reply_to_id, s.content, s.send_at, s.state, s.failure_reason, s.created_at, s.updated_at`

func scanScheduled(row pgx.Row) (Scheduled, error) {
	var e Scheduled
	err := row.Scan(&e.ID, &e.ChannelID, &e.ChannelName, &e.DM, &e.ParentID, &e.ReplyToID, &e.Content, &e.SendAt, &e.State, &e.FailureReason, &e.CreatedAt, &e.UpdatedAt)
	if e.DM {
		e.ChannelName = ""
	}
	return e, err
}

// NewScheduled is a scheduled message to store. The handler has checked that the
// author may send in the channel now and that the content is valid.
type NewScheduled struct {
	User      uuid.UUID
	Channel   uuid.UUID
	ParentID  *uuid.UUID
	ReplyToID *uuid.UUID
	Content   string
	SendAt    time.Time
}

// AddScheduled stores n for sending at n.SendAt.
func (s *Service) AddScheduled(ctx context.Context, n NewScheduled) (Scheduled, error) {
	if err := s.checkTime(n.SendAt); err != nil {
		return Scheduled{}, err
	}
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		// Serialize one member's changes so the cap holds.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, n.User); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM scheduled_messages WHERE user_id = $1`, n.User).Scan(&count); err != nil {
			return err
		}
		if count >= MaxScheduled {
			return errTooManyScheduled
		}
		return tx.QueryRow(ctx, `
			INSERT INTO scheduled_messages (user_id, channel_id, parent_id, reply_to_id, content, send_at) VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id`, n.User, n.Channel, n.ParentID, n.ReplyToID, n.Content, n.SendAt).Scan(&id)
	})
	if err != nil {
		return Scheduled{}, err
	}
	e, err := scanScheduled(s.DB.QueryRow(ctx, `SELECT `+scheduledColumns+` FROM scheduled_messages s JOIN channels c ON c.id = s.channel_id WHERE s.id = $1`, id))
	if err != nil {
		return Scheduled{}, fmt.Errorf("load scheduled message: %w", err)
	}
	s.notify(n.User, events.ScheduledUpdate, ScheduledEvent{ID: id, ChannelID: n.Channel, State: "created"})
	return e, nil
}

// ListScheduled returns the member's scheduled messages: the failed ones first
// (they need attention), then the waiting ones by time.
func (s *Service) ListScheduled(ctx context.Context, sub perm.Subject) ([]Scheduled, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT `+scheduledColumns+` FROM scheduled_messages s JOIN channels c ON c.id = s.channel_id
		WHERE s.user_id = $1 ORDER BY (s.state = 'pending'), s.send_at, s.id LIMIT $2`, sub.ID, MaxScheduled)
	if err != nil {
		return nil, fmt.Errorf("list scheduled: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Scheduled, error) { return scanScheduled(row) })
	if err != nil {
		return nil, fmt.Errorf("list scheduled: %w", err)
	}
	for i := range out {
		ok, err := s.canView(ctx, sub, out[i].ChannelID)
		if err != nil {
			return nil, err
		}
		if !ok {
			out[i].ChannelName = ""
		}
	}
	return out, nil
}

// ScheduledPatch changes a scheduled message; a nil field stays as it is.
type ScheduledPatch struct {
	Content *string
	SendAt  *time.Time
}

// UpdateScheduled edits the member's scheduled message. A new time moves a
// failed message back to waiting; a failed message edited only in its text stays
// a draft. The send-time checks run when it is sent, not here.
func (s *Service) UpdateScheduled(ctx context.Context, user, id uuid.UUID, p ScheduledPatch) (Scheduled, error) {
	if p.SendAt != nil {
		if err := s.checkTime(*p.SendAt); err != nil {
			return Scheduled{}, err
		}
	}
	var channel uuid.UUID
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			UPDATE scheduled_messages SET
				content = COALESCE($3, content),
				send_at = COALESCE($4, send_at),
				state = CASE WHEN $4::timestamptz IS NOT NULL THEN 'pending' ELSE state END,
				failure_reason = CASE WHEN $4::timestamptz IS NOT NULL THEN '' ELSE failure_reason END,
				updated_at = clock_timestamp()
			WHERE id = $1 AND user_id = $2 RETURNING channel_id`, id, user, p.Content, p.SendAt).Scan(&channel)
		if errors.Is(err, pgx.ErrNoRows) {
			return errScheduledGone
		}
		return err
	})
	if err != nil {
		return Scheduled{}, err
	}
	e, err := scanScheduled(s.DB.QueryRow(ctx, `SELECT `+scheduledColumns+` FROM scheduled_messages s JOIN channels c ON c.id = s.channel_id WHERE s.id = $1`, id))
	if err != nil {
		return Scheduled{}, fmt.Errorf("load scheduled message: %w", err)
	}
	s.notify(user, events.ScheduledUpdate, ScheduledEvent{ID: id, ChannelID: channel, State: "updated"})
	return e, nil
}

// RemoveScheduled deletes the member's scheduled message (waiting or failed).
func (s *Service) RemoveScheduled(ctx context.Context, user, id uuid.UUID) error {
	var channel uuid.UUID
	err := s.DB.QueryRow(ctx, `DELETE FROM scheduled_messages WHERE id = $1 AND user_id = $2 RETURNING channel_id`, id, user).Scan(&channel)
	if errors.Is(err, pgx.ErrNoRows) {
		return errScheduledGone
	}
	if err != nil {
		return fmt.Errorf("remove scheduled: %w", err)
	}
	s.notify(user, events.ScheduledUpdate, ScheduledEvent{ID: id, ChannelID: channel, State: "deleted"})
	return nil
}
