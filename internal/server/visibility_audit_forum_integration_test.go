//go:build integration

package server

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/perm"
)

// H-L6-01. doc/moderation.md, "Access": entries about a channel are read only
// by holders of View audit log "who can view the entry's channel when they
// read", because "a private channel's name, topic and overwrites are private
// too". The forum actions are in that class: a moderator's change to a post
// title in a private forum (forum.post_update, with title and
// previous_title) and a tag created there (forum.tag_create, with the tag's
// name) reach only auditors who can view the forum, and the Owner.
func TestForumAuditEntriesOfAPrivateForumReachOnlyAuditorsWhoSeeIt(t *testing.T) {
	f := newAccessFixture(t, false)
	forum := f.privateChannel(t, "staff-board", "forum")
	auditors := createRole(t, f.owner, "Auditors", perm.ViewAuditLog)
	auditor := f.a.register(f.owner, "auditor")
	assignRoles(t, f.owner, auditor, auditors)
	assignRoles(t, f.owner, f.insider, f.staff, auditors)

	res := f.insider.post("/api/channels/"+forum.String()+"/posts", map[string]any{"title": "salary review okapifirst", "content": "body"})
	wantStatus(t, "post", res, http.StatusCreated)
	post := idOf(t, res)
	wantStatus(t, "retitle", f.owner.patch("/api/channels/"+forum.String()+"/posts/"+post.String(),
		map[string]any{"title": "salary review okapisecond"}), http.StatusOK)
	wantStatus(t, "tag", f.owner.post("/api/channels/"+forum.String()+"/tags", map[string]any{"name": "okapitag"}), http.StatusCreated)

	// The auditor cannot see the forum itself.
	wantStatus(t, "forum hidden", auditor.get("/api/channels/"+forum.String()+"/posts"), http.StatusNotFound)
	words := []string{"okapifirst", "okapisecond", "okapitag", forum.String()}
	for who, c := range map[string]*client{"auditor": auditor, "staff auditor": f.insider, "owner": f.owner} {
		log := c.get("/api/admin/audit-log")
		wantStatus(t, who+" audit log", log, http.StatusOK)
		for _, word := range words {
			if got, want := strings.Contains(string(log.body), word), who != "auditor"; got != want {
				t.Errorf("%s reads %s: %v, want %v", who, word, got, want)
			}
		}
	}
}

// The listing classes stored rows when it reads them, by the same table as
// audit.ClassOf: rows written before a class changed follow the new class,
// a channel entry without a channel is hidden, and an action without a row of
// its own is shown to the Owner and Administrators only.
func TestAuditListingAppliesTheClassOfEveryStoredAction(t *testing.T) {
	f := newAccessFixture(t, false)
	open := f.a.createChannel(f.owner, "open-board", "forum")
	private := f.privateChannel(t, "staff-board", "forum")
	auditor := f.a.register(f.owner, "auditor")
	assignRoles(t, f.owner, auditor, createRole(t, f.owner, "Auditors", perm.ViewAuditLog))
	rows := []struct {
		action  string
		channel *uuid.UUID
		shown   bool
	}{
		{"forum.tag_create", &open, true},
		{"forum.tag_update", &private, false},
		{"forum.post_update", nil, false},
		{"something.new", nil, false},
		{"wiki.something_new", nil, false},
		{"moderation.note_add", nil, false},
		{"moderation.kick", nil, true},
		{"event.talk_edit", &open, true},
		{"event.edit", nil, true},
		{"channel.update", &private, false},
	}
	for _, r := range rows {
		if _, err := f.a.db.Exec(context.Background(), `INSERT INTO audit_log (action, target_type, target_id, target_name, details, channel_id)
			VALUES ($1, 'test', '', $1, '{}', $2)`, r.action, r.channel); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	for _, e := range auditEntries(t, auditor, "target_type=test&limit=200").Entries {
		seen = append(seen, e.Action)
	}
	for _, r := range rows {
		if got := slices.Contains(seen, r.action); got != r.shown {
			t.Errorf("%s (class %d): shown %v, want %v", r.action, audit.ClassOf(r.action), got, r.shown)
		}
	}
	if n := len(auditEntries(t, f.owner, "target_type=test&limit=200").Entries); n != len(rows) {
		t.Errorf("the Owner reads %d of %d entries", n, len(rows))
	}
}
