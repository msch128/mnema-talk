package core

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// MaxSlowModeSeconds is the longest slow mode: 24 hours.
const MaxSlowModeSeconds = 86400

// MaxUnansweredDMMessages is how many messages the member who started a
// conversation may send before anybody else has written in it.
const MaxUnansweredDMMessages = 2

// errDMSend is the refusal of a direct message the rules do not allow. Whatever
// the reason, the answer is the same text (see dm.go), so none can be probed.
var ErrDMSend = httpx.ErrForbidden("Couldn't send this message.")

// errPostTitleRequired answers a forum post without a title.
var ErrPostTitleRequired = httpx.ErrInvalidInput("title is required for forum posts")

// DMMemberIDs lists the members of a conversation (empty if there is none).
func DMMemberIDs(ctx context.Context, q db.Querier, id uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT user_id FROM dm_members WHERE channel_id = $1 ORDER BY joined_at, user_id`, id)
	if err != nil {
		return nil, fmt.Errorf("direct message members: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
