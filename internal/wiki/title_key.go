package wiki

import (
	"context"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"golang.org/x/text/unicode/norm"
)

// titleKey is what makes two titles "the same page" (doc/wiki.md): the title
// decomposed (NFD), every character lower-cased on its own with the Unicode
// simple lowercase mapping, composed again (NFC). The web app computes the same
// key to resolve [[Title]] (web/src/wiki/lib/wikiTitleKey.ts); both are checked
// against internal/wiki/testdata/title_keys.json.
//
// Decomposing first makes the rule the same in both languages: JavaScript's
// toLowerCase() differs from the simple mapping only for U+0130 (to i and a
// combining dot) and for the final sigma, which needs a word around it. After
// NFD there is no U+0130 left (it is I and U+0307), and one character at a time
// has no context. No full case folding (sharp s stays apart from ss, the final
// sigma from sigma): JavaScript has none, and those titles look different.
// NFC, not NFKC: canonical equivalents are the same text, while compatibility
// ones (a superscript two and a two, the ffi ligature and ffi) can look
// different, and NFKC would store another title than the writer typed.
func titleKey(title string) string {
	return norm.NFC.String(strings.Map(unicode.ToLower, norm.NFD.String(title)))
}

// cleanTitle checks a title and stores it composed (NFC), so one visible
// title has one spelling in the database. NFC runs after the invisible
// characters are gone (removing one can bring a letter and its accent
// together) and the length is checked again on the composed form.
func cleanTitle(title string) (string, error) {
	t, err := httpx.CleanName("title", title, MaxTitleRunes, true)
	if err != nil {
		return "", err
	}
	return httpx.CleanText("title", norm.NFC.String(t), MaxTitleRunes, true)
}

// titleTaken reports whether a live page other than except has a title with
// the same key: [[Title]] links name one page. The keys are compared here, not
// in SQL, because Postgres lower() is neither the web app's rule nor aware of
// canonical equivalence; the wiki holds at most MaxPages live pages.
func titleTaken(ctx context.Context, q db.Querier, title string, except uuid.UUID) (bool, error) {
	rows, err := q.Query(ctx, `SELECT title FROM wiki_pages WHERE deleted_at IS NULL AND id <> $1`, except)
	if err != nil {
		return false, err
	}
	titles, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return false, err
	}
	want := titleKey(title)
	for _, t := range titles {
		if titleKey(t) == want {
			return true, nil
		}
	}
	return false, nil
}
