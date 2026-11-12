//go:build integration

package wiki

import (
	"context"
	"testing"
)

// Two live pages may already share a title key: they were written before titles
// were compared by key (an NFC and an NFD spelling of one title). Nothing
// refuses them at start-up; both stay in the tree, both can be edited, no third
// page can take their title, and a rename or restore onto it is refused as for
// any other taken title (doc/wiki.md).
func TestPagesThatAlreadyShareATitleKeyStayEditable(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	composed := w.page(t, "Caf\u00e9", nil)
	const decomposed = "Cafe\u0301"
	if _, err := w.p.Exec(ctx, `
		WITH p AS (INSERT INTO wiki_pages (slug, title, content, created_by, updated_by) VALUES ('cafe-old', $1, 'old', $2, $2) RETURNING id)
		INSERT INTO wiki_revisions (page_id, rev, editor_id, title, content) SELECT id, 1, $2, $1, 'old' FROM p`, decomposed, w.alice); err != nil {
		t.Fatal(err)
	}
	tree, err := w.s.Tree(ctx, false)
	if err != nil || len(tree) != 2 {
		t.Fatalf("both pages are listed: %+v, %v", tree, err)
	}
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "CAF\u00c9"}); !is(err, errTitleTaken) {
		t.Fatalf("a third page with the title: %v", err)
	}
	// The old page saves with its own title, which is stored composed from then on.
	edited, err := w.s.Change(ctx, w.alice, "cafe-old", PageChange{BaseRev: 1, Title: ptr(decomposed), Content: ptr("new")}, false)
	if err != nil || edited.Page.Title != composed.Title {
		t.Fatalf("edit of the old page: %+v, %v", edited, err)
	}
	guide := w.page(t, "Guide", nil)
	if _, err := w.s.Change(ctx, w.alice, guide.Slug, PageChange{BaseRev: 1, Title: ptr("cafe\u0301")}, false); !is(err, errTitleTaken) {
		t.Fatalf("rename onto the shared title: %v", err)
	}
	// The admin resolves the pair with a rename; restoring the old title is then refused.
	if _, err := w.s.Change(ctx, w.alice, "cafe-old", PageChange{BaseRev: 2, Title: ptr("Caf\u00e9 (old)")}, false); err != nil {
		t.Fatalf("rename away: %v", err)
	}
	if _, err := w.s.RestoreRevision(ctx, w.alice, "cafe-old", 1, RestoreInput{BaseRev: 3}, false); !is(err, errTitleTaken) {
		t.Fatalf("restore onto the taken title: %v", err)
	}
}
