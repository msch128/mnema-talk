package chat

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
)

// This file is where the chat handlers ask the permission model. A Handler
// without Perms (unit tests) imposes no limits; the router always sets it.

// guard is the permission model's request-level checks (chat/core), built from
// the Perms the handler is wired with.
func (h *Handler) guard() core.Guard { return core.Guard{Perms: h.Perms} }

// hiddenMessageCounts counts messages per author over the channels the caller
// may not view; nil when there are none (the stored totals apply). Only the
// hidden channels are scanned: the stored total minus these is what the caller
// may see.
func (h *Handler) hiddenMessageCounts(r *http.Request) (map[uuid.UUID]int64, error) {
	if h.Perms == nil {
		return nil, nil
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return nil, err
	}
	hidden := snap.HiddenChannels(auth.UserFrom(r.Context()).Subject())
	if len(hidden) == 0 {
		return nil, nil
	}
	return MessageCountsByAuthor(r.Context(), h.DB, hidden)
}

// MessageCountByAuthor counts one author's messages inside the channels that
// are part of the public total (the messages in a conversation's call are not,
// see migration 0098; the messages_user_idx index leads with the author).
func MessageCountByAuthor(ctx context.Context, q db.Querier, author uuid.UUID, channels []uuid.UUID) (int64, error) {
	var n int64
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM messages WHERE user_id = $1 AND counted AND channel_id = ANY($2::uuid[])`, author, channels).Scan(&n); err != nil {
		return 0, fmt.Errorf("count hidden messages: %w", err)
	}
	return n, nil
}

// MessageCountsByAuthor counts the messages per author inside the channels,
// as MessageCountByAuthor does.
func MessageCountsByAuthor(ctx context.Context, q db.Querier, channels []uuid.UUID) (map[uuid.UUID]int64, error) {
	rows, err := q.Query(ctx, `SELECT user_id, COUNT(*) FROM messages WHERE counted AND channel_id = ANY($1::uuid[]) GROUP BY user_id`, channels)
	if err != nil {
		return nil, fmt.Errorf("count hidden messages: %w", err)
	}
	type count struct {
		User uuid.UUID
		N    int64
	}
	found, err := pgx.CollectRows(rows, pgx.RowToStructByPos[count])
	if err != nil {
		return nil, fmt.Errorf("count hidden messages: %w", err)
	}
	out := make(map[uuid.UUID]int64, len(found))
	for _, c := range found {
		out[c.User] = c.N
	}
	return out, nil
}
