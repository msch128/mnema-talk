package stage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// SetStage turns a permanent Talk into a Stage or back into a normal Talk
// (doc/talks.md). Lobbies and temporary Talks cannot be Stages.
func SetStage(ctx context.Context, p db.Querier, id uuid.UUID, stage bool) error {
	tag, err := p.Exec(ctx, `
		UPDATE channels SET is_stage = $2
		WHERE id = $1 AND type = 'voice' AND NOT is_temp AND ($2 = FALSE OR NOT is_lobby)`, id, stage)
	if err != nil {
		return fmt.Errorf("set stage: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// A failed lookup, including a missing row, is "not found". An existing
		// channel that the update skipped is a text channel, a lobby or a
		// temporary Talk.
		var one int
		if qerr := p.QueryRow(ctx, `SELECT 1 FROM channels WHERE id = $1`, id).Scan(&one); qerr != nil {
			return httpx.ErrNotFound("channel not found")
		}
		return httpx.ErrInvalidInput("only a permanent Talk that is no lobby can be a Stage")
	}
	return nil
}
