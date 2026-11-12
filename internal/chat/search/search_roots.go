package search

import (
	"context"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/mdplain"
)

// SearchRoot is the thread root of a reply hit: enough to show where the reply
// belongs without loading the thread.
type SearchRoot struct {
	ID          uuid.UUID `json:"id" format:"uuid"`
	ChannelID   uuid.UUID `json:"channel_id" format:"uuid"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	// Snippet is the start of the root message (the whole
	// text when it has at most 200 characters, else the first 200 characters
	// of its plain text as Markdown, each spoiler as ||…||).
	// Escaped plain text holds no links or formatting, and the escaping
	// (a backslash before symbols) makes it at most 996 characters long.
	Snippet string `json:"snippet"`
}

// searchRoots loads the thread roots of the reply hits, keyed by root id. The
// root lives in the reply's channel, so it is as visible as the hit itself.
func searchRoots(ctx context.Context, p *db.Pool, msgs []core.Message) (map[string]SearchRoot, error) {
	roots := map[string]SearchRoot{}
	ids := make([]uuid.UUID, 0)
	seen := map[uuid.UUID]bool{}
	for _, m := range msgs {
		if m.ParentID != nil && !seen[*m.ParentID] {
			seen[*m.ParentID] = true
			ids = append(ids, *m.ParentID)
		}
	}
	if len(ids) == 0 {
		return roots, nil
	}
	rows, err := p.Query(ctx, `
		SELECT m.id, m.channel_id, u.username, COALESCE(m.author_name, u.display_name), m.content
		FROM messages m JOIN users u ON u.id = m.user_id
		WHERE m.id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r SearchRoot
		if err := rows.Scan(&r.ID, &r.ChannelID, &r.Username, &r.DisplayName, &r.Snippet); err != nil {
			return nil, err
		}
		r.Snippet = mdplain.Snippet(r.Snippet, mdplain.SnippetLen)
		roots[r.ID.String()] = r
	}
	return roots, rows.Err()
}
