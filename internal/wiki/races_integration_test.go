//go:build integration

package wiki

import (
	"context"
	"fmt"
	"testing"

	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// Lane 7 of the defect hunt: the page cap and "one live page per title"
// (ignoring case) under creates, renames and undeletes that arrive together.
func TestParallelCreatesRenamesAndUndeletesKeepOneTitleAndTheCap(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	const callers = 6
	var renamed, deleted []*Page
	res := racetest.Sweep(t, racetest.Rounds(10, 200), callers,
		func(round int) {
			if _, err := w.p.Exec(ctx, `DELETE FROM wiki_redirects; DELETE FROM wiki_revisions; DELETE FROM wiki_pages`); err != nil {
				t.Fatal(err)
			}
			if _, err := w.p.Exec(ctx, `INSERT INTO wiki_pages (slug, title, position, created_by)
				SELECT 'filler-' || g, 'Filler ' || g, g, $1 FROM generate_series(1, $2) g`, w.alice, MaxPages-callers); err != nil {
				t.Fatalf("filler pages (the column list may need a revision): %v", err)
			}
			renamed, deleted = nil, nil
			for i := range callers / 3 {
				renamed = append(renamed, w.page(t, fmt.Sprintf("Other %d", i), nil))
				d := w.page(t, fmt.Sprintf("Shared title %d", round), nil)
				if _, err := w.s.Delete(ctx, w.alice, d.Slug); err != nil {
					t.Fatal(err)
				}
				// Another deleted page with the same title: undeleting both
				// would give two live pages of one title.
				if _, err := w.p.Exec(ctx, `UPDATE wiki_pages SET title = $2 WHERE id = $1`, d.ID, fmt.Sprintf("SHARED TITLE %d", round)); err != nil {
					t.Fatal(err)
				}
				deleted = append(deleted, d)
			}
		},
		func(round, i int) error {
			title := fmt.Sprintf("shared Title %d", round)
			switch i % 3 {
			case 0:
				_, err := w.s.Create(ctx, w.bob, PageInput{Title: title, Content: "x"})
				return err
			case 1:
				p := renamed[i/3]
				_, err := w.s.Change(ctx, w.bob, p.Slug, PageChange{BaseRev: p.Rev, Title: &title, Summary: "s"}, true)
				return err
			default:
				_, err := w.s.Undelete(ctx, deleted[i/3].Slug)
				return err
			}
		},
		func(round int, r racetest.Round) string {
			var same, live int
			if err := w.p.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE lower(title) = lower($1)), COUNT(*)
				FROM wiki_pages WHERE deleted_at IS NULL`, fmt.Sprintf("shared title %d", round)).Scan(&same, &live); err != nil {
				t.Fatal(err)
			}
			if same > 1 || live > MaxPages {
				return fmt.Sprintf("round %d: %d live pages titled alike, %d live pages, answers %v", round, same, live, r.Errs)
			}
			if same != 1 {
				return fmt.Sprintf("round %d: nobody got the title (%v)", round, r.Errs)
			}
			return ""
		})
	res.FailOnViolation(t)
}

// The page cap under creates and undeletes of distinct titles, one short of it.
func TestParallelCreatesAndUndeletesStayWithinThePageCap(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	const callers = 8
	var deleted []*Page
	racetest.Sweep(t, racetest.Rounds(10, 200), callers,
		func(round int) {
			if _, err := w.p.Exec(ctx, `DELETE FROM wiki_redirects; DELETE FROM wiki_revisions; DELETE FROM wiki_pages`); err != nil {
				t.Fatal(err)
			}
			deleted = nil
			for i := range callers / 2 {
				d := w.page(t, fmt.Sprintf("Gone %d %d", round, i), nil)
				if _, err := w.s.Delete(ctx, w.alice, d.Slug); err != nil {
					t.Fatal(err)
				}
				deleted = append(deleted, d)
			}
			if _, err := w.p.Exec(ctx, `INSERT INTO wiki_pages (slug, title, position, created_by)
				SELECT 'filler-' || g, 'Filler ' || g, g, $1 FROM generate_series(1, $2) g`, w.alice, MaxPages-1); err != nil {
				t.Fatal(err)
			}
		},
		func(round, i int) error {
			if i%2 == 0 {
				_, err := w.s.Create(ctx, w.bob, PageInput{Title: fmt.Sprintf("New %d %d", round, i), Content: "x"})
				return err
			}
			_, err := w.s.Undelete(ctx, deleted[i/2].Slug)
			return err
		},
		func(round int, r racetest.Round) string {
			var live int
			if err := w.p.QueryRow(ctx, `SELECT COUNT(*) FROM wiki_pages WHERE deleted_at IS NULL`).Scan(&live); err != nil {
				t.Fatal(err)
			}
			if live != MaxPages || r.Succeeded() != 1 {
				return fmt.Sprintf("round %d: %d live pages, %d writes succeeded", round, live, r.Succeeded())
			}
			return ""
		}).FailOnViolation(t)
}
