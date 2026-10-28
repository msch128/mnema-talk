package chat

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
		if _, lerr := LoadChannel(ctx, p, id); lerr == nil {
			return httpx.ErrInvalidInput("only a permanent Talk that is no lobby can be a Stage")
		}
		return errChannelNotFound
	}
	return nil
}
