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
	"github.com/msch128/mnema-talk/internal/perm"
)

// This file is where the chat handlers ask the permission model. A Handler
// without Perms (unit tests) imposes no limits; the router always sets it.

// guard is the permission model's request-level checks (chat/core), built from
// the Perms the handler is wired with.
func (h *Handler) guard() core.Guard { return core.Guard{Perms: h.Perms} }

// require checks that the caller holds need in channel ch. Without
// ViewChannel the channel does not exist for them (404).
func (h *Handler) require(r *http.Request, ch uuid.UUID, need perm.Set) error {
	return h.guard().Require(r, ch, need)
}

// channelPerms is what the caller may do in channel ch.
func (h *Handler) channelPerms(r *http.Request, ch uuid.UUID) (perm.Set, error) {
	return h.guard().ChannelPerms(r, ch)
}

// refusalFor turns a "missing permission" refusal into RULES_NOT_ACCEPTED when
// the caller's rules acceptance is what took the permission away.
func (h *Handler) refusalFor(r *http.Request, err error, need perm.Set) error {
	return h.guard().RefusalFor(r, err, need)
}

// viewableChannels lists the channels the caller may view; nil means all.
func (h *Handler) viewableChannels(r *http.Request) ([]uuid.UUID, error) {
	return h.guard().ViewableChannels(r)
}

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

// MessageCountsByAuthor counts the messages per author inside the channels.
func MessageCountsByAuthor(ctx context.Context, q db.Querier, channels []uuid.UUID) (map[uuid.UUID]int64, error) {
	rows, err := q.Query(ctx, `SELECT user_id, COUNT(*) FROM messages WHERE channel_id = ANY($1::uuid[]) GROUP BY user_id`, channels)
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

// requireCategory checks that the caller may see a category (a category that
// does not exist is left to the operation itself to report).
func (h *Handler) requireCategory(r *http.Request, id uuid.UUID) error {
	return h.guard().RequireCategory(r, id)
}
