package chat

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	maxSearchQueryLen = 200
	maxSearchTerms    = 8
)

// SearchQuery filters messages in text channels. Every term of Text must
// occur in the message (case-insensitive substring); Has is "", "file",
// "image" or "link". Results are newest first; Before pages further back.
type SearchQuery struct {
	Text      string
	ChannelID *uuid.UUID
	AuthorID  *uuid.UUID
	Has       string
	Before    *uuid.UUID
	Limit     int
}

// likePattern escapes LIKE wildcards so user input matches literally.
func likePattern(term string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(term) + "%"
}

// Search returns one page of matching messages and whether more exist.
func Search(ctx context.Context, p *db.Pool, q SearchQuery) ([]Message, bool, error) {
	text := strings.TrimSpace(q.Text)
	if utf8.RuneCountInString(text) > maxSearchQueryLen {
		return nil, false, httpx.ErrInvalidInput("search text is too long")
	}
	terms := strings.Fields(text)
	if len(terms) > maxSearchTerms {
		terms = terms[:maxSearchTerms]
	}
	if len(terms) == 0 && q.ChannelID == nil && q.AuthorID == nil && q.Has == "" {
		return nil, false, httpx.ErrInvalidInput("enter a search term or choose a filter")
	}

	where := []string{"c.type = 'text'"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	for _, t := range terms {
		where = append(where, "m.content ILIKE "+arg(likePattern(t))+` ESCAPE '\'`)
	}
	if q.ChannelID != nil {
		where = append(where, "m.channel_id = "+arg(*q.ChannelID))
	}
	if q.AuthorID != nil {
		where = append(where, "m.user_id = "+arg(*q.AuthorID))
	}
	switch q.Has {
	case "":
	case "file":
		where = append(where, "EXISTS (SELECT 1 FROM media f WHERE f.message_id = m.id)")
	case "image":
		where = append(where, "EXISTS (SELECT 1 FROM media f WHERE f.message_id = m.id AND f.mime_type LIKE 'image/%')")
	case "link":
		where = append(where, `m.content ~* 'https?://'`)
	default:
		return nil, false, httpx.ErrInvalidInput("has must be file, image or link")
	}
	if q.Before != nil {
		where = append(where, "(m.created_at, m.id) < (SELECT created_at, id FROM messages WHERE id = "+arg(*q.Before)+")")
	}

	sql := messageSelect + `
	JOIN channels c ON c.id = m.channel_id
	WHERE ` + strings.Join(where, " AND ") + `
	ORDER BY m.created_at DESC, m.id DESC
	LIMIT ` + arg(q.Limit+1)

	rows, err := p.Query(ctx, sql, args...)
	if err != nil {
		return nil, false, fmt.Errorf("search: %w", err)
	}
	msgs, err := scanMessages(rows)
	if err != nil {
		return nil, false, fmt.Errorf("search: %w", err)
	}
	more := len(msgs) > q.Limit
	if more {
		msgs = msgs[:q.Limit]
	}
	if err := enrich(ctx, p, msgs); err != nil {
		return nil, false, err
	}
	return msgs, more, nil
}
