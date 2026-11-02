package wiki

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const snippetRunes = 160

// likePattern is a substring pattern for ILIKE with the wildcards of q escaped.
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// Search finds pages whose title or text contains q (at least MinSearchRunes characters),
// titles first, then the most recently changed.
func (s *Store) Search(ctx context.Context, q string) ([]SearchHit, error) {
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) < MinSearchRunes {
		return []SearchHit{}, nil
	}
	rows, err := s.DB.Query(ctx, `
		SELECT slug, title, content FROM wiki_pages
		WHERE deleted_at IS NULL AND (title ILIKE $1 ESCAPE '\' OR content ILIKE $1 ESCAPE '\')
		ORDER BY (title ILIKE $1 ESCAPE '\') DESC, updated_at DESC LIMIT $2`, likePattern(q), SearchLimit)
	if err != nil {
		return nil, fmt.Errorf("search the wiki: %w", err)
	}
	hits, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SearchHit, error) {
		var h SearchHit
		var content string
		err := r.Scan(&h.Slug, &h.Title, &content)
		h.Snippet = snippet(content, q)
		return h, err
	})
	if hits == nil {
		hits = []SearchHit{}
	}
	return hits, err
}

// snippet is the words around the first match of q in text (or the start of the text).
func snippet(text, q string) string {
	runes := []rune(text)
	start := 0
	lower := strings.ToLower(text)
	at := strings.Index(lower, strings.ToLower(q))
	if at > 0 && len(lower) == len(text) {
		start = utf8.RuneCountInString(text[:at]) - snippetRunes/4
	}
	start = max(0, min(start, len(runes)))
	end := min(len(runes), start+snippetRunes)
	out := strings.Join(strings.Fields(string(runes[start:end])), " ")
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}
