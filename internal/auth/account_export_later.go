package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

// exportReminders lists the person's reminders by message id and time. The text
// of the message belongs to its author as much as to the person and is not
// exported.
func exportReminders(ctx context.Context, q db.Querier, user uuid.UUID) (capped, error) {
	type reminder struct {
		MessageID   *uuid.UUID `json:"message_id" db:"message_id"`
		RemindAt    time.Time  `json:"remind_at" db:"remind_at"`
		DeliveredAt *time.Time `json:"delivered_at" db:"delivered_at"`
		CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	}
	return listDataset[reminder](ctx, q, user, `SELECT message_id, remind_at, delivered_at, created_at
		FROM message_reminders WHERE user_id = $1 ORDER BY created_at LIMIT $2`)
}

// exportScheduled lists the person's scheduled messages that were not sent yet
// (waiting or failed). The text is the person's own words.
func exportScheduled(ctx context.Context, q db.Querier, user uuid.UUID, viewable viewSet) (capped, error) {
	type scheduled struct {
		ChannelID     uuid.UUID `json:"-" db:"channel_id"`
		ChannelName   *string   `json:"channel_name" db:"channel_name"`
		Content       string    `json:"content" db:"content"`
		SendAt        time.Time `json:"send_at" db:"send_at"`
		State         string    `json:"state" db:"state"`
		FailureReason string    `json:"failure_reason" db:"failure_reason"`
		CreatedAt     time.Time `json:"created_at" db:"created_at"`
	}
	rs, cut, err := fetchRows[scheduled](ctx, q, exportMaxRows, `SELECT s.channel_id, CASE WHEN c.type = 'dm' THEN NULL ELSE c.name END AS channel_name,
		s.content, s.send_at, s.state, s.failure_reason, s.created_at
		FROM scheduled_messages s JOIN channels c ON c.id = s.channel_id WHERE s.user_id = $1 ORDER BY s.created_at LIMIT $2`, user)
	if err != nil {
		return capped{}, err
	}
	for i := range rs {
		// A channel the person can no longer see keeps its name to itself.
		if !viewable.has(rs[i].ChannelID) {
			rs[i].ChannelName = nil
		}
	}
	return listOf(rs, cut), nil
}
