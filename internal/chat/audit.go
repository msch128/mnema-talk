package chat

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// maxAuditedReplies bounds the thread replies kept with a deleted message.
const maxAuditedReplies = 50

// auditSnapshot is a message as the audit log keeps it before it is deleted
// or edited: the full text, as the operator's audit policy says
// (doc/moderation.md), plus who wrote it and where.
type auditSnapshot struct {
	msg     *Message
	channel string
	replies []uuid.UUID
}

// snapshotMessage reads what the audit log needs to know about a message. A
// failure only costs detail: the action itself must not fail because of it.
func (h *Handler) snapshotMessage(ctx context.Context, id uuid.UUID, withReplies bool) *auditSnapshot {
	if h.Audit == nil {
		return nil
	}
	msg, err := GetMessage(ctx, h.DB, id)
	if err != nil {
		return nil
	}
	s := &auditSnapshot{msg: msg}
	_ = h.DB.QueryRow(ctx, `SELECT name FROM channels WHERE id = $1`, msg.ChannelID).Scan(&s.channel)
	if withReplies && msg.ReplyCount > 0 {
		s.replies = h.fetchAuditedReplyIDs(ctx, id)
	}
	return s
}

func (h *Handler) fetchAuditedReplyIDs(ctx context.Context, parentID uuid.UUID) []uuid.UUID {
	// Ids only: replies are other members' messages, and one that is
	// deleted on its own gets its own entry with its text.
	rows, err := h.DB.Query(ctx, `SELECT id FROM messages WHERE parent_id = $1 ORDER BY created_at LIMIT $2`, parentID, maxAuditedReplies)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var replies []uuid.UUID
	for rows.Next() {
		var rid uuid.UUID
		if err := rows.Scan(&rid); err == nil {
			replies = append(replies, rid)
		}
	}
	return replies
}

func attachmentList(m *Message) []map[string]any {
	out := make([]map[string]any, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		out = append(out, map[string]any{"id": a.ID, "filename": a.OriginalFilename, "mime_type": a.MimeType, "size_bytes": a.SizeBytes})
	}
	return out
}

func (s *auditSnapshot) details() map[string]any {
	m := s.msg
	d := map[string]any{
		"channel_id": m.ChannelID, "channel_name": s.channel,
		"author_id": m.UserID, "author_name": m.Username,
		"content": audit.Content(m.Content), "attachments": attachmentList(m),
		"sent_at": m.CreatedAt,
	}
	if m.ParentID != nil {
		d["parent_id"] = *m.ParentID
	}
	return d
}

// auditDelete records the deletion of a message, by its author or by a
// moderator.
func (h *Handler) auditDelete(ctx context.Context, user *auth.User, id uuid.UUID, s *auditSnapshot, reason string) {
	e := audit.Entry{Action: "message.delete", TargetType: "message", TargetID: id.String(), Details: map[string]any{}}
	if s != nil {
		e.Details = s.details()
		e.Details["moderator"] = s.msg.UserID != user.ID
		e.Details["reply_ids"] = s.replies
		if reason != "" {
			e.Details["reason"] = reason
		}
		e.TargetName = s.channel
		e.ChannelID = &s.msg.ChannelID
	}
	h.Audit.RecordOrLog(ctx, e)
}

// auditEdit records an edit with the text before and after.
func (h *Handler) auditEdit(ctx context.Context, id uuid.UUID, s *auditSnapshot, after string) {
	if s == nil {
		return
	}
	d := s.details()
	delete(d, "content")
	d["before"] = audit.Content(s.msg.Content)
	d["after"] = audit.Content(after)
	h.Audit.RecordOrLog(ctx, audit.Entry{Action: "message.edit", TargetType: "message", TargetID: id.String(), TargetName: s.channel, ChannelID: &s.msg.ChannelID, Details: d})
}

// DeleteMessageRequest is the optional body of DELETE
// /api/channels/{channelID}/messages/{messageID}.
type DeleteMessageRequest struct {
	// Reason is shown to the author when a moderator deletes their message,
	// and kept in the audit log. Up to 200 characters.
	Reason string `json:"reason" maxLength:"200" binding:"optional"`
}

// deleteReason reads the optional reason of a deletion. DELETE normally has
// no body, so an empty one is fine.
func deleteReason(r *http.Request) (string, error) {
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
		return "", nil
	}
	var req DeleteMessageRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return "", err
	}
	return httpx.CleanText("reason", req.Reason, MaxModerationReason, false)
}

// tellAuthor lets the author know that a moderator removed their message.
func (h *Handler) tellAuthor(s *auditSnapshot, by *auth.User, reason string) {
	if s == nil || s.msg.UserID == by.ID || h.Events == nil {
		return
	}
	h.Events.SendToUsers([]uuid.UUID{s.msg.UserID}, "moderation_notice", map[string]any{
		"kind": "message_deleted", "reason": reason, "channel_name": s.channel,
	})
}
