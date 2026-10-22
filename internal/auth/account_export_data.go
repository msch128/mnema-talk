package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// The datasets of an export. They hold the person's own data and nothing that
// names another member: messages carry the channel (when still visible) but
// not who else was there, reactions only the message id, audit entries say
// "you" or "staff".

type exportedMessage struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	ChannelID   uuid.UUID  `json:"channel_id" db:"channel_id"`
	ChannelName *string    `json:"channel_name" db:"channel_name"`
	ThreadOf    *uuid.UUID `json:"thread_of,omitempty" db:"parent_id"`
	ReplyTo     *uuid.UUID `json:"reply_to,omitempty" db:"reply_to_id"`
	Content     string     `json:"content" db:"content"`
	Edited      bool       `json:"edited" db:"is_edited"`
	Pinned      bool       `json:"pinned" db:"is_pinned"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

// streamMessages writes the member's messages as one JSON document, a page at
// a time, and stops (marking the document truncated) at the message or byte
// bound.
func streamMessages(ctx context.Context, q db.Querier, user uuid.UUID, viewable map[uuid.UUID]bool, w io.Writer) error {
	if _, err := io.WriteString(w, `{"messages":[`); err != nil {
		return err
	}
	var (
		written   int
		bytesOut  int
		truncated bool
		lastAt    = time.Time{}
		lastID    = uuid.Nil
	)
	for !truncated {
		page, err := collect(ctx, q, pgx.RowToStructByName[exportedMessage], `SELECT m.id, m.channel_id, c.name AS channel_name, m.parent_id, m.reply_to_id,
			m.content, m.is_edited, m.is_pinned, m.created_at, m.updated_at
			FROM messages m JOIN channels c ON c.id = m.channel_id
			WHERE m.user_id = $1 AND (m.created_at, m.id) > ($2, $3) ORDER BY m.created_at, m.id LIMIT $4`, user, lastAt, lastID, exportPage)
		if err != nil {
			return err
		}
		for _, m := range page {
			if written >= exportMaxMessages || bytesOut >= exportMessageBytes {
				truncated = true
				break
			}
			if !viewable[m.ChannelID] {
				m.ChannelName = nil
			}
			n, err := writeMessage(w, m, written > 0)
			if err != nil {
				return err
			}
			written++
			bytesOut += n
			lastAt, lastID = m.CreatedAt, m.ID
		}
		if len(page) < exportPage {
			break
		}
	}
	_, err := fmt.Fprintf(w, `],"truncated":%t}`, truncated)
	return err
}

func writeMessage(w io.Writer, m exportedMessage, comma bool) (int, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return 0, err
	}
	if comma {
		raw = append([]byte{','}, raw...)
	}
	n, err := w.Write(raw)
	return n, err
}

func exportSettings(ctx context.Context, q db.Querier, user uuid.UUID, viewable map[uuid.UUID]bool) (map[string]any, error) {
	type channelSetting struct {
		ChannelID   uuid.UUID  `json:"channel_id" db:"channel_id"`
		ChannelName *string    `json:"channel_name" db:"name"`
		Level       string     `json:"notify_level" db:"notify_level"`
		MutedUntil  *time.Time `json:"muted_until" db:"muted_until"`
		LastReadAt  *time.Time `json:"last_read_at" db:"last_read_at"`
	}
	channels, err := collect(ctx, q, pgx.RowToStructByName[channelSetting], `SELECT cr.channel_id, c.name, cr.notify_level, cr.muted_until, cr.last_read_at
		FROM channel_reads cr JOIN channels c ON c.id = cr.channel_id WHERE cr.user_id = $1 ORDER BY c.name LIMIT $2`, user, exportMaxRows)
	if err != nil {
		return nil, err
	}
	for i := range channels {
		if !viewable[channels[i].ChannelID] {
			channels[i].ChannelName = nil
		}
	}
	type categorySetting struct {
		CategoryID uuid.UUID  `json:"category_id" db:"category_id"`
		Level      string     `json:"notify_level" db:"notify_level"`
		MutedUntil *time.Time `json:"muted_until" db:"muted_until"`
	}
	categories, err := collect(ctx, q, pgx.RowToStructByName[categorySetting], `SELECT category_id, notify_level, muted_until
		FROM category_notify WHERE user_id = $1 LIMIT $2`, user, exportMaxRows)
	if err != nil {
		return nil, err
	}
	return map[string]any{"channels": channels, "categories": categories}, nil
}

func exportReactions(ctx context.Context, q db.Querier, user uuid.UUID) ([]any, error) {
	type reaction struct {
		MessageID uuid.UUID `json:"message_id" db:"message_id"`
		Emoji     string    `json:"emoji" db:"emoji"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
	}
	rs, err := collect(ctx, q, pgx.RowToStructByName[reaction], `SELECT message_id, emoji, created_at
		FROM message_reactions WHERE user_id = $1 ORDER BY created_at LIMIT $2`, user, exportMaxRows)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(rs))
	for i, r := range rs {
		out[i] = r
	}
	return out, nil
}

func exportUploads(ctx context.Context, q db.Querier, user uuid.UUID) ([]any, error) {
	type upload struct {
		ID        uuid.UUID  `json:"id" db:"id"`
		Filename  string     `json:"filename" db:"original_filename"`
		MimeType  string     `json:"mime_type" db:"mime_type"`
		SizeBytes int64      `json:"size_bytes" db:"size_bytes"`
		MessageID *uuid.UUID `json:"message_id,omitempty" db:"message_id"`
		Deleted   bool       `json:"deleted" db:"is_deleted"`
		CreatedAt time.Time  `json:"created_at" db:"created_at"`
		URL       string     `json:"url,omitempty" db:"-"`
	}
	us, err := collect(ctx, q, pgx.RowToStructByName[upload], `SELECT id, original_filename, mime_type, size_bytes, message_id, is_deleted, created_at
		FROM media WHERE uploader_id = $1 ORDER BY created_at LIMIT $2`, user, exportMaxRows)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(us))
	for i, u := range us {
		if !u.Deleted {
			u.URL = "/api/media/" + u.ID.String()
		}
		out[i] = u
	}
	return out, nil
}

func exportAudit(ctx context.Context, q db.Querier, user uuid.UUID) ([]any, error) {
	type entry struct {
		At     time.Time `json:"at" db:"created_at"`
		Action string    `json:"action" db:"action"`
		By     string    `json:"by" db:"by"`
		About  string    `json:"about" db:"about"`
	}
	es, err := collect(ctx, q, pgx.RowToStructByName[entry], `SELECT created_at, action,
		CASE WHEN actor_id = $1 THEN 'you' ELSE 'staff' END AS by,
		CASE WHEN target_type = 'user' AND target_id = $1::text THEN 'your account' ELSE target_type END AS about
		FROM audit_log WHERE actor_id = $1 OR (target_type = 'user' AND target_id = $1::text) ORDER BY id LIMIT $2`, user, exportMaxRows)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(es))
	for i, e := range es {
		out[i] = e
	}
	return out, nil
}
