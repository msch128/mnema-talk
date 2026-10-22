package chat

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// MaxEditVersions is how many replaced versions of one message are kept; the
// oldest go first.
const MaxEditVersions = 50

// EditHistoryRate is how many history reads one member may make per minute.
const EditHistoryRate = 60

// recordReplacedVersion keeps the text an edit is about to replace and drops
// the oldest versions beyond MaxEditVersions. It runs in the edit's
// transaction, under the message row lock, so versions are never lost or
// doubled by concurrent edits.
func recordReplacedVersion(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO message_edits (message_id, editor_id, content, edited_at)
		SELECT id, user_id, content, updated_at FROM messages WHERE id = $1`, messageID); err != nil {
		return fmt.Errorf("keep replaced version: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM message_edits WHERE message_id = $1 AND id NOT IN
			(SELECT id FROM message_edits WHERE message_id = $1 ORDER BY id DESC LIMIT $2)`,
		messageID, MaxEditVersions); err != nil {
		return fmt.Errorf("trim edit history: %w", err)
	}
	return nil
}

// MessageEdit is one earlier version of a message.
type MessageEdit struct {
	ID int64 `json:"id"`
	// Content is the text of this earlier version.
	Content string `json:"content"`
	// EditedAt is when this version was written (the message's creation time
	// for the original).
	EditedAt time.Time `json:"edited_at" format:"date-time"`
	// ReplacedAt is when an edit replaced it.
	ReplacedAt time.Time `json:"replaced_at" format:"date-time"`
	// EditorID wrote this version.
	EditorID uuid.UUID `json:"editor_id" format:"uuid"`
}

// MessageEditHistory is the earlier versions of a message, newest first.
type MessageEditHistory struct {
	Edits []MessageEdit `json:"edits"`
}

func loadEditHistory(ctx context.Context, q db.Querier, messageID uuid.UUID) (*MessageEditHistory, error) {
	rows, err := q.Query(ctx, `
		SELECT id, content, edited_at, replaced_at, editor_id
		FROM message_edits WHERE message_id = $1 ORDER BY id DESC LIMIT $2`, messageID, MaxEditVersions)
	if err != nil {
		return nil, fmt.Errorf("load edit history: %w", err)
	}
	edits, err := pgx.CollectRows(rows, pgx.RowToStructByPos[MessageEdit])
	if err != nil {
		return nil, fmt.Errorf("load edit history: %w", err)
	}
	if edits == nil {
		edits = []MessageEdit{}
	}
	return &MessageEditHistory{Edits: edits}, nil
}

// getEditHistory handles GET /api/channels/{channelID}/messages/{messageID}/edits.
//
// @Summary Edit history of a message
// @Description The earlier versions of an edited message, newest first (at most 50). Needs the Manage messages permission in the channel (the Owner always); the author and other members cannot read it. Not searched and not part of any export except the author's own. Rate limited per member. An unedited message has an empty list.
// @ID getMessageEditHistory
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Success 200 {object} MessageEditHistory "Earlier versions, newest first."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID}/edits [get]
func (h *Handler) getEditHistory(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	// Permissions first: a member who cannot see the channel learns nothing
	// about which messages exist in it.
	if h.Perms == nil {
		return httpx.ErrForbidden("edit history needs the permission service")
	}
	p, err := h.channelPerms(r, chID)
	if err != nil {
		return err
	}
	if err := perm.CheckChannel(p, perm.ViewChannel|perm.ManageMessages); err != nil {
		return err
	}
	msgID, _, err := h.messageInChannel(r)
	if err != nil {
		return err
	}
	history, err := loadEditHistory(r.Context(), h.DB, msgID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, history)
	return nil
}

// PruneEditHistory deletes versions replaced more than days ago and returns
// how many went.
func PruneEditHistory(ctx context.Context, p *db.Pool, days int) (int64, error) {
	tag, err := p.Exec(ctx, `DELETE FROM message_edits WHERE replaced_at < NOW() - make_interval(days => $1)`, days)
	if err != nil {
		return 0, fmt.Errorf("prune edit history: %w", err)
	}
	return tag.RowsAffected(), nil
}

// RunEditHistoryRetention prunes every interval until ctx ends. With days <= 0
// it returns at once: nothing is deleted automatically.
func RunEditHistoryRetention(ctx context.Context, p *db.Pool, days int, every time.Duration) {
	if days <= 0 {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if n, err := PruneEditHistory(ctx, p, days); err != nil {
			slog.Error("edit history retention failed", "err", err)
		} else if n > 0 {
			slog.Info("edit history retention", "deleted", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
