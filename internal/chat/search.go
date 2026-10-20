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
	sql, args, err := buildSearchSQL(q)
	if err != nil {
		return nil, false, err
	}
	var msgs []Message
	err = pgx.BeginTxFunc(ctx, p, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
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

func isSearchEmpty(terms []string, q SearchQuery) bool {
	if len(terms) > 0 || q.ChannelID != nil || q.AuthorID != nil {
		return false
	}
	return q.Has == ""
}

func validateSearchTerms(q SearchQuery) ([]string, error) {
	text := strings.TrimSpace(q.Text)
	if utf8.RuneCountInString(text) > maxSearchQueryLen {
		return nil, httpx.ErrInvalidInput("search text is too long")
	}
	terms := strings.Fields(text)
	if len(terms) > maxSearchTerms {
		terms = terms[:maxSearchTerms]
	}
	if isSearchEmpty(terms, q) {
		return nil, httpx.ErrInvalidInput("enter a search term or choose a filter")
	}
	return terms, nil
}

type searchBuilder struct {
	where []string
	args  []any
}

func (b *searchBuilder) addArg(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

func (b *searchBuilder) addTerms(terms []string) {
	for _, t := range terms {
		b.where = append(b.where, "m.content ILIKE "+b.addArg(likePattern(t))+` ESCAPE '\'`)
	}
}

func (b *searchBuilder) addFilters(q SearchQuery) error {
	if q.Visible != nil {
		b.where = append(b.where, "m.channel_id = ANY("+b.addArg(q.Visible)+"::uuid[])")
	}
	if q.ChannelID != nil {
		b.where = append(b.where, "m.channel_id = "+b.addArg(*q.ChannelID))
	}
	if q.AuthorID != nil {
		b.where = append(b.where, "m.user_id = "+b.addArg(*q.AuthorID))
	}
	has, err := hasCondition(q.Has)
	if err != nil {
		return err
	}
	if has != "" {
		b.where = append(b.where, has)
	}
	if q.Before != nil {
		b.where = append(b.where, "(m.created_at, m.id) < (SELECT created_at, id FROM messages WHERE id = "+b.addArg(*q.Before)+")")
	}
	return nil
}

// buildSearchSQL validates the query and turns it into SQL with its arguments.
func buildSearchSQL(q SearchQuery) (string, []any, error) {
	terms, err := validateSearchTerms(q)
	if err != nil {
		return "", nil, err
	}
	b := &searchBuilder{}
	b.addTerms(terms)
	if err := b.addFilters(q); err != nil {
		return "", nil, err
	}

	sql := messageSelect + `
	JOIN channels c ON c.id = m.channel_id
	WHERE ` + strings.Join(b.where, " AND ") + `
	ORDER BY m.created_at DESC, m.id DESC
	LIMIT ` + b.addArg(q.Limit+1)
	return sql, b.args, nil
}

// hasCondition is the WHERE condition of the "has" filter ("" for none).
func hasCondition(has string) (string, error) {
	switch has {
	case "":
		return "", nil
	case "file":
		return "EXISTS (SELECT 1 FROM media f WHERE f.message_id = m.id)", nil
	case "image":
		return "EXISTS (SELECT 1 FROM media f WHERE f.message_id = m.id AND f.mime_type LIKE 'image/%')", nil
	case "link":
		return `m.content ~* 'https?://'`, nil
	}
	return "", httpx.ErrInvalidInput("has must be file, image or link")
}
