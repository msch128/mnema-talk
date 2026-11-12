package messages

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// maxAuditedReplies bounds the thread replies kept with a deleted message.
const maxAuditedReplies = 50

// auditSnapshot is a message as the audit log keeps it before it is deleted
// or edited: the full text, as the operator's audit policy says
// (doc/moderation.md), plus who wrote it and where.
type auditSnapshot struct {
	// dm marks a message of a direct message conversation: nothing of it is
	// ever audited (doc/moderation.md).
	dm      bool
	msg     *core.Message
	channel string
	replies []uuid.UUID
	// edits are the replaced versions of an edited message, kept because
	// message_edits cascades with the message.
	edits []map[string]any
}

// snapshotMessage reads, through q, what the audit log needs to know about a
// message. Callers read it in the transaction of the change, so a failure
// fails the change too: a change is never left without its entry. Only the
// reply ids and edit versions are detail whose failure is tolerated.
func (a *API) snapshotMessage(ctx context.Context, q db.Querier, id uuid.UUID, withReplies bool) (*auditSnapshot, error) {
	if a.Audit == nil {
		return nil, nil
	}
	msg, err := GetMessage(ctx, q, id)
	if err != nil {
		return nil, err
	}
	s := &auditSnapshot{msg: msg}
	// Fail closed: when the channel cannot be read, a direct message must not
	// end up in the log with its text.
	if s.channel, s.dm, err = channelAuditName(ctx, q, msg.ChannelID); err != nil {
		return nil, err
	}
	if s.dm {
		return &auditSnapshot{dm: true, msg: &core.Message{UserID: msg.UserID}}, nil
	}
	if withReplies && msg.ReplyCount > 0 {
		s.replies = ReplyIDsForAudit(ctx, q, []uuid.UUID{id})[id]
	}
	if withReplies && msg.IsEdited {
		s.edits = editVersionsForAuditDetail(ctx, q, []uuid.UUID{id})[id]
	}
	return s, nil
}

// ReplyIDsForAudit reads, per message, the ids of its first maxAuditedReplies
// thread replies. Ids only: replies are other members' messages, and one that
// is deleted on its own gets its own entry with its text. A failure costs
// detail only: the deletion must not fail, so inside a transaction the read
// runs in a savepoint (a failed statement would abort the whole transaction).
func ReplyIDsForAudit(ctx context.Context, q db.Querier, parentIDs []uuid.UUID) map[uuid.UUID][]uuid.UUID {
	var out map[uuid.UUID][]uuid.UUID
	read := func(q db.Querier) (err error) {
		out, err = readReplyIDs(ctx, q, parentIDs)
		return err
	}
	var err error
	if tx, ok := q.(pgx.Tx); ok {
		err = pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error { return read(sp) })
	} else {
		err = read(q)
	}
	if err != nil {
		slog.Warn("audit: read replies", "err", err)
		return map[uuid.UUID][]uuid.UUID{}
	}
	return out
}

func readReplyIDs(ctx context.Context, q db.Querier, parentIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT parent_id, id FROM (
			SELECT parent_id, id, row_number() OVER (PARTITION BY parent_id ORDER BY created_at, id) AS rn
			FROM messages WHERE parent_id = ANY($1)) v
		WHERE rn <= $2 ORDER BY parent_id, rn`, parentIDs, maxAuditedReplies)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]uuid.UUID{}
	for rows.Next() {
		var parent, reply uuid.UUID
		if err := rows.Scan(&parent, &reply); err != nil {
			return nil, err
		}
		out[parent] = append(out[parent], reply)
	}
	return out, rows.Err()
}

func attachmentList(m *core.Message) []map[string]any {
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
// moderator, through tx, the transaction of the deletion. It returns the
// snapshot for the notice to the author.
func (a *API) auditDelete(ctx context.Context, tx pgx.Tx, user *auth.User, id uuid.UUID, reason string) (*auditSnapshot, error) {
	s, err := a.snapshotMessage(ctx, tx, id, true)
	if err != nil || s == nil || s.dm {
		return s, err
	}
	e := audit.Entry{Action: "message.delete", TargetType: "message", TargetID: id.String(), TargetName: s.channel, ChannelID: &s.msg.ChannelID}
	e.Details = s.details()
	e.Details["moderator"] = s.msg.UserID != user.ID
	e.Details["reply_ids"] = s.replies
	if len(s.edits) > 0 {
		e.Details["edit_versions"] = s.edits
	}
	if reason != "" {
		e.Details["reason"] = reason
	}
	return s, a.Audit.RecordTx(ctx, tx, e)
}

// auditEdit records an edit with the text before and after, through tx, the
// transaction of the edit, after the new text is stored: the text before is
// the version the edit just kept in message_edits.
func (a *API) auditEdit(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	s, err := a.snapshotMessage(ctx, tx, id, false)
	if err != nil || s == nil || s.dm {
		return err
	}
	var before string
	if err := tx.QueryRow(ctx, `SELECT content FROM message_edits WHERE message_id = $1 ORDER BY id DESC LIMIT 1`, id).Scan(&before); err != nil {
		return fmt.Errorf("read the replaced version: %w", err)
	}
	d := s.details()
	delete(d, "content")
	d["before"] = audit.Content(before)
	d["after"] = audit.Content(s.msg.Content)
	return a.Audit.RecordTx(ctx, tx, audit.Entry{Action: "message.edit", TargetType: "message", TargetID: id.String(), TargetName: s.channel, ChannelID: &s.msg.ChannelID, Details: d})
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
	var req DeleteMessageRequest
	if err := httpx.DecodeOptionalJSON(r, &req); err != nil {
		return "", err
	}
	return httpx.CleanText("reason", req.Reason, MaxModerationReason, false)
}

// tellAuthor lets the author know that a moderator removed their message.
func (a *API) tellAuthor(s *auditSnapshot, by *auth.User, reason string) {
	if s == nil || s.msg.UserID == by.ID || a.Events == nil {
		return
	}
	a.Events.SendToUsers([]uuid.UUID{s.msg.UserID}, wire.MessageDeletedNotice{
		Kind: "message_deleted", Reason: reason, ChannelName: s.channel,
	})
}

// channelAuditName reads a channel's name and whether it is private: a conversation or
// a Talk of one.
func channelAuditName(ctx context.Context, q db.Querier, id uuid.UUID) (name string, dm bool, err error) {
	err = q.QueryRow(ctx, `SELECT name, type = 'dm' OR dm_id IS NOT NULL FROM channels WHERE id = $1`, id).Scan(&name, &dm)
	return name, dm, err
}
