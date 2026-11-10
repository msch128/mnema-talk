package forum

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// forumSortNewest maps the sort query value of the post list to its order:
// true for creation time, false for latest activity (the default).
func forumSortNewest(mode string) (bool, error) {
	switch mode {
	case "", "latest_activity", "activity":
		return false, nil
	case "newest", "creation":
		return true, nil
	}
	return false, httpx.ErrInvalidInput("sort must be latest_activity or newest")
}

// PostFilter narrows and orders the post list of a forum.
type PostFilter struct {
	// Sort is "latest_activity" (default) or "newest".
	Sort string
	// TagID keeps the posts that carry this tag.
	TagID *uuid.UUID
	// Search keeps the posts whose title, first message or a reply contains the text.
	Search string
}

// MaxPostSearchLen is the longest search text a list accepts.
const MaxPostSearchLen = 100

// ListForumPostsFiltered returns a page of root posts in channelID, ordered by
// latest activity or creation and narrowed by f's tag and search text. Pinned
// posts come first and only on the first page (no Before); the pages behind it hold
// the unpinned posts, so the cursor of the order never has to know about pins.
func ListForumPostsFiltered(ctx context.Context, p *db.Pool, channelID uuid.UUID, f PostFilter, q core.HistoryQuery) ([]core.Message, error) {
	newest, err := forumSortNewest(f.Sort)
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	if err := forumCursorExists(ctx, p, channelID, q.Before); err != nil {
		return nil, err
	}
	whereClause, args, err := postFilterWhere(channelID, f)
	if err != nil {
		return nil, err
	}

	l := postList{p: p, search: strings.TrimSpace(f.Search) != ""}
	var pinned []core.Message
	if q.Before == nil {
		if pinned, err = l.pinned(ctx, whereClause, args); err != nil {
			return nil, err
		}
	}
	whereClause += ` AND NOT m.is_pinned`

	var page []core.Message
	if newest {
		page, err = l.byCreation(ctx, whereClause, args, q.Before, limit)
	} else {
		page, err = l.byActivity(ctx, whereClause, args, q, limit)
	}
	if err != nil {
		return nil, err
	}
	return append(pinned, page...), nil
}

// forumCursorExists checks that the cursor is a post of this forum: a message
// of another channel answers like a missing one, so a cursor cannot tell
// whether a message elsewhere exists.
func forumCursorExists(ctx context.Context, p *db.Pool, channelID uuid.UUID, before *uuid.UUID) error {
	if before == nil {
		return nil
	}
	return core.CursorExists(ctx, p, `SELECT 1 FROM messages WHERE id = $1 AND channel_id = $2 AND parent_id IS NULL`, *before, channelID)
}

// postFilterWhere is the WHERE clause (and its arguments) of the root posts of
// channelID that match f.
func postFilterWhere(channelID uuid.UUID, f PostFilter) (string, []any, error) {
	where := `WHERE m.channel_id = $1 AND m.parent_id IS NULL`
	args := []any{channelID}
	if f.TagID != nil {
		args = append(args, *f.TagID)
		where += fmt.Sprintf(` AND EXISTS (SELECT 1 FROM forum_post_tags pt WHERE pt.message_id = m.id AND pt.tag_id = $%d)`, len(args))
	}
	if text := strings.TrimSpace(f.Search); text != "" {
		clean, err := httpx.CleanText("q", text, MaxPostSearchLen, false)
		if err != nil {
			return "", nil, err
		}
		args = append(args, "%"+likeEscaper.Replace(clean)+"%")
		n := len(args)
		where += fmt.Sprintf(` AND (m.title ILIKE $%[1]d OR m.content ILIKE $%[1]d OR EXISTS (SELECT 1 FROM messages r WHERE r.parent_id = m.id AND r.content ILIKE $%[1]d))`, n)
	}
	return where, args, nil
}

// likeEscaper makes user text literal inside a LIKE pattern.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// postList runs the queries of one post list.
type postList struct {
	p *db.Pool
	// search: the list is narrowed by a search text.
	search bool
}

// query runs one query of the list. A search reads every post and reply of
// the forum, so it is bounded like the message search and answers
// core.ErrSearchSlow when the database cancels it.
func (l postList) query(ctx context.Context, sql string, args ...any) ([]core.Message, error) {
	if !l.search {
		return messages.QueryMessages(ctx, l.p, sql, args...)
	}
	var msgs []core.Message
	err := core.SearchTx(ctx, l.p, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		msgs, err = messages.ScanMessages(rows)
		return err
	})
	if db.IsStatementTimeout(err) {
		return nil, core.ErrSearchSlow
	}
	if err != nil {
		return nil, fmt.Errorf("search posts: %w", err)
	}
	return msgs, messages.Enrich(ctx, l.p, msgs)
}

// pinned returns the pinned posts matching where, most recently pinned first.
func (l postList) pinned(ctx context.Context, where string, args []any) ([]core.Message, error) {
	sql := messages.MessageSelect + " " + where + fmt.Sprintf(` AND m.is_pinned ORDER BY m.pinned_at DESC, m.id DESC LIMIT %d`, messages.MaxPinsPerChannel)
	return l.query(ctx, sql, args...)
}

func (l postList) byCreation(ctx context.Context, where string, args []any, before *uuid.UUID, limit int) ([]core.Message, error) {
	if before != nil {
		args = append(args, *before)
		where += fmt.Sprintf(` AND (m.created_at, m.id) < (SELECT created_at, id FROM messages WHERE id = $%d)`, len(args))
	}
	args = append(args, limit)
	sql := messages.MessageSelect + " " + where + fmt.Sprintf(` ORDER BY m.created_at DESC, m.id DESC LIMIT $%d`, len(args))
	return l.query(ctx, sql, args...)
}

const activityExpr = `COALESCE((SELECT MAX(r.created_at) FROM messages r WHERE r.parent_id = m.id), m.created_at)`

// cursorActivityExpr is activityExpr evaluated for the cursor row (alias a).
const cursorActivityExpr = `COALESCE((SELECT MAX(r.created_at) FROM messages r WHERE r.parent_id = a.id), a.created_at)`

// byActivity pages by (activity, id). A client that sends the
// activity it saw for the cursor post (BeforeActivity) gets exactly the posts
// behind that position; without it the cursor's activity is read now, which
// moves when the cursor post has since got or lost a reply.
func (l postList) byActivity(ctx context.Context, where string, args []any, q core.HistoryQuery, limit int) ([]core.Message, error) {
	switch {
	case q.Before != nil && q.BeforeActivity != nil:
		args = append(args, *q.BeforeActivity, *q.Before)
		where += fmt.Sprintf(` AND (`+activityExpr+`, m.id) < ($%d, $%d)`, len(args)-1, len(args))
	case q.Before != nil:
		args = append(args, *q.Before)
		where += fmt.Sprintf(` AND (`+activityExpr+`, m.id) < (SELECT `+cursorActivityExpr+`, a.id FROM messages a WHERE a.id = $%d)`, len(args))
	}
	args = append(args, limit)
	sql := messages.MessageSelect + " " + where + fmt.Sprintf(` ORDER BY `+activityExpr+` DESC, m.id DESC LIMIT $%d`, len(args))
	return l.query(ctx, sql, args...)
}
