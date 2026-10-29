//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

type historyFixture struct {
	a                                     *app
	admin, mod, mod2, timeoutOnly, deputy *client
	auditor, member, other                *client
	general                               uuid.UUID
}

func newHistoryFixture(t *testing.T) *historyFixture {
	t.Helper()
	a := newApp(t, false)
	admin := a.seedAdmin()
	f := &historyFixture{a: a, admin: admin}
	f.mod, f.mod2 = a.register(admin, "moddy"), a.register(admin, "moddytwo")
	f.timeoutOnly, f.deputy = a.register(admin, "hammer"), a.register(admin, "deputy")
	f.auditor, f.member, f.other = a.register(admin, "auditor"), a.register(admin, "chatty"), a.register(admin, "bystander")
	readers := createRole(t, admin, "queue readers", perm.ManageReports)
	assignRoles(t, admin, f.mod, readers)
	assignRoles(t, admin, f.mod2, readers)
	assignRoles(t, admin, f.timeoutOnly, createRole(t, admin, "hammers", perm.TimeoutMembers|perm.BanMembers|perm.KickMembers|perm.ManageMembers|perm.ManageMessages))
	assignRoles(t, admin, f.deputy, createRole(t, admin, "deputies", perm.Administrator))
	assignRoles(t, admin, f.auditor, createRole(t, admin, "auditors", perm.ViewAuditLog))
	f.general = a.createChannel(admin, "general", "text")
	return f
}

func (f *historyFixture) path(c *client, rest string) string {
	return "/api/admin/users/" + c.user.ID.String() + "/moderation" + rest
}

type historyView struct {
	Strikes int `json:"strikes"`
	Notes   []struct {
		ID         uuid.UUID `json:"id"`
		AuthorName string    `json:"author_name"`
		Body       string    `json:"body"`
		Strike     bool      `json:"strike"`
		Mine       bool      `json:"mine"`
		CanDelete  bool      `json:"can_delete"`
	} `json:"notes"`
	Events []struct {
		Kind            string   `json:"kind"`
		Actor           string   `json:"actor"`
		Reason          string   `json:"reason"`
		DurationSeconds int      `json:"duration_seconds"`
		AddedRoles      []string `json:"added_roles"`
		RemovedRoles    []string `json:"removed_roles"`
		Category        string   `json:"category"`
		Rule            string   `json:"rule"`
		Outcome         string   `json:"outcome"`
	} `json:"events"`
}

func (f *historyFixture) history(c *client, about *client) historyView {
	f.a.t.Helper()
	res := c.get(f.path(about, ""))
	wantStatus(f.a.t, "history", res, http.StatusOK)
	var h historyView
	res.decode(f.a.t, &h)
	return h
}

func (f *historyFixture) addNote(c *client, about *client, body string, strike bool) uuid.UUID {
	f.a.t.Helper()
	res := c.post(f.path(about, "/notes"), map[string]any{"body": body, "strike": strike})
	wantStatus(f.a.t, "add note", res, http.StatusCreated)
	var n struct{ ID uuid.UUID }
	res.decode(f.a.t, &n)
	return n.ID
}

func TestModerationHistoryAndNotesFollowTheReportPermissionOnEveryRoute(t *testing.T) {
	f := newHistoryFixture(t)
	note := f.addNote(f.mod, f.member, "first warning", true)
	notePath := f.path(f.member, "/notes/"+note.String())
	body := map[string]any{"body": "text", "strike": false}
	calls := map[string]func(c *client) response{
		"history": func(c *client) response { return c.get(f.path(f.member, "")) },
		"add":     func(c *client) response { return c.post(f.path(f.member, "/notes"), body) },
		"edit":    func(c *client) response { return c.put(notePath, body) },
		"delete":  func(c *client) response { return c.delete(notePath) },
	}
	for name, call := range calls {
		wantStatus(t, "anonymous "+name, call(f.a.anon()), http.StatusUnauthorized)
		wantStatus(t, "plain member "+name, call(f.other), http.StatusForbidden)
		wantStatus(t, "the member themselves "+name, call(f.member), http.StatusForbidden)
		// Timeouts, bans, kicks and message moderation are not enough.
		wantStatus(t, "other moderator permissions "+name, call(f.timeoutOnly), http.StatusForbidden)
		wantStatus(t, "audit log reader "+name, call(f.auditor), http.StatusForbidden)
	}
	wantStatus(t, "reader history", calls["history"](f.mod2), http.StatusOK)
	wantStatus(t, "reader add", calls["add"](f.mod2), http.StatusCreated)
	wantStatus(t, "deputy history", calls["history"](f.deputy), http.StatusOK)
	wantStatus(t, "owner history", calls["history"](f.admin), http.StatusOK)
	wantStatus(t, "author edit", calls["edit"](f.mod), http.StatusOK)
	wantStatus(t, "author delete", calls["delete"](f.mod), http.StatusNoContent)
	wantStatus(t, "deleted", calls["delete"](f.mod), http.StatusNotFound)
	wantStatus(t, "edit gone", calls["edit"](f.mod), http.StatusNotFound)

	// Nobody opens their own history, whatever they hold; bots and strangers have none.
	wantStatus(t, "moderator on self", f.mod.get(f.path(f.mod, "")), http.StatusForbidden)
	wantStatus(t, "owner on self", f.admin.get(f.path(f.admin, "")), http.StatusForbidden)
	wantStatus(t, "unknown member", f.mod.get("/api/admin/users/"+uuid.NewString()+"/moderation"), http.StatusNotFound)
	wantStatus(t, "bad id", f.mod.get("/api/admin/users/nope/moderation"), http.StatusBadRequest)
	// A note of another subject cannot be reached through this member's path.
	wantStatus(t, "wrong subject", f.mod.put(f.path(f.other, "/notes/"+f.addNote(f.mod, f.member, "x", false).String()), body), http.StatusNotFound)
}

func TestNotesAreEditedByTheirAuthorAndDeletedByTheirAuthorOrAnAdministrator(t *testing.T) {
	f := newHistoryFixture(t)
	mine := f.addNote(f.mod, f.member, "called me names", true)
	theirs := f.addNote(f.mod2, f.member, "ignored the warning", true)
	f.addNote(f.mod2, f.member, "just an observation", false)

	h := f.history(f.mod, f.member)
	if h.Strikes != 2 || len(h.Notes) != 3 {
		t.Fatalf("strikes %d notes %d", h.Strikes, len(h.Notes))
	}
	for _, n := range h.Notes {
		if n.Mine != (n.AuthorName == "moddy") || n.CanDelete != n.Mine {
			t.Errorf("rights on %q: %+v", n.Body, n)
		}
	}
	wantStatus(t, "edit another's", f.mod.put(f.path(f.member, "/notes/"+theirs.String()), map[string]any{"body": "mine now", "strike": false}), http.StatusForbidden)
	wantStatus(t, "administrators edit no one's", f.deputy.put(f.path(f.member, "/notes/"+theirs.String()), map[string]any{"body": "mine now", "strike": false}), http.StatusForbidden)
	wantStatus(t, "delete another's", f.mod.delete(f.path(f.member, "/notes/"+theirs.String())), http.StatusForbidden)
	wantStatus(t, "edit own", f.mod.put(f.path(f.member, "/notes/"+mine.String()), map[string]any{"body": "called me names twice", "strike": false}), http.StatusOK)
	if h = f.history(f.mod, f.member); h.Strikes != 1 {
		t.Fatalf("the strike was taken back by the edit: %d", h.Strikes)
	}
	for _, c := range []*client{f.deputy, f.admin} {
		h = f.history(c, f.member)
		for _, n := range h.Notes {
			if !n.CanDelete {
				t.Errorf("administrators may delete any note: %+v", n)
			}
		}
	}
	wantStatus(t, "administrator deletes another's", f.deputy.delete(f.path(f.member, "/notes/"+theirs.String())), http.StatusNoContent)
	wantStatus(t, "owner deletes another's", f.admin.delete(f.path(f.member, "/notes/"+mine.String())), http.StatusNoContent)

	for name, bad := range map[string]map[string]any{
		"empty": {"body": "  ", "strike": false}, "too long": {"body": strings.Repeat("x", 1001), "strike": false}, "unknown field": {"body": "x", "extra": 1},
	} {
		wantStatus(t, name, f.mod.post(f.path(f.member, "/notes"), bad), http.StatusBadRequest)
	}
}

func TestAMemberHoldsAtMostOneHundredNotes(t *testing.T) {
	f := newHistoryFixture(t)
	if _, err := f.a.db.Exec(context.Background(), `
		INSERT INTO moderation_notes (subject_id, author_id, body) SELECT $1, $2, 'n' || g FROM generate_series(1, 100) g`, f.member.user.ID, f.mod.user.ID); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "101st", f.mod.post(f.path(f.member, "/notes"), map[string]any{"body": "one more"}), http.StatusConflict)
}

func TestTheTimelineGathersWhatModeratorsAndAutomodDidAboutAMember(t *testing.T) {
	f := newHistoryFixture(t)
	target := f.member.user.ID.String()
	wantStatus(t, "timeout", f.timeoutOnly.post("/api/admin/users/"+target+"/timeout", map[string]any{"duration_seconds": 600, "reason": "cool off"}), http.StatusOK)
	wantStatus(t, "lift", f.timeoutOnly.delete("/api/admin/users/"+target+"/timeout"), http.StatusNoContent)
	wantStatus(t, "ban", f.timeoutOnly.post("/api/admin/users/"+target+"/ban", map[string]any{"reason": "evasion", "duration_seconds": 3600}), http.StatusNoContent)
	wantStatus(t, "unban", f.timeoutOnly.delete("/api/admin/users/"+target+"/ban"), http.StatusNoContent)
	f.member = f.a.login("chatty", memberPassword) // a ban ended their sessions
	role := createRole(t, f.admin, "regulars", 0)
	assignRoles(t, f.admin, f.member, role)
	// A report about them, by somebody else, and an automod hit.
	msg := f.a.send(f.member, f.general, "rude words")
	wantStatus(t, "report", f.other.post("/api/messages/"+msg.String()+"/report", map[string]any{"category": "harassment", "note": "mean"}), http.StatusCreated)
	wantStatus(t, "rule", f.admin.post("/api/admin/automod/rules", wordRule("No spam", "block", "spam")), http.StatusCreated)
	wantStatus(t, "hit", f.member.post("/api/channels/"+f.general.String()+"/messages", map[string]any{"content": "spam"}), http.StatusBadRequest)

	h := f.history(f.mod, f.member)
	got := map[string]int{}
	for _, e := range h.Events {
		got[e.Kind]++
	}
	for _, kind := range []string{"timeout", "timeout_removed", "ban", "unban", "roles", "report", "automod"} {
		if got[kind] != 1 {
			t.Errorf("kind %s: %d events (%+v)", kind, got[kind], got)
		}
	}
	for _, e := range h.Events {
		switch e.Kind {
		case "timeout":
			if e.Actor != "hammer" || e.Reason != "cool off" || e.DurationSeconds != 600 {
				t.Errorf("timeout: %+v", e)
			}
		case "ban":
			if e.Reason != "evasion" || e.DurationSeconds != 3600 {
				t.Errorf("ban: %+v", e)
			}
		case "roles":
			if e.Actor != "Admin" || len(e.AddedRoles) != 1 || e.AddedRoles[0] != "regulars" {
				t.Errorf("roles: %+v", e)
			}
		case "report":
			if e.Category != "harassment" || e.Rule != "" {
				t.Errorf("report: %+v", e)
			}
		case "automod":
			if e.Rule != "No spam" || e.Outcome != "blocked" {
				t.Errorf("automod: %+v", e)
			}
		}
	}
	for i := 1; i < len(h.Events); i++ {
		if h.Events[i].Kind == "" {
			t.Errorf("event without kind: %+v", h.Events[i])
		}
	}
	// What was said in the report stays in the queue, never in the timeline.
	if raw := f.mod.get(f.path(f.member, "")).body; strings.Contains(string(raw), "mean") || strings.Contains(string(raw), "rude words") {
		t.Fatalf("the timeline carries no report text: %s", raw)
	}
	// Nothing of this is about the other members.
	if len(f.history(f.mod, f.other).Events) != 0 {
		t.Fatal("the bystander has no history")
	}
}

func TestNotesNeverReachTheMemberTheirExportOrTheAuditReaders(t *testing.T) {
	f := newHistoryFixture(t)
	secret := "watch this one, secret-moderator-wording"
	f.addNote(f.mod, f.member, secret, true)

	// Not in anything the member can fetch about themselves.
	for _, path := range []string{"/api/users/me", "/api/members", "/api/users/me/export"} {
		if res := f.member.get(path); strings.Contains(string(res.body), "secret-moderator-wording") {
			t.Errorf("%s shows a note", path)
		}
	}
	if res := f.a.requestExport(f.member, memberPassword, ""); res.status != http.StatusAccepted {
		t.Fatalf("export: %d %s", res.status, res.body)
	}
	f.a.waitForExport(f.member)
	dl := f.member.get("/api/users/me/export/download")
	wantStatus(t, "download", dl, http.StatusOK)
	for name, content := range unzipAll(t, dl.body) {
		if strings.Contains(content, "secret-moderator-wording") || strings.Contains(strings.ToLower(content), "moderation_note") {
			t.Errorf("%s of the export holds moderator data", name)
		}
	}

	// The audit log says that a note was written, never what it says, and only
	// to the Owner and Administrators.
	var details string
	if err := f.a.db.QueryRow(context.Background(), `SELECT details::text FROM audit_log WHERE action = 'moderation.note_add'`).Scan(&details); err != nil {
		t.Fatalf("the note is audited: %v", err)
	}
	if strings.Contains(details, "secret-moderator-wording") {
		t.Fatalf("audit details: %s", details)
	}
	if res := f.auditor.get("/api/admin/audit-log?limit=100"); strings.Contains(string(res.body), "note_add") {
		t.Fatalf("auditors do not see notes: %s", res.body)
	}
	if res := f.admin.get("/api/admin/audit-log?limit=100"); !strings.Contains(string(res.body), "moderation.note_add") {
		t.Fatalf("the Owner sees them: %s", res.body)
	}
}

func TestNotesOutliveErasedAccountsUnderTheTombstone(t *testing.T) {
	f := newHistoryFixture(t)
	f.addNote(f.mod, f.member, "kept for the moderators", true)
	f.addNote(f.mod2, f.member, "written by somebody who leaves", false)

	// The author of a note erases their account: the note stays, its author is the tombstone.
	if res := f.a.deleteAccount(f.mod2, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("delete author: %d %s", res.status, res.body)
	}
	h := f.history(f.mod, f.member)
	names := map[string]bool{}
	for _, n := range h.Notes {
		names[n.AuthorName] = true
	}
	if len(h.Notes) != 2 || !names["Deleted member"] || !names["moddy"] {
		t.Fatalf("notes: %+v", h.Notes)
	}

	// The subject erases theirs: the notes about them stay, under the tombstone.
	id := f.member.user.ID
	if res := f.a.deleteAccount(f.member, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("delete subject: %d %s", res.status, res.body)
	}
	res := f.mod.get("/api/admin/users/" + id.String() + "/moderation")
	wantStatus(t, "history of a tombstone", res, http.StatusOK)
	if !strings.Contains(string(res.body), "kept for the moderators") {
		t.Fatalf("notes about an erased account stay: %s", res.body)
	}
	var shown string
	if err := f.a.db.QueryRow(context.Background(), `SELECT display_name FROM users WHERE id = $1`, id).Scan(&shown); err != nil || shown != "Deleted member" {
		t.Fatalf("the subject is the tombstone: %q %v", shown, err)
	}
}

func TestEveryHistoryRouteIsInTheAuditTableAndTheQueueStaysOpenToReaders(t *testing.T) {
	f := newHistoryFixture(t)
	id := f.addNote(f.mod, f.member, "x", false)
	wantStatus(t, "edit", f.mod.put(f.path(f.member, "/notes/"+id.String()), map[string]any{"body": "y"}), http.StatusOK)
	wantStatus(t, "delete", f.mod.delete(f.path(f.member, "/notes/"+id.String())), http.StatusNoContent)
	for _, action := range []string{"moderation.note_add", "moderation.note_edit", "moderation.note_delete"} {
		var n int
		if err := f.a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = $1 AND target_id = $2`, action, f.member.user.ID.String()).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: %d entries, %v", action, n, err)
		}
	}
}
