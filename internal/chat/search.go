package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	maxSearchQueryLen = 200
	maxSearchTerms    = 8
	// searchStatementTimeout bounds one search query inside the database.
	searchStatementTimeout = "5s"
	// maxSearchesPerUser is how many searches one user may have running at once.
	maxSearchesPerUser = 2
)

// searchSlots counts the searches in flight per user.
var searchSlots = &inflight{max: maxSearchesPerUser, n: map[uuid.UUID]int{}}

type inflight struct {
	mu  sync.Mutex
	max int
	n   map[uuid.UUID]int
}

// acquire reserves a slot for id; the returned func frees it. ok is false at the cap.
func (f *inflight) acquire(id uuid.UUID) (release func(), ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.n[id] >= f.max {
		return nil, false
	}
	f.n[id]++
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.n[id]--; f.n[id] <= 0 {
			delete(f.n, id)
		}
	}, true
}

// AcquireSearchSlot reserves one of the user's concurrent search slots (used by
// the search handler; exported so integration tests can hold slots).
func AcquireSearchSlot(user uuid.UUID) (release func(), ok bool) {
	return searchSlots.acquire(user)
}

// mapSearchError turns the database's statement_timeout cancellation
// (SQLSTATE 57014) into errSearchSlow and wraps anything else.
func mapSearchError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "57014" {
		return errSearchSlow
	}
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	return nil
}

// errSearchBusy and errSearchSlow answer 429; searches are expensive.
var (
	errSearchBusy = httpx.NewAPIError(http.StatusTooManyRequests, httpx.CodeRateLimited, "too many searches at once, wait for the running ones to finish")
	errSearchSlow = httpx.NewAPIError(http.StatusTooManyRequests, httpx.CodeRateLimited, "the search took too long, narrow it down")
)

// SearchQuery filters messages in all channels (text and voice channels' chats). Every term of Text must
// occur in the message (case-insensitive substring); Has is "", "file",
// "image" or "link". Results are newest first; Before pages further back.
type SearchQuery struct {
	Text      string
	ChannelID *uuid.UUID
	AuthorID  *uuid.UUID
	Has       string
	Before    *uuid.UUID
	Limit     int
	// Visible limits the search to these channels; nil searches all of them
	// (an empty, non-nil list finds nothing).
	Visible []uuid.UUID
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

	// Every channel's chat is searched, voice channels included. The check
	// above guarantees at least one condition.
	where := []string{}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	for _, t := range terms {
		where = append(where, "m.content ILIKE "+arg(likePattern(t))+` ESCAPE '\'`)
	}
	if q.Visible != nil {
		where = append(where, "m.channel_id = ANY("+arg(q.Visible)+"::uuid[])")
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

	var msgs []Message
	err := pgx.BeginTxFunc(ctx, p, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '"+searchStatementTimeout+"'"); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		msgs, err = scanMessages(rows)
		return err
	})
	if err = mapSearchError(err); err != nil {
		return nil, false, err
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
