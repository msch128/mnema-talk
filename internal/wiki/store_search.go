package wiki

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"
)

const snippetRunes = 160

// likePattern is a substring pattern for ILIKE with the wildcards of q escaped.
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// Search finds pages whose title or text contains q (at least MinSearchRunes characters),
// titles first, then the most recently changed. Titles are composed (NFC) when
// they are written; page text and old titles are stored as typed. So the query
// matches in both forms, composed and decomposed (one pattern when they agree).
func (s *Store) Search(ctx context.Context, q string) ([]SearchHit, error) {
	forms := queryForms(q)
	if utf8.RuneCountInString(forms[0]) < MinSearchRunes {
		return []SearchHit{}, nil
	}
	patterns := make([]string, len(forms))
	for i, f := range forms {
		patterns[i] = likePattern(f)
	}
	// ANY takes no ESCAPE clause; LIKE's default escape character is the backslash likePattern uses.
	rows, err := s.DB.Query(ctx, `
		SELECT slug, title, content FROM wiki_pages
		WHERE deleted_at IS NULL AND (title ILIKE ANY($1::text[]) OR content ILIKE ANY($1::text[]))
		ORDER BY (title ILIKE ANY($1::text[])) DESC, updated_at DESC LIMIT $2`, patterns, SearchLimit)
	if err != nil {
		return nil, fmt.Errorf("search the wiki: %w", err)
	}
	hits, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SearchHit, error) {
		var h SearchHit
		var content string
		err := r.Scan(&h.Slug, &h.Title, &content)
		h.Snippet = snippet(content, formIn(content, forms))
		return h, err
	})
	if hits == nil {
		hits = []SearchHit{}
	}
	return hits, err
}

// queryForms is the trimmed query composed (NFC) and, when that differs, decomposed (NFD).
func queryForms(q string) []string {
	c := norm.NFC.String(strings.TrimSpace(q))
	if d := norm.NFD.String(c); d != c {
		return []string{c, d}
	}
	return []string{c}
}

// formIn is the form of the query the text holds (ignoring case), else the first.
func formIn(text string, forms []string) string {
	lower := strings.ToLower(text)
	for _, f := range forms {
		if strings.Contains(lower, strings.ToLower(f)) {
			return f
		}
	}
	return forms[0]
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
