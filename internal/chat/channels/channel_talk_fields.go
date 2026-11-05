package channels

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// SetLobby marks a voice channel as a lobby or clears the mark. Temporary
// Talks cannot be lobbies.
func SetLobby(ctx context.Context, p db.Querier, id uuid.UUID, lobby bool) error {
	tag, err := p.Exec(ctx, `UPDATE channels SET is_lobby = $2 WHERE id = $1 AND type = 'voice' AND NOT is_temp AND ($2 = FALSE OR NOT is_stage)`, id, lobby)
	if err != nil {
		return fmt.Errorf("set lobby: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, lerr := core.LoadChannel(ctx, p, id); lerr == nil {
			return httpx.ErrInvalidInput("only a permanent Talk that is no Stage can be a lobby")
		}
		return core.ErrChannelNotFound
	}
	return nil
}

// FillTalkFields loads the lobby/temporary-Talk fields of ch, which the
// create and update statements do not return.
func FillTalkFields(ctx context.Context, p *db.Pool, ch *core.Channel) error {
	err := p.QueryRow(ctx, `SELECT is_lobby, is_temp, leader_id, is_stage FROM channels WHERE id = $1`, ch.ID).Scan(&ch.IsLobby, &ch.IsTemp, &ch.LeaderID, &ch.IsStage)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.ErrChannelNotFound
	}
	if err != nil {
		return fmt.Errorf("load talk fields: %w", err)
	}
	return nil
}
