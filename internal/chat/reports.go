package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Reports are how a member brings one message, from a channel or from a direct
// message, to the moderators. A report carries its own copy of the message and
// of a little context around it, because that copy is the only part of a
// direct message conversation a moderator can ever read; the author deleting
// the message afterwards does not undo the report (doc/moderation.md).

const (
	// ReportContextBefore and ReportContextAfter are the messages kept around
	// the reported one: at most 20 in all.
	ReportContextBefore = 15
	ReportContextAfter  = 5
	// MaxReportNote is the longest note a reporter or a moderator may write.
	MaxReportNote = 500
	// ReportsPerHour is how many reports one member may file per hour.
	ReportsPerHour = 10
	// MaxOpenReportsPerMember bounds the open reports one member has filed.
	MaxOpenReportsPerMember = 50
)

// Report kinds and statuses.
const (
	ReportKindChannel = "channel"
	ReportKindDM      = "dm"

	ReportOpen      = "open"
	ReportResolved  = "resolved"
	ReportDismissed = "dismissed"
)

var reportCategories = map[string]bool{"spam": true, "harassment": true, "illegal": true, "other": true}

var (
	errReportOwn      = httpx.ErrInvalidInput("you can't report your own message")
	errReportNotFound = httpx.ErrNotFound("report not found")
	errTooManyReports = httpx.ErrConflict(fmt.Sprintf("you have %d open reports; wait for the moderators to handle some", MaxOpenReportsPerMember))
)

// CreateReportRequest is the body of POST /api/messages/{messageID}/report.
type CreateReportRequest struct {
	// Category is why the message is reported.
	Category string `json:"category" enums:"spam,harassment,illegal,other"`
	// Note is an optional explanation for the moderators, up to 500 characters.
	Note string `json:"note" maxLength:"500" binding:"optional"`
}

// ReportAttachment is a file of a reported message: name and type only, the
// moderators never get access to a file through a report.
type ReportAttachment struct {
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
}

// ReportedMessage is the copy of one message kept in a report.
type ReportedMessage struct {
	ID          uuid.UUID          `json:"id" format:"uuid"`
	UserID      uuid.UUID          `json:"user_id" format:"uuid"`
	Username    string             `json:"username"`
	DisplayName string             `json:"display_name"`
	Content     string             `json:"content"`
	Attachments []ReportAttachment `json:"attachments"`
	ParentID    *uuid.UUID         `json:"parent_id,omitempty" binding:"optional" format:"uuid"`
	CreatedAt   time.Time          `json:"created_at" format:"date-time"`
}

// ReportSnapshot is what a report keeps of the conversation.
type ReportSnapshot struct {
	Message ReportedMessage `json:"message"`
	// Context are up to 20 messages of the same channel or conversation
	// around the reported one, oldest first.
	Context []ReportedMessage `json:"context"`
}

// reportMessagesSQL loads the reported message ($2) and its neighbours in the
// channel ($1) in one statement, oldest first: ReportContextBefore before and
// ReportContextAfter after.
var reportMessagesSQL = fmt.Sprintf(`
	WITH anchor AS (SELECT created_at, id FROM messages WHERE id = $2 AND channel_id = $1),
	picked AS (
	    (SELECT m.id FROM messages m, anchor a WHERE m.channel_id = $1 AND (m.created_at, m.id) < (a.created_at, a.id)
	     ORDER BY m.created_at DESC, m.id DESC LIMIT %d)
	    UNION ALL SELECT id FROM anchor
	    UNION ALL
	    (SELECT m.id FROM messages m, anchor a WHERE m.channel_id = $1 AND (m.created_at, m.id) > (a.created_at, a.id)
	     ORDER BY m.created_at, m.id LIMIT %d)
	)
	SELECT m.id, m.user_id, u.username, COALESCE(m.author_name, u.display_name), m.content, m.parent_id, m.created_at,
	       COALESCE((SELECT jsonb_agg(jsonb_build_object('filename', md.original_filename, 'mime_type', md.mime_type, 'size_bytes', md.size_bytes)
	                                 ORDER BY md.position, md.created_at) FROM media md WHERE md.message_id = m.id), '[]'::jsonb)
	FROM picked JOIN messages m ON m.id = picked.id JOIN users u ON u.id = m.user_id
	ORDER BY m.created_at, m.id`, ReportContextBefore, ReportContextAfter)

// reportContext loads the reported message and its neighbours.
func reportContext(ctx context.Context, p *db.Pool, channelID, messageID uuid.UUID) (ReportSnapshot, error) {
	snap := ReportSnapshot{Context: []ReportedMessage{}}
	rows, err := p.Query(ctx, reportMessagesSQL, channelID, messageID)
	if err != nil {
		return snap, fmt.Errorf("report context: %w", err)
	}
	all, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ReportedMessage, error) {
		var m ReportedMessage
		var files []byte
		if err := row.Scan(&m.ID, &m.UserID, &m.Username, &m.DisplayName, &m.Content, &m.ParentID, &m.CreatedAt, &files); err != nil {
			return m, err
		}
		return m, json.Unmarshal(files, &m.Attachments)
	})
	if err != nil {
		return snap, fmt.Errorf("report context: %w", err)
	}
	for _, m := range all {
		if m.ID == messageID {
			snap.Message = m
		} else {
			snap.Context = append(snap.Context, m)
		}
	}
	if snap.Message.ID != messageID {
		return snap, errMessageNotFound
	}
	return snap, nil
}

// NewReport describes a report to store.
type NewReport struct {
	Reporter  uuid.UUID
	MessageID uuid.UUID
	Category  string
	Note      string
}

// FileReport stores a report of a message the reporter can see (the caller
// checked that). A repeated report of the same message by the same member
// while the first is open returns the first one's id. A direct message is
// recognised by its channel's type, and its report names no channel.
func FileReport(ctx context.Context, p *db.Pool, nr NewReport) (uuid.UUID, error) {
	if !reportCategories[nr.Category] {
		return uuid.Nil, httpx.ErrInvalidInput("category must be spam, harassment, illegal or other")
	}
	ref, err := loadMessageRef(ctx, p, nr.MessageID)
	if err != nil {
		return uuid.Nil, err
	}
	if ref.AuthorID == nr.Reporter {
		return uuid.Nil, errReportOwn
	}
	var kind, name string
	if err := p.QueryRow(ctx, `SELECT CASE WHEN type = 'dm' THEN 'dm' ELSE 'channel' END, CASE WHEN type = 'dm' THEN '' ELSE name END FROM channels WHERE id = $1`, ref.ChannelID).Scan(&kind, &name); err != nil {
		return uuid.Nil, fmt.Errorf("load report channel: %w", err)
	}
	snap, err := reportContext(ctx, p, ref.ChannelID, nr.MessageID)
	if err != nil {
		return uuid.Nil, err
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return uuid.Nil, err
	}
	return insertReport(ctx, p, nr, ref, kind, name, raw)
}

func insertReport(ctx context.Context, p *db.Pool, nr NewReport, ref *messageRef, kind, name string, snapshot []byte) (uuid.UUID, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, nr.Reporter); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT id FROM message_reports WHERE reporter_id = $1 AND message_id = $2 AND status = 'open'`, nr.Reporter, nr.MessageID).Scan(&id)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var open int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM message_reports WHERE reporter_id = $1 AND status = 'open'`, nr.Reporter).Scan(&open); err != nil {
			return err
		}
		if open >= MaxOpenReportsPerMember {
			return errTooManyReports
		}
		return tx.QueryRow(ctx, `
			INSERT INTO message_reports (reporter_id, reported_user_id, message_id, channel_id, kind, channel_name, category, note, snapshot)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			nr.Reporter, ref.AuthorID, nr.MessageID, ref.ChannelID, kind, name, nr.Category, nr.Note, snapshot).Scan(&id)
	})
	if db.IsUniqueViolation(err) {
		return uuid.Nil, httpx.ErrConflict("this message is already reported")
	}
	return id, err
}
