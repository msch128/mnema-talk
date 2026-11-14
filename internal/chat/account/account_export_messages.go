package account

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/userdata"
)

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

const messagePageSQL = `SELECT m.id, m.channel_id, c.name AS channel_name, m.parent_id, m.reply_to_id, m.in_dm AS direct_message,
	m.title, m.content, m.is_edited, m.is_pinned, m.created_at, m.updated_at
	FROM messages m JOIN channels c ON c.id = m.channel_id
	WHERE m.user_id = $1 AND (m.created_at, m.id) > ($2, $3) AND (m.expires_at IS NULL OR m.expires_at > now())
	ORDER BY m.created_at, m.id LIMIT $4`

func messageFile(ctx context.Context, q db.Querier, user uuid.UUID, channels userdata.Channels, limits userdata.Limits) userdata.File {
	return userdata.File{
		Name: "messages.json", Order: userdata.FileMessages,
		Stream: func(w io.Writer) error {
			return streamMessages(ctx, q, user, channels, limits, w)
		},
	}
}

// messageStream writes messages one by one and knows when the bounds are hit.
type messageStream struct {
	w         io.Writer
	channels  userdata.Channels
	limits    userdata.Limits
	written   int
	bytes     int
	truncated bool
}

// add writes one message. It reports false, and marks the document truncated,
// once the message or byte bound is reached.
func (ms *messageStream) add(m exportedMessage) (bool, error) {
	if ms.written >= ms.limits.MaxMessages || ms.bytes >= ms.limits.MessageBytes {
		ms.truncated = true
		return false, nil
	}
	if !ms.channels.Has(m.ChannelID) {
		m.ChannelName = nil
	}
	n, err := writeMessage(ms.w, m, ms.written > 0)
	ms.written++
	ms.bytes += n
	return err == nil, err
}

// streamMessages writes the member's messages as one JSON document, a page at
// a time, and stops (marking the document truncated) at the message or byte bound.
func streamMessages(ctx context.Context, q db.Querier, user uuid.UUID, channels userdata.Channels, limits userdata.Limits, w io.Writer) error {
	if _, err := io.WriteString(w, `{"messages":[`); err != nil {
		return err
	}
	ms := &messageStream{w: w, channels: channels, limits: limits}
	var lastAt time.Time
	lastID := uuid.Nil
	for !ms.truncated {
		page, err := userdata.Collect(ctx, q, pgx.RowToStructByName[exportedMessage], messagePageSQL, user, lastAt, lastID, limits.Page)
		if err != nil {
			return err
		}
		if err := ms.writePage(page, &lastAt, &lastID); err != nil {
			return err
		}
		if len(page) < limits.Page {
			break
		}
	}
	_, err := fmt.Fprintf(w, `],"truncated":%t}`, ms.truncated)
	return err
}

func (ms *messageStream) writePage(page []exportedMessage, lastAt *time.Time, lastID *uuid.UUID) error {
	for _, m := range page {
		more, err := ms.add(m)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
		*lastAt, *lastID = m.CreatedAt, m.ID
	}
	return nil
}

func writeMessage(w io.Writer, m exportedMessage, comma bool) (int, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return 0, err
	}
	if comma {
		raw = append([]byte{','}, raw...)
	}
	return w.Write(raw)
}
