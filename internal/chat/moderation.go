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
	r.Put("/channels/{channelID}/messages/{messageID}/pin", httpx.Handle(h.pinMessage))
	r.Delete("/channels/{channelID}/messages/{messageID}/pin", httpx.Handle(h.unpinMessage))
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

// pinMessage handles PUT /api/channels/{channelID}/messages/{messageID}/pin.
//
// @Summary Pin a message
// @Description Needs the Manage messages permission in the channel. Broadcasts message_update.
// @ID pinMessage
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Success 200 {object} Message "The message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID}/pin [put]
func (h *Handler) pinMessage(w http.ResponseWriter, r *http.Request) error {
	return h.setPinned(w, r, true)
}

// unpinMessage handles DELETE /api/channels/{channelID}/messages/{messageID}/pin.
//
// @Summary Unpin a message
// @Description Needs the Manage messages permission in the channel. Broadcasts message_update.
// @ID unpinMessage
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Success 200 {object} Message "The message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID}/pin [delete]
func (h *Handler) unpinMessage(w http.ResponseWriter, r *http.Request) error {
	return h.setPinned(w, r, false)
}

func (h *Handler) setPinned(w http.ResponseWriter, r *http.Request, pinned bool) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	err = h.asModerator(r, chID, func(tx pgx.Tx, _ *perm.Snapshot) error {
		tag, err := tx.Exec(r.Context(), `UPDATE messages SET is_pinned = $3 WHERE id = $1 AND channel_id = $2`, msgID, chID, pinned)
		if err != nil {
			return fmt.Errorf("pin message: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return errMessageNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}
	msg, err := GetMessage(r.Context(), h.DB, msgID)
	if err != nil {
		return err
	}
	action := "message.unpin"
	if pinned {
		action = "message.pin"
	}
	h.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: action, TargetType: "message", TargetID: msgID.String(), ChannelID: &chID,
		Details: map[string]any{"channel_id": chID, "author_id": msg.UserID, "author_name": msg.Username, "content": audit.Content(msg.Content)},
	})
	h.Events.BroadcastChannel(chID, "message_update", msg)
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/bulk-delete [post]
func (h *Handler) bulkDelete(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	var req BulkDeleteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Count < 1 || req.Count > MaxBulkDelete {
		return httpx.ErrInvalidInput(fmt.Sprintf("count must be between 1 and %d", MaxBulkDelete))
	}
	reason, err := httpx.CleanText("reason", req.Reason, MaxModerationReason, false)
	if err != nil {
		return err
	}
	ctx := r.Context()
	actor := auth.UserFrom(ctx)
	var gone []deletedMessage
	var keys []string
	var channelName, targetName string
	err = h.asModerator(r, chID, func(tx pgx.Tx, snap *perm.Snapshot) error {
		if err := tx.QueryRow(ctx, `SELECT username FROM users WHERE id = $1 AND NOT is_bot`, req.UserID).Scan(&targetName); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound("user not found")
			}
			return err
		}
		if err := snap.CanModerate(actor.Subject(), perm.Subject{ID: req.UserID}); err != nil {
			return err
		}
		_ = tx.QueryRow(ctx, `SELECT name FROM channels WHERE id = $1`, chID).Scan(&channelName)
		var err error
		gone, keys, err = takeLatestMessages(ctx, tx, chID, req.UserID, req.Count, snap.IsOwner(actor.Subject()))
		return err
	})
	if err != nil {
		return err
	}
	h.deleteObjects(ctx, keys)
	for _, m := range gone {
		h.Events.BroadcastChannel(chID, "message_delete", map[string]any{"id": m.id, "channel_id": chID, "parent_id": m.parentID})
		d := map[string]any{
			"channel_id": chID, "channel_name": channelName, "author_id": req.UserID, "author_name": targetName,
			"content": audit.Content(m.content), "attachments": m.attachments, "sent_at": m.createdAt,
			"moderator": req.UserID != actor.ID, "bulk": true, "reason": reason,
		}
		if m.parentID != nil {
			d["parent_id"] = *m.parentID
		}
		h.Audit.RecordOrLog(ctx, audit.Entry{Action: "message.delete", TargetType: "message", TargetID: m.id.String(), TargetName: channelName, ChannelID: &chID, Details: d})
	}
	h.Audit.RecordOrLog(ctx, audit.Entry{
		Action: "moderation.bulk_delete", TargetType: "user", TargetID: req.UserID.String(), TargetName: targetName, ChannelID: &chID,
		Details: map[string]any{"channel_id": chID, "channel_name": channelName, "requested": req.Count, "deleted": len(gone), "reason": reason},
	})
	if len(gone) > 0 && req.UserID != actor.ID && h.Events != nil {
		h.Events.SendToUsers([]uuid.UUID{req.UserID}, "moderation_notice", map[string]any{
			"kind": "messages_deleted", "reason": reason, "channel_name": channelName, "count": len(gone),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, BulkDeleteResult{Deleted: len(gone)})
	return nil
}

// takeLatestMessages deletes the n latest messages of a member in a channel
// inside tx and returns what the audit log needs of them and the storage keys
// of their attachments (and those of the replies that go with them). Messages
// with a reply of the Owner stay unless the Owner asks.
func takeLatestMessages(ctx context.Context, tx pgx.Tx, chID, userID uuid.UUID, n int, byOwner bool) ([]deletedMessage, []string, error) {
	rows, err := tx.Query(ctx, `
		SELECT m.id, m.parent_id, m.content, m.created_at FROM messages m
		WHERE m.channel_id = $1 AND m.user_id = $2
		  AND ($4 OR NOT EXISTS (SELECT 1 FROM messages r JOIN users ru ON ru.id = r.user_id
		                         WHERE r.parent_id = m.id AND ru.role = 'admin'))
		ORDER BY m.created_at DESC, m.id DESC LIMIT $3 FOR UPDATE OF m`, chID, userID, n, byOwner)
	if err != nil {
		return nil, nil, fmt.Errorf("select messages: %w", err)
	}
	gone, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (deletedMessage, error) {
		var m deletedMessage
		err := row.Scan(&m.id, &m.parentID, &m.content, &m.createdAt)
		return m, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("select messages: %w", err)
	}
	ids := make([]uuid.UUID, len(gone))
	for i, m := range gone {
		ids[i] = m.id
	}
	var keys []string
	if len(ids) > 0 {
		media, err := tx.Query(ctx, `
			SELECT message_id, id, original_filename, mime_type, size_bytes, s3_key FROM media
			WHERE message_id = ANY($1) OR message_id IN (SELECT id FROM messages WHERE parent_id = ANY($1))`, ids)
		if err != nil {
			return nil, nil, fmt.Errorf("select attachments: %w", err)
		}
		defer media.Close()
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
		for i := range gone {
			gone[i].attachments = byMessage[gone[i].id]
			if gone[i].attachments == nil {
				gone[i].attachments = []map[string]any{}
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE id = ANY($1)`, ids); err != nil {
			return nil, nil, fmt.Errorf("delete messages: %w", err)
		}
	}
	return gone, keys, nil
}
