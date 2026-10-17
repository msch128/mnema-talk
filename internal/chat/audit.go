package chat

import (
	"context"

	"github.com/google/uuid"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
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
		// Ids only: replies are other members' messages, and one that is
		// deleted on its own gets its own entry with its text.
		rows, err := h.DB.Query(ctx, `SELECT id FROM messages WHERE parent_id = $1 ORDER BY created_at LIMIT $2`, id, maxAuditedReplies)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var rid uuid.UUID
				if rows.Scan(&rid) == nil {
					s.replies = append(s.replies, rid)
				}
			}
		}
	}
	return s
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
func (h *Handler) auditDelete(ctx context.Context, user *auth.User, id uuid.UUID, s *auditSnapshot) {
	e := audit.Entry{Action: "message.delete", TargetType: "message", TargetID: id.String(), Details: map[string]any{}}
	if s != nil {
		e.Details = s.details()
		e.Details["moderator"] = s.msg.UserID != user.ID
		e.Details["reply_ids"] = s.replies
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
