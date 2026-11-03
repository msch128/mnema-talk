package communityevents

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// accountData is community events in an export and an erasure.
type accountData struct{}

// RegisterAccountData adds event RSVPs, sign-ups and comments.
func RegisterAccountData(r *userdata.Registry) {
	var d accountData
	r.Register(d, d)
}

func (accountData) Export(ctx context.Context, q db.Querier, user uuid.UUID, _ userdata.Channels, limits userdata.Limits) ([]userdata.File, error) {
	return []userdata.File{
		rsvpFile(ctx, q, user, limits),
		signupFile(ctx, q, user, limits),
		commentFile(ctx, q, user, limits),
	}, nil
}

func (accountData) Erase() []userdata.Step {
	return []userdata.Step{
		{Order: userdata.OrderRSVPs, SQL: `DELETE FROM event_rsvps WHERE user_id = $1`},
		{Order: userdata.OrderSignups, SQL: `DELETE FROM event_signups WHERE user_id = $1`},
		{Order: userdata.OrderEventSeries, SQL: `UPDATE event_series SET created_by = NULL WHERE created_by = $1`},
		{Order: userdata.OrderEventComments, SQL: `DELETE FROM event_comments WHERE user_id = $1`},
		{Order: userdata.OrderEventCommentMutes, SQL: `DELETE FROM event_comment_mutes WHERE user_id = $1`},
		{Order: userdata.OrderEvents, SQL: `UPDATE events SET created_by = NULL WHERE created_by = $1`},
	}
}

func rowsFile[T any](name string, order int, load func() ([]T, bool, error)) userdata.File {
	return userdata.File{
		Name: name, Order: order,
		Build: func() (any, bool, error) {
			rows, cut, err := load()
			if err != nil {
				return nil, false, err
			}
			return userdata.AsList(rows), cut, nil
		},
	}
}

func rsvpFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	type rsvpRow struct {
		EventID   uuid.UUID `json:"event_id" db:"event_id"`
		Response  string    `json:"response" db:"response"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
		UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
	}
	return rowsFile("rsvps.json", userdata.FileRSVPs, func() ([]rsvpRow, bool, error) {
		return userdata.FetchRows[rsvpRow](ctx, q, limits.MaxRows, `SELECT event_id, response, created_at, updated_at
		FROM event_rsvps WHERE user_id = $1 ORDER BY created_at LIMIT $2`, user)
	})
}

func signupFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	type signupRow struct {
		EventID   uuid.UUID `json:"event_id" db:"event_id"`
		Role      string    `json:"role" db:"role"`
		State     string    `json:"state" db:"state"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
	}
	return rowsFile("event_signups.json", userdata.FileEventSignups, func() ([]signupRow, bool, error) {
		return userdata.FetchRows[signupRow](ctx, q, limits.MaxRows, `SELECT s.event_id, r.name AS role, s.state, s.created_at
		FROM event_signups s JOIN event_roles r ON r.id = s.role_id
		WHERE s.user_id = $1 ORDER BY s.created_at LIMIT $2`, user)
	})
}

func commentFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	type commentRow struct {
		EventID   uuid.UUID  `json:"event_id" db:"event_id"`
		Content   string     `json:"content" db:"content"`
		CreatedAt time.Time  `json:"created_at" db:"created_at"`
		EditedAt  *time.Time `json:"edited_at,omitempty" db:"edited_at"`
	}
	return rowsFile("event_comments.json", userdata.FileEventComments, func() ([]commentRow, bool, error) {
		return userdata.FetchRows[commentRow](ctx, q, limits.MaxRows, `SELECT event_id, content, created_at, edited_at
		FROM event_comments WHERE user_id = $1 ORDER BY created_at LIMIT $2`, user)
	})
}
