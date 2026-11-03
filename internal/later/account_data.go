package later

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// accountData is reminders and scheduled messages in an export and an erasure.
type accountData struct{}

// RegisterAccountData adds later's export files and erasure statements.
func RegisterAccountData(r *userdata.Registry) {
	var d accountData
	r.Register(d, d)
}

func (accountData) Export(ctx context.Context, q db.Querier, user uuid.UUID, channels userdata.Channels, limits userdata.Limits) ([]userdata.File, error) {
	return []userdata.File{
		reminderFile(ctx, q, user, limits),
		scheduledFile(ctx, q, user, channels, limits),
	}, nil
}

func (accountData) Erase() []userdata.Step {
	return []userdata.Step{
		{Order: userdata.OrderReminders, SQL: `DELETE FROM message_reminders WHERE user_id = $1`},
		{Order: userdata.OrderScheduled, SQL: `DELETE FROM scheduled_messages WHERE user_id = $1`},
	}
}

func reminderFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	type reminder struct {
		MessageID   *uuid.UUID `json:"message_id" db:"message_id"`
		RemindAt    time.Time  `json:"remind_at" db:"remind_at"`
		DeliveredAt *time.Time `json:"delivered_at" db:"delivered_at"`
		CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	}
	return userdata.File{
		Name: "reminders.json", Order: userdata.FileReminders,
		Build: func() (any, bool, error) {
			rows, cut, err := userdata.FetchRows[reminder](ctx, q, limits.MaxRows, `SELECT message_id, remind_at, delivered_at, created_at
		FROM message_reminders WHERE user_id = $1 ORDER BY created_at LIMIT $2`, user)
			if err != nil {
				return nil, false, err
			}
			return userdata.AsList(rows), cut, nil
		},
	}
}

func scheduledFile(ctx context.Context, q db.Querier, user uuid.UUID, channels userdata.Channels, limits userdata.Limits) userdata.File {
	return userdata.File{
		Name: "scheduled_messages.json", Order: userdata.FileScheduled,
		Build: func() (any, bool, error) {
			return exportScheduled(ctx, q, user, channels, limits.MaxRows)
		},
	}
}

// exportScheduled lists the person's scheduled messages that were not sent yet.
// The text is the person's own words. A channel they can no longer see keeps
// its name to itself.
func exportScheduled(ctx context.Context, q db.Querier, user uuid.UUID, channels userdata.Channels, maxRows int) (any, bool, error) {
	type scheduled struct {
		ChannelID     uuid.UUID `json:"-" db:"channel_id"`
		ChannelName   *string   `json:"channel_name" db:"channel_name"`
		Content       string    `json:"content" db:"content"`
		SendAt        time.Time `json:"send_at" db:"send_at"`
		State         string    `json:"state" db:"state"`
		FailureReason string    `json:"failure_reason" db:"failure_reason"`
		CreatedAt     time.Time `json:"created_at" db:"created_at"`
	}
	rs, cut, err := userdata.FetchRows[scheduled](ctx, q, maxRows, `SELECT s.channel_id, CASE WHEN c.type = 'dm' THEN NULL ELSE c.name END AS channel_name,
		s.content, s.send_at, s.state, s.failure_reason, s.created_at
		FROM scheduled_messages s JOIN channels c ON c.id = s.channel_id WHERE s.user_id = $1 ORDER BY s.created_at LIMIT $2`, user)
	if err != nil {
		return nil, false, err
	}
	for i := range rs {
		if !channels.Has(rs[i].ChannelID) {
			rs[i].ChannelName = nil
		}
	}
	return userdata.AsList(rs), cut, nil
}
