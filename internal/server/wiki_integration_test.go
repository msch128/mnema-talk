//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/wiki"
)

type wikiFixture struct {
	a       *app
	admin   *client
	viewer  *client // a plain member: may read
	editor  *client // EditWiki
	manager *client // ManageWiki
}

func newWikiFixture(t *testing.T) *wikiFixture {
	t.Helper()
	a := newApp(t, false)
	admin := a.seedAdmin()
	f := &wikiFixture{a: a, admin: admin, viewer: a.register(admin, "vera"), editor: a.register(admin, "edda"), manager: a.register(admin, "manu")}
	assignRoles(t, admin, f.editor, createRole(t, admin, "wiki editors", perm.EditWiki))
	assignRoles(t, admin, f.manager, createRole(t, admin, "wiki managers", perm.ManageWiki))
	return f
}

type wikiPage struct {
	ID       string  `json:"id"`
	Slug     string  `json:"slug"`
	Title    string  `json:"title"`
	Content  string  `json:"content"`
	Rev      int     `json:"rev"`
	Locked   bool    `json:"locked"`
	ParentID *string `json:"parent_id"`
}

func (f *wikiFixture) create(t *testing.T, c *client, title, content string) wikiPage {
	t.Helper()
	res := c.post("/api/wiki/pages", map[string]any{"title": title, "content": content})
	wantStatus(t, "create "+title, res, http.StatusCreated)
	var p wikiPage
	res.decode(t, &p)
	return p
}

func TestEveryoneReadsTheWikiButOnlyHoldersOfAPermissionWriteIt(t *testing.T) {
	f := newWikiFixture(t)
	page := f.create(t, f.admin, "Welcome", "# Hello")
	edit := func(c *client, base int, content string) response {
		return c.patch("/api/wiki/pages/"+page.Slug, map[string]any{"base_rev": base, "content": content})
	}

	// A plain member reads the tree, the page, the revisions and searches.
	for _, path := range []string{"/api/wiki", "/api/wiki/pages/welcome", "/api/wiki/pages/welcome/revisions", "/api/wiki/pages/welcome/revisions/1", "/api/wiki/search?q=hello"} {
		wantStatus(t, "viewer reads "+path, f.viewer.get(path), http.StatusOK)
	}
	// But does not write, whatever they try.
	wantStatus(t, "viewer creates", f.viewer.post("/api/wiki/pages", map[string]any{"title": "x"}), http.StatusForbidden)
	wantStatus(t, "viewer edits", edit(f.viewer, 1, "vandalism"), http.StatusForbidden)
	wantStatus(t, "viewer restores", f.viewer.post("/api/wiki/pages/welcome/revisions/1/restore", map[string]any{"base_rev": 1}), http.StatusForbidden)
	wantStatus(t, "viewer locks", f.viewer.put("/api/wiki/pages/welcome/lock", map[string]any{"locked": true}), http.StatusForbidden)
	wantStatus(t, "viewer deletes", f.viewer.delete("/api/wiki/pages/welcome"), http.StatusForbidden)
	wantStatus(t, "viewer moves", f.viewer.patch("/api/wiki/pages/welcome", map[string]any{"slug": "other"}), http.StatusForbidden)

	// An editor writes and restores, but does not manage.
	wantStatus(t, "editor edits", edit(f.editor, 1, "better"), http.StatusOK)
	created := f.create(t, f.editor, "Editor page", "mine")
	wantStatus(t, "editor restores", f.editor.post("/api/wiki/pages/welcome/revisions/1/restore", map[string]any{"base_rev": 2}), http.StatusOK)
	wantStatus(t, "editor renames", f.editor.patch("/api/wiki/pages/"+created.Slug, map[string]any{"slug": "renamed"}), http.StatusForbidden)
	wantStatus(t, "editor moves", f.editor.patch("/api/wiki/pages/"+created.Slug, map[string]any{"parent_id": page.ID}), http.StatusForbidden)
	wantStatus(t, "editor locks", f.editor.put("/api/wiki/pages/welcome/lock", map[string]any{"locked": true}), http.StatusForbidden)
	wantStatus(t, "editor deletes", f.editor.delete("/api/wiki/pages/"+created.Slug), http.StatusForbidden)
	wantStatus(t, "editor sees no deleted pages", f.editor.get("/api/wiki?deleted=true"), http.StatusOK)

	// A manager organises and moderates, but does not write text.
	wantStatus(t, "manager edits text", edit(f.manager, 3, "no"), http.StatusForbidden)
	wantStatus(t, "manager moves", f.manager.patch("/api/wiki/pages/"+created.Slug, map[string]any{"parent_id": page.ID}), http.StatusOK)
	wantStatus(t, "manager renames", f.manager.patch("/api/wiki/pages/"+created.Slug, map[string]any{"slug": "renamed"}), http.StatusOK)
	wantStatus(t, "manager locks", f.manager.put("/api/wiki/pages/welcome/lock", map[string]any{"locked": true}), http.StatusOK)
	wantStatus(t, "a locked page refuses the editor", edit(f.editor, 3, "x"), http.StatusForbidden)
	wantStatus(t, "and the restore", f.editor.post("/api/wiki/pages/welcome/revisions/1/restore", map[string]any{"base_rev": 3}), http.StatusForbidden)
	wantStatus(t, "a manager unlocks", f.manager.put("/api/wiki/pages/welcome/lock", map[string]any{"locked": false}), http.StatusOK)
	wantStatus(t, "a page with a subpage is not deleted", f.manager.delete("/api/wiki/pages/welcome"), http.StatusConflict)
	wantStatus(t, "manager deletes", f.manager.delete("/api/wiki/pages/renamed"), http.StatusOK)
	wantStatus(t, "viewers no longer find it", f.viewer.get("/api/wiki/pages/renamed"), http.StatusNotFound)
	wantStatus(t, "managers still do", f.manager.get("/api/wiki/pages/renamed"), http.StatusOK)
	wantStatus(t, "viewers do not undelete", f.viewer.post("/api/wiki/pages/renamed/undelete", nil), http.StatusForbidden)
	wantStatus(t, "manager undeletes", f.manager.post("/api/wiki/pages/renamed/undelete", nil), http.StatusOK)

	// The Owner and Administrators do everything.
	wantStatus(t, "admin edits", edit(f.admin, 3, "admin"), http.StatusOK)
	wantStatus(t, "admin locks", f.admin.put("/api/wiki/pages/welcome/lock", map[string]any{"locked": true}), http.StatusOK)
	wantStatus(t, "admin edits a locked page", edit(f.admin, 4, "admin again"), http.StatusOK)
}

func TestAStaleSaveIsAnErrorOverHTTPAndTheOtherEditorKeepsTheirText(t *testing.T) {
	f := newWikiFixture(t)
	page := f.create(t, f.admin, "Plans", "v1")
	wantStatus(t, "first save", f.editor.patch("/api/wiki/pages/plans", map[string]any{"base_rev": 1, "content": "edda"}), http.StatusOK)
	res := f.admin.patch("/api/wiki/pages/plans", map[string]any{"base_rev": 1, "content": "admin"})
	wantStatus(t, "second save on the same revision", res, http.StatusConflict)
	if !strings.Contains(string(res.body), `"code":"STALE_REVISION"`) {
		t.Fatalf("body: %s", res.body)
	}
	var now wikiPage
	f.viewer.get("/api/wiki/pages/plans").decode(t, &now)
	if now.Content != "edda" || now.Rev != 2 || now.ID != page.ID {
		t.Fatalf("page: %+v", now)
	}
	wantStatus(t, "no base_rev", f.admin.patch("/api/wiki/pages/plans", map[string]any{"content": "x"}), http.StatusBadRequest)
	wantStatus(t, "unknown field", f.admin.patch("/api/wiki/pages/plans", map[string]any{"base_rev": 2, "html": "x"}), http.StatusBadRequest)
}

func TestTheWikiCanBeClosedToReadersAndOpenedAgain(t *testing.T) {
	f := newWikiFixture(t)
	f.create(t, f.admin, "Secret-ish", "x")
	everyone := everyoneID(t, f.admin)
	setEveryone := func(p perm.Set) {
		t.Helper()
		res := f.admin.patch("/api/admin/roles/"+everyone.String(), map[string]any{"permissions": int64(p)})
		wantStatus(t, "everyone", res, http.StatusOK)
	}
	setEveryone(perm.DefaultEveryone &^ perm.ViewWiki)
	for _, path := range []string{"/api/wiki", "/api/wiki/pages/secret-ish", "/api/wiki/search?q=ab"} {
		wantStatus(t, "viewer, wiki closed: "+path, f.viewer.get(path), http.StatusForbidden)
	}
	wantStatus(t, "admin still reads", f.admin.get("/api/wiki"), http.StatusOK)
	setEveryone(perm.DefaultEveryone)
	wantStatus(t, "open again", f.viewer.get("/api/wiki"), http.StatusOK)
}

func TestAGivenEditorStaysSubjectToTimeoutsAndTheNewMemberWait(t *testing.T) {
	f := newWikiFixture(t)
	wantStatus(t, "editor writes", f.editor.post("/api/wiki/pages", map[string]any{"title": "Before"}), http.StatusCreated)

	// A timeout takes editing away, not reading.
	wantStatus(t, "timeout", f.admin.post("/api/admin/users/"+f.editor.user.ID.String()+"/timeout", map[string]any{"duration_seconds": 3600, "reason": "calm down"}), http.StatusOK)
	wantStatus(t, "timed out editor writes", f.editor.post("/api/wiki/pages", map[string]any{"title": "During"}), http.StatusForbidden)
	wantStatus(t, "timed out editor reads", f.editor.get("/api/wiki"), http.StatusOK)
	wantStatus(t, "end the timeout", f.admin.delete("/api/admin/users/"+f.editor.user.ID.String()+"/timeout"), http.StatusNoContent)
	wantStatus(t, "after the timeout", f.editor.post("/api/wiki/pages", map[string]any{"title": "After"}), http.StatusCreated)

	// A timeout takes managing too: a timed-out manager cannot lock or delete.
	page := f.create(t, f.editor, "Managed", "text")
	wantStatus(t, "timeout manager", f.admin.post("/api/admin/users/"+f.manager.user.ID.String()+"/timeout", map[string]any{"duration_seconds": 3600, "reason": "calm down"}), http.StatusOK)
	wantStatus(t, "timed out manager locks", f.manager.put("/api/wiki/pages/"+page.Slug+"/lock", map[string]any{"locked": true}), http.StatusForbidden)
	wantStatus(t, "end the manager timeout", f.admin.delete("/api/admin/users/"+f.manager.user.ID.String()+"/timeout"), http.StatusNoContent)

	// Raid protection: a member who just joined waits before writing, the wiki included.
	wantStatus(t, "set the wait", putRaid(t, f.admin, 0, 60, 0, 0), http.StatusOK)
	res := f.editor.post("/api/wiki/pages", map[string]any{"title": "Too early"})
	wantStatus(t, "new editor", res, http.StatusForbidden)
	if !strings.Contains(string(res.body), "new members can write") {
		t.Fatalf("body %s", res.body)
	}
	wantStatus(t, "a new editor still reads", f.editor.get("/api/wiki"), http.StatusOK)
	wantStatus(t, "admins never wait", f.admin.post("/api/wiki/pages", map[string]any{"title": "Admin"}), http.StatusCreated)
	voucher := f.a.register(f.admin, "vouched")
	assignRoles(t, f.admin, voucher, createRole(t, f.admin, "vouchers", perm.EditWiki|perm.BypassNewMemberLimits))
	wantStatus(t, "bypass", voucher.post("/api/wiki/pages", map[string]any{"title": "Vouched"}), http.StatusCreated)
}

func TestNobodyCanChooseTheWikiPermissionsForThemselves(t *testing.T) {
	f := newWikiFixture(t)
	for _, p := range []perm.Set{perm.EditWiki, perm.ManageWiki, perm.EditWiki | perm.ViewWiki} {
		res := f.admin.post("/api/admin/roles", map[string]any{"name": "open " + p.MissingName(p), "permissions": int64(p), "self_assignable": true})
		wantStatus(t, "a self-assignable role with wiki powers", res, http.StatusBadRequest)
	}
	res := f.admin.post("/api/admin/roles", map[string]any{"name": "readers", "permissions": int64(perm.ViewWiki), "self_assignable": true})
	wantStatus(t, "reading is fine", res, http.StatusCreated)
}

func TestWikiChangesLeaveAuditEntries(t *testing.T) {
	f := newWikiFixture(t)
	p := f.create(t, f.editor, "Audited", "one")
	wantStatus(t, "edit", f.editor.patch("/api/wiki/pages/audited", map[string]any{"base_rev": 1, "content": "two"}), http.StatusOK)
	wantStatus(t, "restore", f.editor.post("/api/wiki/pages/audited/revisions/1/restore", map[string]any{"base_rev": 2}), http.StatusOK)
	wantStatus(t, "lock", f.manager.put("/api/wiki/pages/audited/lock", map[string]any{"locked": true}), http.StatusOK)
	wantStatus(t, "rename", f.manager.patch("/api/wiki/pages/audited", map[string]any{"slug": "audited-2"}), http.StatusOK)
	wantStatus(t, "delete", f.manager.delete("/api/wiki/pages/audited-2"), http.StatusOK)
	wantStatus(t, "undelete", f.manager.post("/api/wiki/pages/audited-2/undelete", nil), http.StatusOK)
	got := waitForAudit(t, f.admin, "limit=100", 7)
	want := map[string]int{"wiki.create": 1, "wiki.edit": 1, "wiki.restore": 2, "wiki.lock": 1, "wiki.move": 1, "wiki.delete": 1}
	for _, a := range got {
		want[a]--
	}
	for action, left := range want {
		if left > 0 {
			t.Errorf("no audit entry %s (got %v)", action, got)
		}
	}
	for _, e := range auditEntries(t, f.admin, "action=wiki.create").Entries {
		if e.TargetID != p.ID || e.TargetName != "Audited" {
			t.Errorf("the entry names the page: %+v", e)
		}
	}
}

func TestEditsOfOneMemberAreRateLimited(t *testing.T) {
	f := newWikiFixture(t)
	f.create(t, f.admin, "Busy", "0")
	limited := 0
	for i := 1; i <= wiki.EditsPerMinute+3; i++ {
		res := f.editor.post("/api/wiki/pages", map[string]any{"title": "Page " + string(rune('A'+i%26)) + strings.Repeat("x", i)})
		if res.status == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited != 3 {
		t.Fatalf("%d requests were limited, want 3", limited)
	}
	wantStatus(t, "reads are not limited", f.editor.get("/api/wiki"), http.StatusOK)
}

func TestErasingAMemberKeepsTheirWikiTextAndTheExportHoldsIt(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	author := a.register(admin, "wanda")
	assignRoles(t, admin, author, createRole(t, admin, "wiki editors", perm.EditWiki))
	res := author.post("/api/wiki/pages", map[string]any{"title": "Lore", "content": "what wanda wrote"})
	wantStatus(t, "create", res, http.StatusCreated)
	a.setEmail(author, "wanda@example.org")

	wantStatus(t, "request", a.requestExport(author, memberPassword, ""), http.StatusAccepted)
	a.waitForExport(author)
	files := unzipAll(t, author.get("/api/users/me/export/download").body)
	if !strings.Contains(files["wiki_revisions.json"], "what wanda wrote") || !strings.Contains(files["wiki_revisions.json"], `"page": "lore"`) {
		t.Fatalf("wiki_revisions.json: %s", files["wiki_revisions.json"])
	}

	wantStatus(t, "erase", a.deleteAccount(author, memberPassword, ""), http.StatusNoContent)
	var content string
	var editor, creator *string
	err := a.db.QueryRow(context.Background(), `
		SELECT p.content, p.updated_by::text, r.editor_id::text FROM wiki_pages p JOIN wiki_revisions r ON r.page_id = p.id WHERE p.slug = 'lore'`).Scan(&content, &creator, &editor)
	if err != nil || content != "what wanda wrote" || creator != nil || editor != nil {
		t.Fatalf("after erasure: %q %v %v %v", content, creator, editor, err)
	}
	var shown wikiPage
	admin.get("/api/wiki/pages/lore").decode(t, &shown)
	if shown.Content != "what wanda wrote" {
		t.Fatalf("the page stays: %+v", shown)
	}
}
