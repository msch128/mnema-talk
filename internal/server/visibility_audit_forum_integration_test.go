//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
)

// H-L6-01 (known failure). doc/moderation.md, "Access": entries about a
// channel are read only by holders of View audit log "who can view the
// entry's channel when they read", because "a private channel's name, topic
// and overwrites are private too". The forum actions are not in that class
// (audit.Classes has no "forum.%" row, so they fall to the auditors): a
// moderator's change to a post title in a private forum (forum.post_update,
// with title and previous_title) and a tag created there (forum.tag_create,
// with the tag's name) reach every holder of View audit log.
//
// The assertion is inverted: the test passes while the entries leak and turns
// red the day they stop, so the fix must flip it (and drop the matching rows
// of canaryKnownLeaks).
func TestKnownFailureForumAuditEntriesOfAPrivateForumReachEveryAuditor(t *testing.T) {
	f := newAccessFixture(t, false)
	forum := f.privateChannel(t, "staff-board", "forum")
	auditor := f.a.register(f.owner, "auditor")
	assignRoles(t, f.owner, auditor, createRole(t, f.owner, "Auditors", perm.ViewAuditLog))

	res := f.insider.post("/api/channels/"+forum.String()+"/posts", map[string]any{"title": "salary review okapifirst", "content": "body"})
	wantStatus(t, "post", res, http.StatusCreated)
	post := idOf(t, res)
	wantStatus(t, "retitle", f.owner.patch("/api/channels/"+forum.String()+"/posts/"+post.String(),
		map[string]any{"title": "salary review okapisecond"}), http.StatusOK)
	wantStatus(t, "tag", f.owner.post("/api/channels/"+forum.String()+"/tags", map[string]any{"name": "okapitag"}), http.StatusCreated)

	// The auditor cannot see the forum itself.
	wantStatus(t, "forum hidden", auditor.get("/api/channels/"+forum.String()+"/posts"), http.StatusNotFound)
	log := auditor.get("/api/admin/audit-log")
	wantStatus(t, "audit log", log, http.StatusOK)
	var leaked []string
	for _, word := range []string{"okapifirst", "okapisecond", "okapitag", forum.String()} {
		if strings.Contains(string(log.body), word) {
			leaked = append(leaked, word)
		}
	}
	if len(leaked) == 0 {
		t.Fatal("H-L6-01 no longer reproduces: the forum entries are hidden now. Flip this test into a guard and drop the rows of canaryKnownLeaks")
	}
	t.Logf("H-L6-01 still reproduces: an auditor without the forum reads %v", leaked)
}
