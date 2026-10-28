package auth

import (
	"context"
	"encoding/json"
	"errors"
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
	// DirectMessage marks a message of a conversation; its channel_id is the
	// conversation's id and channel_name is empty.
	DirectMessage bool      `json:"direct_message" db:"direct_message"`
	Title         *string   `json:"title,omitempty" db:"title"`
	Content       string    `json:"content" db:"content"`
	Edited        bool      `json:"edited" db:"is_edited"`
	Pinned        bool      `json:"pinned" db:"is_pinned"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// messageStream writes messages one by one and knows when the bounds are hit.
type messageStream struct {
	w         io.Writer
	viewable  viewSet
	written   int
	bytes     int
	truncated bool
}

// add writes one message; it reports false (and marks the document
// truncated) once the message or byte bound is reached.
func (ms *messageStream) add(m exportedMessage) (bool, error) {
	if ms.written >= exportMaxMessages || ms.bytes >= exportMessageBytes {
		ms.truncated = true
		return false, nil
	}
	if !ms.viewable.has(m.ChannelID) {
		m.ChannelName = nil
	}
	n, err := writeMessage(ms.w, m, ms.written > 0)
	ms.written++
	ms.bytes += n
	return err == nil, err
}

const messagePageSQL = `SELECT m.id, m.channel_id, c.name AS channel_name, m.parent_id, m.reply_to_id, m.in_dm AS direct_message,
	m.title, m.content, m.is_edited, m.is_pinned, m.created_at, m.updated_at
	FROM messages m JOIN channels c ON c.id = m.channel_id
	WHERE m.user_id = $1 AND (m.created_at, m.id) > ($2, $3) ORDER BY m.created_at, m.id LIMIT $4`

// streamMessages writes the member's messages as one JSON document, a page at
// a time, and stops (marking the document truncated) at the message or byte
// bound.
func streamMessages(ctx context.Context, q db.Querier, user uuid.UUID, viewable viewSet, w io.Writer) error {
	if _, err := io.WriteString(w, `{"messages":[`); err != nil {
		return err
	}
	ms := &messageStream{w: w, viewable: viewable}
	var lastAt time.Time
	lastID := uuid.Nil
	for !ms.truncated {
		page, err := collect(ctx, q, pgx.RowToStructByName[exportedMessage], messagePageSQL, user, lastAt, lastID, exportPage)
		if err != nil {
			return err
		}
		for _, m := range page {
			more, err := ms.add(m)
			if err != nil {
				return err
			}
			if !more {
				break
			}
			lastAt, lastID = m.CreatedAt, m.ID
		}
		if len(page) < exportPage {
			break
		}
	}
	_, err := fmt.Fprintf(w, `],"truncated":%t}`, ms.truncated)
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

func exportSettings(ctx context.Context, q db.Querier, user uuid.UUID, viewable viewSet) (capped, error) {
	type channelSetting struct {
		ChannelID   uuid.UUID  `json:"channel_id" db:"channel_id"`
		ChannelName *string    `json:"channel_name" db:"name"`
		Level       string     `json:"notify_level" db:"notify_level"`
		MutedUntil  *time.Time `json:"muted_until" db:"muted_until"`
		LastReadAt  *time.Time `json:"last_read_at" db:"last_read_at"`
	}
	channels, channelsCut, err := fetchRows[channelSetting](ctx, q, exportMaxRows, `SELECT cr.channel_id, c.name, cr.notify_level, cr.muted_until, cr.last_read_at
		FROM channel_reads cr JOIN channels c ON c.id = cr.channel_id WHERE cr.user_id = $1 ORDER BY c.name LIMIT $2`, user)
	if err != nil {
		return capped{}, err
	}
	for i := range channels {
		if !viewable.has(channels[i].ChannelID) {
			channels[i].ChannelName = nil
		}
	}
	type categorySetting struct {
		CategoryID uuid.UUID  `json:"category_id" db:"category_id"`
		Level      string     `json:"notify_level" db:"notify_level"`
		MutedUntil *time.Time `json:"muted_until" db:"muted_until"`
	}
	categories, categoriesCut, err := fetchRows[categorySetting](ctx, q, exportMaxRows, `SELECT category_id, notify_level, muted_until
		FROM category_notify WHERE user_id = $1 LIMIT $2`, user)
	if err != nil {
		return capped{}, err
	}
	server, err := exportServerSetting(ctx, q, user)
	if err != nil {
		return capped{}, err
	}
	return capped{
		value:     map[string]any{"channels": channels, "categories": categories, "server": server},
		truncated: channelsCut || categoriesCut,
	}, nil
}

func exportServerSetting(ctx context.Context, q db.Querier, user uuid.UUID) (map[string]any, error) {
	rows, err := q.Query(ctx, `SELECT notify_level, muted_until, suppress_everyone FROM server_notify WHERE user_id = $1`, user)
	if err != nil {
		return nil, err
	}
	type serverSetting struct {
		Level            string     `json:"notify_level" db:"notify_level"`
		MutedUntil       *time.Time `json:"muted_until" db:"muted_until"`
		SuppressEveryone bool       `json:"suppress_everyone" db:"suppress_everyone"`
	}
	set, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[serverSetting])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"notify_level": set.Level, "muted_until": set.MutedUntil, "suppress_everyone": set.SuppressEveryone}, nil
}

func exportReactions(ctx context.Context, q db.Querier, user uuid.UUID) (capped, error) {
	type reaction struct {
		MessageID uuid.UUID `json:"message_id" db:"message_id"`
		Emoji     string    `json:"emoji" db:"emoji"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
	}
	return listDataset[reaction](ctx, q, user, `SELECT message_id, emoji, created_at
		FROM message_reactions WHERE user_id = $1 ORDER BY created_at LIMIT $2`)
}

func exportUploads(ctx context.Context, q db.Querier, user uuid.UUID) (capped, error) {
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
	us, cut, err := fetchRows[upload](ctx, q, exportMaxRows, `SELECT id, original_filename, mime_type, size_bytes, message_id, is_deleted, created_at
		FROM media WHERE uploader_id = $1 ORDER BY created_at LIMIT $2`, user)
	if err != nil {
		return capped{}, err
	}
	for i := range us {
		if !us[i].Deleted {
			us[i].URL = "/api/media/" + us[i].ID.String()
		}
	}
	return listOf(us, cut), nil
}

func exportAudit(ctx context.Context, q db.Querier, user uuid.UUID) (capped, error) {
	type entry struct {
		At     time.Time `json:"at" db:"created_at"`
		Action string    `json:"action" db:"action"`
		By     string    `json:"by" db:"by"`
		About  string    `json:"about" db:"about"`
	}
	return listDataset[entry](ctx, q, user, `SELECT created_at, action,
		CASE WHEN actor_id = $1 THEN 'you' ELSE 'staff' END AS by,
		CASE WHEN target_type = 'user' AND target_id = $1::text THEN 'your account' ELSE target_type END AS about
		FROM audit_log WHERE actor_id = $1 OR (target_type = 'user' AND target_id = $1::text) ORDER BY id LIMIT $2`)
}
