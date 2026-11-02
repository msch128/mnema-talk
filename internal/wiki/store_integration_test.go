//go:build integration

package wiki

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

type world struct {
	s     *Store
	p     *db.Pool
	alice uuid.UUID
	bob   uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	w := &world{s: NewStore(p), p: p}
	for name, dst := range map[string]*uuid.UUID{"alice_w": &w.alice, "bob_w": &w.bob} {
		if err := p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ($1,$1,'x') RETURNING id`, name).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func (w *world) page(t *testing.T, title string, parent *uuid.UUID) *Page {
	t.Helper()
	p, err := w.s.Create(context.Background(), w.alice, PageInput{Title: title, ParentID: parent, Content: "first"})
	if err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	return p
}

func ptr[T any](v T) *T { return &v }

func is(err error, want *httpx.APIError) bool { return errors.Is(err, want) || err == want }

func (w *world) edit(slug string, base int, content string) (*Edited, error) {
	return w.s.Change(context.Background(), w.bob, slug, PageChange{BaseRev: base, Content: &content, Summary: "s"}, false)
}

func TestACreatedPageIsRevisionOneAndEveryEditIsTheNext(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	p := w.page(t, "Raid guide", nil)
	if p.Rev != 1 || p.Slug != "raid-guide" || p.Content != "first" || p.CreatedBy == nil || p.CreatedBy.Username != "alice_w" {
		t.Fatalf("page = %+v", p)
	}
	done, err := w.edit(p.Slug, 1, "second")
	if err != nil || !done.Revised || done.Page.Rev != 2 || done.Page.UpdatedBy.Username != "bob_w" {
		t.Fatalf("edit: %+v, %v", done, err)
	}
	revs, err := w.s.Revisions(ctx, p.Slug, 0, 10, false)
	if err != nil || len(revs) != 2 || revs[0].Rev != 2 || revs[0].Editor.Username != "bob_w" || revs[0].Summary != "s" || revs[1].Editor.Username != "alice_w" {
		t.Fatalf("revisions: %+v, %v", revs, err)
	}
	old, err := w.s.Revision(ctx, p.Slug, 1, false)
	if err != nil || old.Content == nil || *old.Content != "first" || old.Title != "Raid guide" {
		t.Fatalf("revision 1: %+v, %v", old, err)
	}
	if _, err := w.s.Revision(ctx, p.Slug, 9, false); !is(err, errRevGone) {
		t.Fatalf("a revision that does not exist: %v", err)
	}
}

func TestASaveOnAnOldRevisionIsRefusedAndChangesNothing(t *testing.T) {
	w := newWorld(t)
	p := w.page(t, "Rules", nil)
	if _, err := w.edit(p.Slug, 1, "mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Change(context.Background(), w.alice, p.Slug, PageChange{BaseRev: 1, Content: ptr("theirs")}, false); !is(err, errStale) {
		t.Fatalf("a stale save: %v", err)
	}
	got, _ := w.s.Get(context.Background(), p.Slug, false)
	if got.Content != "mine" || got.Rev != 2 {
		t.Fatalf("the page after the refused save: %+v", got)
	}
	if _, err := w.s.Change(context.Background(), w.alice, p.Slug, PageChange{Content: ptr("x")}, false); !is(err, errBaseRev) {
		t.Fatalf("a save without base_rev: %v", err)
	}
}

func TestTwoSavesOnTheSameRevisionLetOneWin(t *testing.T) {
	w := newWorld(t)
	p := w.page(t, "Race", nil)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := w.edit(p.Slug, 1, strings.Repeat("x", i+1))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	won := 0
	for err := range results {
		if err == nil {
			won++
		} else if !is(err, errStale) {
			t.Fatalf("unexpected: %v", err)
		}
	}
	if got, _ := w.s.Get(context.Background(), p.Slug, false); won != 1 || got.Rev != 2 {
		t.Fatalf("%d saves won, page at revision %d", won, got.Rev)
	}
}

func TestASaveThatChangesNothingWritesNoRevision(t *testing.T) {
	w := newWorld(t)
	p := w.page(t, "Same", nil)
	done, err := w.edit(p.Slug, 1, "first")
	if err != nil || done.Revised || done.Page.Rev != 1 {
		t.Fatalf("%+v, %v", done, err)
	}
}

func TestOldRevisionsArePrunedButNotThoseAKeptRestorePointsTo(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	p := w.page(t, "History", nil)
	for rev := 1; rev < 5; rev++ { // revisions 2 to 5
		if _, err := w.edit(p.Slug, rev, strings.Repeat("v", rev+1)); err != nil {
			t.Fatal(err)
		}
	}
	// Revision 6 restores revision 2.
	if _, err := w.s.RestoreRevision(ctx, w.bob, p.Slug, 2, RestoreInput{BaseRev: 5}, false); err != nil {
		t.Fatal(err)
	}
	for rev := 6; rev < 103; rev++ { // up to revision 103: the latest 100 are 4 to 103
		if _, err := w.edit(p.Slug, rev, strings.Repeat("w", rev)); err != nil {
			t.Fatal(err)
		}
	}
	have := func() map[int]bool {
		revs := map[int]bool{}
		rows, err := w.p.Query(ctx, `SELECT rev FROM wiki_revisions WHERE page_id = $1`, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var r int
			if err := rows.Scan(&r); err != nil {
				t.Fatal(err)
			}
			revs[r] = true
		}
		return revs
	}
	got := have()
	if got[1] || got[3] {
		t.Fatalf("old revisions stay: %v", got)
	}
	if !got[2] || !got[4] || !got[6] || !got[103] || len(got) != KeepRevisions+1 {
		t.Fatalf("revision 2 stays because revision 6 restores it: %d revisions, 2:%v 6:%v", len(got), got[2], got[6])
	}
	// Once the restore itself ages out (revisions 104 to 106), so does what it pointed to.
	for rev := 103; rev < 106; rev++ {
		if _, err := w.edit(p.Slug, rev, strings.Repeat("z", rev)); err != nil {
			t.Fatal(err)
		}
	}
	if got := have(); got[2] || got[6] || !got[7] || len(got) != KeepRevisions {
		t.Fatalf("after the restore aged out: %d revisions, 2:%v 6:%v", len(got), got[2], got[6])
	}
}

func TestRestoringARevisionIsANewRevisionAndRefusesWhatIsAlreadyThere(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	p := w.page(t, "Vandalised", nil)
	if _, err := w.edit(p.Slug, 1, "THE CONTENT HAS BEEN REPLACED"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.RestoreRevision(ctx, w.alice, p.Slug, 1, RestoreInput{BaseRev: 1}, false); !is(err, errStale) {
		t.Fatalf("restore on a stale base: %v", err)
	}
	got, err := w.s.RestoreRevision(ctx, w.alice, p.Slug, 1, RestoreInput{BaseRev: 2, Summary: "revert"}, false)
	if err != nil || got.Content != "first" || got.Rev != 3 {
		t.Fatalf("restore: %+v, %v", got, err)
	}
	revs, _ := w.s.Revisions(ctx, p.Slug, 0, 10, false)
	if len(revs) != 3 || revs[0].RestoredFrom == nil || *revs[0].RestoredFrom != 1 || revs[0].Summary != "revert" {
		t.Fatalf("the restore is a revision of its own: %+v", revs)
	}
	if _, err := w.s.RestoreRevision(ctx, w.alice, p.Slug, 1, RestoreInput{BaseRev: 3}, false); !is(err, errNothingNew) {
		t.Fatalf("restoring what is there: %v", err)
	}
	if _, err := w.s.RestoreRevision(ctx, w.alice, p.Slug, 42, RestoreInput{BaseRev: 3}, false); !is(err, errRevGone) {
		t.Fatalf("a revision that is gone: %v", err)
	}
	if _, err := w.s.RestoreRevision(ctx, w.alice, p.Slug, 1, RestoreInput{}, false); !is(err, errBaseRev) {
		t.Fatalf("restore without base_rev: %v", err)
	}
}

func TestALockedPageChangesOnlyForManagers(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	p := w.page(t, "Frozen", nil)
	if _, err := w.s.SetLocked(ctx, p.Slug, true); err != nil {
		t.Fatal(err)
	}
	if _, err := w.edit(p.Slug, 1, "no"); !is(err, errLocked) {
		t.Fatalf("an editor on a locked page: %v", err)
	}
	if _, err := w.s.RestoreRevision(ctx, w.bob, p.Slug, 1, RestoreInput{BaseRev: 1}, false); !is(err, errLocked) {
		t.Fatalf("a restore on a locked page: %v", err)
	}
	done, err := w.s.Change(ctx, w.alice, p.Slug, PageChange{BaseRev: 1, Content: ptr("yes")}, true)
	if err != nil || !done.Page.Locked || done.Page.Content != "yes" {
		t.Fatalf("a manager: %+v, %v", done, err)
	}
}

func TestAddressesAreDerivedMadeUniqueAndReserved(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	a := w.page(t, "Größe & Ärger!", nil)
	b := w.page(t, "Größe & Ärger!", nil)
	c := w.page(t, "Search", nil)
	if a.Slug != "groesse-aerger" || b.Slug != "groesse-aerger-2" || c.Slug != "search-2" {
		t.Fatalf("slugs: %q %q %q", a.Slug, b.Slug, c.Slug)
	}
	for _, bad := range []string{"Has Space", "-x", "x--y", "search", strings.Repeat("a", 81)} {
		if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "T", Slug: bad}); !is(err, errSlugInvalid) {
			t.Errorf("slug %q: %v", bad, err)
		}
	}
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "T", Slug: "groesse-aerger"}); !is(err, errSlugTaken) {
		t.Fatalf("a taken address: %v", err)
	}
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "  "}); err == nil {
		t.Fatal("an empty title")
	}
}

func TestARenamedPageIsStillFoundAtItsOldAddress(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	p := w.page(t, "Old name", nil)
	done, err := w.s.Change(ctx, w.alice, p.Slug, PageChange{Slug: ptr("new-name")}, true)
	if err != nil || !done.Renamed || done.Revised || done.Page.Slug != "new-name" || done.Page.Rev != 1 {
		t.Fatalf("rename: %+v, %v", done, err)
	}
	old, err := w.s.Get(ctx, "old-name", false)
	if err != nil || old.Slug != "new-name" || old.RedirectedFrom != "old-name" {
		t.Fatalf("the old address: %+v, %v", old, err)
	}
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "x", Slug: "old-name"}); !is(err, errSlugTaken) {
		t.Fatalf("a redirect keeps its address: %v", err)
	}
	// Editing through the old address works, and renaming back frees it again.
	if _, err := w.s.Change(ctx, w.alice, "old-name", PageChange{BaseRev: 1, Content: ptr("c")}, false); err != nil {
		t.Fatalf("edit via the old address: %v", err)
	}
	if _, err := w.s.Change(ctx, w.alice, "new-name", PageChange{Slug: ptr("old-name")}, true); err != nil {
		t.Fatalf("rename back: %v", err)
	}
	if got, _ := w.s.Get(ctx, "new-name", false); got.Slug != "old-name" || got.RedirectedFrom != "new-name" {
		t.Fatalf("after renaming back: %+v", got)
	}
	other := w.page(t, "Other", nil)
	if _, err := w.s.Change(ctx, w.alice, other.Slug, PageChange{Slug: ptr("old-name")}, true); !is(err, errSlugTaken) {
		t.Fatalf("taking a page's address: %v", err)
	}
	if _, err := w.s.Change(ctx, w.alice, other.Slug, PageChange{Slug: ptr("Not Valid")}, true); !is(err, errSlugInvalid) {
		t.Fatalf("a bad address: %v", err)
	}
}

func TestOnlyManagersMoveRenameOrReorder(t *testing.T) {
	w := newWorld(t)
	_ = w.page(t, "Anything", nil)
	// The handler checks the permission; the store refuses nothing on its own, so
	// its callers must pass the right flag. A change of tree fields without any
	// other field does not need base_rev.
	if _, err := w.s.Change(context.Background(), w.alice, "anything", PageChange{Position: ptr(3)}, true); err != nil {
		t.Fatal(err)
	}
}

func TestThePageTreeHasLimitsAndNoCycles(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	var parent *uuid.UUID
	var ids []uuid.UUID
	for level := 1; level <= MaxDepth; level++ {
		p := w.page(t, "Level "+string(rune('0'+level)), parent)
		parent = &p.ID
		ids = append(ids, p.ID)
	}
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "Too deep", ParentID: parent}); !is(err, errTooDeep) {
		t.Fatalf("a sixth level: %v", err)
	}
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "Orphan", ParentID: ptr(uuid.New())}); !is(err, errParentGone) {
		t.Fatalf("a parent that does not exist: %v", err)
	}
	top := w.page(t, "Another top", nil)
	// Moving the whole five-level tree below another page would make six levels.
	if _, err := w.s.Change(ctx, w.alice, "level-1", PageChange{ParentID: &top.ID}, true); !is(err, errTooDeep) {
		t.Fatalf("moving a subtree past the limit: %v", err)
	}
	// A page cannot move under itself or its own subpage.
	if _, err := w.s.Change(ctx, w.alice, "level-1", PageChange{ParentID: &ids[3]}, true); !is(err, errMoveIntoSelf) {
		t.Fatalf("a cycle: %v", err)
	}
	// A leaf moves to the top (the zero UUID) and below another page.
	if done, err := w.s.Change(ctx, w.alice, "level-5", PageChange{ParentID: ptr(uuid.Nil)}, true); err != nil || done.Page.ParentID != nil || !done.Moved {
		t.Fatalf("to the top: %+v, %v", done, err)
	}
	if done, err := w.s.Change(ctx, w.alice, "level-5", PageChange{ParentID: &top.ID}, true); err != nil || done.Page.ParentID == nil || *done.Page.ParentID != top.ID {
		t.Fatalf("below another page: %+v, %v", done, err)
	}
	if done, err := w.s.Change(ctx, w.alice, "level-5", PageChange{ParentID: &top.ID}, true); err != nil || done.Moved {
		t.Fatalf("moving where it is: %+v, %v", done, err)
	}
}

func TestTheWikiHoldsAtMostFiveHundredPages(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := w.p.Exec(ctx, `
		INSERT INTO wiki_pages (slug, title, created_by, updated_by)
		SELECT 'p-' || n, 'Page ' || n, $1, $1 FROM generate_series(1, $2) n`, w.alice, MaxPages-1); err != nil {
		t.Fatal(err)
	}
	last := w.page(t, "The last one", nil)
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "One too many"}); !is(err, errTooMany) {
		t.Fatalf("the 501st page: %v", err)
	}
	// A deleted page does not count, and cannot come back while the wiki is full.
	if _, err := w.s.Delete(ctx, w.bob, last.Slug); err != nil {
		t.Fatal(err)
	}
	again := w.page(t, "Instead", nil)
	if _, err := w.s.Undelete(ctx, last.Slug); !is(err, errTooMany) {
		t.Fatalf("undeleting into a full wiki: %v", err)
	}
	_ = again
}

func TestADeletedPageIsGoneForEveryoneButManagersAndComesBack(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	parent := w.page(t, "Parent", nil)
	child := w.page(t, "Child", &parent.ID)
	if _, err := w.s.Delete(ctx, w.bob, parent.Slug); !is(err, errHasChildren) {
		t.Fatalf("a page with subpages: %v", err)
	}
	if _, err := w.s.Delete(ctx, w.bob, child.Slug); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Get(ctx, child.Slug, false); !is(err, errPageNotFound) {
		t.Fatalf("a viewer finds a deleted page: %v", err)
	}
	if got, err := w.s.Get(ctx, child.Slug, true); err != nil || got.DeletedAt == nil {
		t.Fatalf("a manager: %+v, %v", got, err)
	}
	if _, err := w.s.Change(ctx, w.bob, child.Slug, PageChange{BaseRev: 1, Content: ptr("x")}, true); !is(err, errPageNotFound) {
		t.Fatalf("editing a deleted page: %v", err)
	}
	tree, _ := w.s.Tree(ctx, false)
	all, _ := w.s.Tree(ctx, true)
	if len(tree) != 1 || len(all) != 2 {
		t.Fatalf("tree %d, with deleted %d", len(tree), len(all))
	}
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "x", Slug: child.Slug}); !is(err, errSlugTaken) {
		t.Fatalf("a deleted page keeps its address: %v", err)
	}
	back, err := w.s.Undelete(ctx, child.Slug)
	if err != nil || back.DeletedAt != nil || back.ParentID == nil || back.Content != "first" {
		t.Fatalf("undelete: %+v, %v", back, err)
	}
	if _, err := w.s.Undelete(ctx, child.Slug); !is(err, errNotDeleted) {
		t.Fatalf("undeleting twice: %v", err)
	}
	// With its parent deleted too, it comes back at the top.
	if _, err := w.s.Delete(ctx, w.bob, child.Slug); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Delete(ctx, w.bob, parent.Slug); err != nil {
		t.Fatal(err)
	}
	if back, err := w.s.Undelete(ctx, child.Slug); err != nil || back.ParentID != nil {
		t.Fatalf("under a deleted parent: %+v, %v", back, err)
	}
}

func TestSearchFindsTitlesFirstAndOnlyWhatIsNotDeleted(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	a := w.page(t, "Loot rules", nil)
	if _, err := w.s.Change(ctx, w.alice, a.Slug, PageChange{BaseRev: 1, Content: ptr("We roll need before greed. 50% of the loot goes to the guild bank.")}, false); err != nil {
		t.Fatal(err)
	}
	b := w.page(t, "Bank", nil)
	if _, err := w.s.Change(ctx, w.alice, b.Slug, PageChange{BaseRev: 1, Content: ptr("Ask an officer about the loot.")}, false); err != nil {
		t.Fatal(err)
	}
	gone := w.page(t, "Old loot", nil)
	if _, err := w.s.Delete(ctx, w.alice, gone.Slug); err != nil {
		t.Fatal(err)
	}
	hits, err := w.s.Search(ctx, "loot")
	if err != nil || len(hits) != 2 || hits[0].Slug != "loot-rules" || hits[1].Slug != "bank" {
		t.Fatalf("hits: %+v, %v", hits, err)
	}
	if !strings.Contains(hits[1].Snippet, "loot") {
		t.Fatalf("snippet: %q", hits[1].Snippet)
	}
	if hits, _ := w.s.Search(ctx, "50%"); len(hits) != 1 {
		t.Fatalf("a percent sign is not a wildcard: %+v", hits)
	}
	if hits, _ := w.s.Search(ctx, "%"); len(hits) != 0 {
		t.Fatalf("one character: %+v", hits)
	}
	if hits, _ := w.s.Search(ctx, "__"); len(hits) != 0 {
		t.Fatalf("underscores are not wildcards: %+v", hits)
	}
}

func TestTextLimitsAndBadCharacters(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "Big", Content: strings.Repeat("a", MaxContentBytes+1)}); !is(err, errTooLong) {
		t.Fatalf("51 KB: %v", err)
	}
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "Max", Content: strings.Repeat("a", MaxContentBytes)}); err != nil {
		t.Fatalf("exactly 50 KB: %v", err)
	}
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "Nul", Content: "a\x00b"}); err == nil {
		t.Fatal("a NUL character")
	}
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: strings.Repeat("t", MaxTitleRunes+1)}); err == nil {
		t.Fatal("a title of 101 characters")
	}
}

func TestErasingAnEditorKeepsTheTextAndDetachesThePerson(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	p := w.page(t, "Shared", nil)
	if _, err := w.edit(p.Slug, 1, "bob wrote this"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.Exec(ctx, `DELETE FROM users WHERE id = $1`, w.bob); err != nil {
		t.Fatal(err)
	}
	got, err := w.s.Get(ctx, p.Slug, false)
	if err != nil || got.Content != "bob wrote this" || got.UpdatedBy != nil {
		t.Fatalf("page: %+v, %v", got, err)
	}
	revs, _ := w.s.Revisions(ctx, p.Slug, 0, 10, false)
	if len(revs) != 2 || revs[0].Editor != nil || revs[1].Editor == nil {
		t.Fatalf("revisions: %+v", revs)
	}
}
