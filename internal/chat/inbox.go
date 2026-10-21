package chat

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	inboxDefaultLimit = 25
	inboxMaxLimit     = 50
)

// MentionsPage is one page of the mentions inbox.
type MentionsPage struct {
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"has_more"`
}

// InboxQuery selects a page of the messages that mention user or reply to one
// of their messages, newest first.
type InboxQuery struct {
	User   uuid.UUID
	Before *uuid.UUID
	Limit  int
	// Visible limits the inbox to these channels; nil means all of them (an
	// empty, non-nil list finds nothing).
	Visible []uuid.UUID
}

// Mentions returns one page of the caller's mentions inbox and whether older
// entries exist. Messages by the user themselves never appear.
func Mentions(ctx context.Context, p *db.Pool, q InboxQuery) ([]Message, bool, error) {
	args := []any{q.User, q.Limit + 1}
	sql := messageSelect + `
		WHERE m.user_id <> $1
		  AND (rm.user_id = $1 OR EXISTS (SELECT 1 FROM message_mentions mm WHERE mm.message_id = m.id AND mm.user_id = $1))`
	if q.Visible != nil {
		args = append(args, q.Visible)
		sql += fmt.Sprintf(" AND m.channel_id = ANY($%d::uuid[])", len(args))
	}
	if q.Before != nil {
		args = append(args, *q.Before)
		sql += fmt.Sprintf(" AND (m.created_at, m.id) < (SELECT created_at, id FROM messages WHERE id = $%d)", len(args))
	}
	sql += " ORDER BY m.created_at DESC, m.id DESC LIMIT $2"
	msgs, err := queryMessages(ctx, p, sql, args...)
	if err != nil {
		return nil, false, err
	}
	more := len(msgs) > q.Limit
	if more {
		msgs = msgs[:q.Limit]
	}
	return msgs, more, nil
}

// mentionsInbox handles GET /api/mentions.
//
// @Summary Mentions inbox
// @Description Messages that mention the caller (by name, @all, @here or through a role) or reply to one of their messages, newest first. Only channels the caller may view are included; the caller's own messages never are.
// @ID listMentions
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param before query string false "Page back: only messages older than this message." Format(uuid)
// @Param limit query int false "Page size. Values below 1 or non-numeric fall back to the default; larger values are clamped to 50." minimum(1) maximum(50) default(25)
// @Success 200 {object} MentionsPage "One page of mentions."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/mentions [get]
func (h *Handler) mentionsInbox(w http.ResponseWriter, r *http.Request) error {
	q := InboxQuery{User: auth.UserFrom(r.Context()).ID, Limit: httpx.QueryLimit(r, "limit", inboxDefaultLimit, inboxMaxLimit)}
	if _, err := queryIDs(r, "an id", map[string]**uuid.UUID{"before": &q.Before}); err != nil {
		return err
	}
	var err error
	if q.Visible, err = h.viewableChannels(r); err != nil {
		return err
	}
	msgs, more, err := Mentions(r.Context(), h.DB, q)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, MentionsPage{Messages: msgs, HasMore: more})
	return nil
}
