package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Message moderation limits (doc/moderation.md).
const (
	// MaxBulkDelete is the most messages one bulk delete removes.
	MaxBulkDelete = 100
	// MaxModerationReason is the longest reason, in characters.
	MaxModerationReason = 200
)

func (h *Handler) mountModeration(r chi.Router) {
	h.mountPins(r)
	r.Post("/channels/{channelID}/messages/bulk-delete", httpx.Handle(h.bulkDelete))
}

// asModerator runs fn in a transaction under the permission lock after
// checking, on the state inside that transaction, that the caller may view
// the channel and manage its messages. The check and the write cannot be
// separated by a concurrent role or overwrite change.
func (h *Handler) asModerator(r *http.Request, channelID uuid.UUID, fn func(tx pgx.Tx, snap *perm.Snapshot) error) error {
	if h.Perms == nil {
		return httpx.ErrForbidden("moderation needs the permission service")
	}
	ctx := r.Context()
	actor := auth.UserFrom(ctx)
	return h.Perms.Locked(ctx, actor.Subject(), func(tx pgx.Tx, snap *perm.Snapshot) error {
		if !snap.KnowsChannel(channelID) && !snap.IsOwner(actor.Subject()) {
			return perm.ErrChannelHidden
		}
		if err := perm.CheckChannel(snap.Channel(actor.Subject(), channelID), perm.ViewChannel|perm.ManageMessages); err != nil {
			return err
		}
		return fn(tx, snap)
	})
}

// BulkDeleteRequest is the body of POST /api/channels/{channelID}/messages/bulk-delete.
type BulkDeleteRequest struct {
	// UserID is the member whose latest messages in the channel go.
	UserID uuid.UUID `json:"user_id" format:"uuid"`
	// Count is how many of their latest messages (replies included), 1 to 100.
	Count int `json:"count" minimum:"1" maximum:"100"`
	// Reason is kept in the audit log. Optional.
	Reason string `json:"reason" maxLength:"200" binding:"optional"`
}

// BulkDeleteResult says how many messages were deleted.
type BulkDeleteResult struct {
	Deleted int `json:"deleted"`
}

// deletedMessage is what the audit log keeps of a message a bulk delete took.
type deletedMessage struct {
	id          uuid.UUID
	parentID    *uuid.UUID
	content     string
	createdAt   time.Time
	attachments []map[string]any
	edits       []map[string]any
	// replyIDs are the thread replies of other members that go with it.
	replyIDs []uuid.UUID
}

func parseBulkDeleteRequest(r *http.Request) (uuid.UUID, BulkDeleteRequest, string, error) {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return uuid.Nil, BulkDeleteRequest{}, "", err
	}
	var req BulkDeleteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return uuid.Nil, BulkDeleteRequest{}, "", err
	}
	if req.Count < 1 || req.Count > MaxBulkDelete {
		return uuid.Nil, BulkDeleteRequest{}, "", httpx.ErrInvalidInput(fmt.Sprintf("count must be between 1 and %d", MaxBulkDelete))
	}
	reason, err := httpx.CleanText("reason", req.Reason, MaxModerationReason, false)
	if err != nil {
		return uuid.Nil, BulkDeleteRequest{}, "", err
	}
	return chID, req, reason, nil
}

func verifyBulkDeleteTarget(ctx context.Context, tx pgx.Tx, snap *perm.Snapshot, actor auth.User, targetID uuid.UUID) (string, error) {
	var targetName string
	if err := tx.QueryRow(ctx, `SELECT username FROM users WHERE id = $1 AND NOT is_bot`, targetID).Scan(&targetName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", httpx.ErrNotFound("user not found")
		}
		return "", err
	}
	if err := snap.CanModerate(actor.Subject(), perm.Subject{ID: targetID}); err != nil {
		return "", err
	}
	return targetName, nil
}

// bulkDelete handles POST /api/channels/{channelID}/messages/bulk-delete.
//
// @Summary Delete a member's latest messages in a channel
// @Description Deletes up to 100 of the member's latest messages in the channel, their thread replies and attachments included, in one step. Needs the Manage messages permission in the channel and a higher role than the member; the Owner's messages can only go by the Owner's hand. Every message gets its own audit entry with its text, plus one summary entry. The member is told through a moderation_notice event. Broadcasts message_delete per message.
// @ID bulkDeleteMessages
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body BulkDeleteRequest true "Request body."
// @Success 200 {object} BulkDeleteResult "How many messages went."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage messages permission in the channel, or the member's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/bulk-delete [post]
func (h *Handler) bulkDelete(w http.ResponseWriter, r *http.Request) error {
	chID, req, reason, err := parseBulkDeleteRequest(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	actor := auth.UserFrom(ctx)
	var gone []deletedMessage
	var keys []string
	var channelName, targetName string
	err = h.asModerator(r, chID, func(tx pgx.Tx, snap *perm.Snapshot) error {
		var err error
		if targetName, err = verifyBulkDeleteTarget(ctx, tx, snap, *actor, req.UserID); err != nil {
			return err
		}
		_ = tx.QueryRow(ctx, `SELECT name FROM channels WHERE id = $1`, chID).Scan(&channelName)
		gone, keys, err = takeLatestMessages(ctx, tx, chID, req.UserID, req.Count, snap.IsOwner(actor.Subject()))
		return err
	})
	if err != nil {
		return err
	}
	h.deleteObjects(ctx, keys)
	h.announceBulkDelete(ctx, bulkDeleteReport{
		chID: chID, channelName: channelName, targetName: targetName, actor: actor,
		req: req, reason: reason, gone: gone,
	})
	httpx.WriteJSON(w, http.StatusOK, BulkDeleteResult{Deleted: len(gone)})
	return nil
}

// bulkDeleteReport is what a finished bulk delete tells the live clients,
// the audit log and the member whose messages went.
type bulkDeleteReport struct {
	chID        uuid.UUID
	channelName string
	targetName  string
	actor       *auth.User
	req         BulkDeleteRequest
	reason      string
	gone        []deletedMessage
}

func (h *Handler) announceBulkDelete(ctx context.Context, rep bulkDeleteReport) {
	chID, req := rep.chID, rep.req
	for _, m := range rep.gone {
		h.Events.BroadcastChannel(chID, events.MessageDelete, map[string]any{"id": m.id, "channel_id": chID, "parent_id": m.parentID})
		d := map[string]any{
			"channel_id": chID, "channel_name": rep.channelName, "author_id": req.UserID, "author_name": rep.targetName,
			"content": audit.Content(m.content), "attachments": m.attachments, "sent_at": m.createdAt,
			"moderator": req.UserID != rep.actor.ID, "bulk": true, "reason": rep.reason,
		}
		if m.parentID != nil {
			d["parent_id"] = *m.parentID
		}
		if len(m.edits) > 0 {
			d["edit_versions"] = m.edits
		}
		if len(m.replyIDs) > 0 {
			d["reply_ids"] = m.replyIDs
		}
		h.Audit.RecordOrLog(ctx, audit.Entry{Action: "message.delete", TargetType: "message", TargetID: m.id.String(), TargetName: rep.channelName, ChannelID: &chID, Details: d})
	}
	h.Audit.RecordOrLog(ctx, audit.Entry{
		Action: "moderation.bulk_delete", TargetType: "user", TargetID: req.UserID.String(), TargetName: rep.targetName, ChannelID: &chID,
		Details: map[string]any{"channel_id": chID, "channel_name": rep.channelName, "requested": req.Count, "deleted": len(rep.gone), "reason": rep.reason},
	})
	if len(rep.gone) > 0 && req.UserID != rep.actor.ID {
		h.Events.SendToUsers([]uuid.UUID{req.UserID}, events.ModerationNotice, map[string]any{
			"kind": "messages_deleted", "reason": rep.reason, "channel_name": rep.channelName, "count": len(rep.gone),
		})
	}
}

func queryLatestMessages(ctx context.Context, tx pgx.Tx, chID, userID uuid.UUID, n int, byOwner bool) ([]deletedMessage, error) {
	rows, err := tx.Query(ctx, `
		SELECT m.id, m.parent_id, m.content, m.created_at FROM messages m
		WHERE m.channel_id = $1 AND m.user_id = $2
		  AND ($4 OR NOT EXISTS (SELECT 1 FROM messages r JOIN users ru ON ru.id = r.user_id
		                         WHERE r.parent_id = m.id AND ru.role = 'admin'))
		ORDER BY m.created_at DESC, m.id DESC LIMIT $3 FOR UPDATE OF m`, chID, userID, n, byOwner)
	if err != nil {
		return nil, fmt.Errorf("select messages: %w", err)
	}
	gone, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (deletedMessage, error) {
		var m deletedMessage
		err := row.Scan(&m.id, &m.parentID, &m.content, &m.createdAt)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("select messages: %w", err)
	}
	return gone, nil
}

func attachDeletedMedia(gone []deletedMessage, byMessage map[uuid.UUID][]map[string]any) {
	for i := range gone {
		gone[i].attachments = byMessage[gone[i].id]
		if gone[i].attachments == nil {
			gone[i].attachments = []map[string]any{}
		}
	}
}

// takeLatestMessages deletes the n latest messages of a member in a channel
// inside tx and returns what the audit log needs of them and the storage keys
// of their attachments (and those of the replies that go with them). Messages
// with a reply of the Owner stay unless the Owner asks.
func takeLatestMessages(ctx context.Context, tx pgx.Tx, chID, userID uuid.UUID, n int, byOwner bool) ([]deletedMessage, []string, error) {
	gone, err := queryLatestMessages(ctx, tx, chID, userID, n, byOwner)
	if err != nil || len(gone) == 0 {
		return gone, nil, err
	}
	ids := make([]uuid.UUID, len(gone))
	for i, m := range gone {
		ids[i] = m.id
	}
	byMessage, keys, err := loadDeletedAttachments(ctx, tx, ids)
	if err != nil {
		return nil, nil, err
	}
	attachDeletedMedia(gone, byMessage)
	versions := editVersionsForAuditDetail(ctx, tx, ids)
	replies := replyIDsForAudit(ctx, tx, ids)
	for i := range gone {
		gone[i].edits = versions[gone[i].id]
		gone[i].replyIDs = replies[gone[i].id]
	}
	if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE id = ANY($1)`, ids); err != nil {
		return nil, nil, fmt.Errorf("delete messages: %w", err)
	}
	return gone, keys, nil
}

// loadDeletedAttachments returns the attachments of the messages ids (and of
// their replies) grouped by message, and all their storage keys.
func loadDeletedAttachments(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID][]map[string]any, []string, error) {
	media, err := tx.Query(ctx, `
		SELECT message_id, id, original_filename, mime_type, size_bytes, s3_key FROM media
		WHERE message_id = ANY($1) OR message_id IN (SELECT id FROM messages WHERE parent_id = ANY($1))`, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("select attachments: %w", err)
	}
	defer media.Close()
	var keys []string
	byMessage := map[uuid.UUID][]map[string]any{}
	for media.Next() {
		var msgID, id uuid.UUID
		var name, mime, key string
		var size int64
		if err := media.Scan(&msgID, &id, &name, &mime, &size, &key); err != nil {
			return nil, nil, err
		}
		keys = append(keys, key)
		byMessage[msgID] = append(byMessage[msgID], map[string]any{"id": id, "filename": name, "mime_type": mime, "size_bytes": size})
	}
	if err := media.Err(); err != nil {
		return nil, nil, err
	}
	media.Close()
	return byMessage, keys, nil
}
