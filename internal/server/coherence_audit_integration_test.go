//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
)

func entriesOf(t *testing.T, f *messageModerationFixture, action string) []audit.Item {
	t.Helper()
	var out []audit.Item
	for _, e := range auditEntries(t, f.admin, "action="+action+"&limit=50").Entries {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

func TestReadingAnotherMembersEditHistoryIsAuditedOncePerReader(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	msg := f.a.send(f.user, f.channel, "first draft")
	f.edit(f.user, msg, "final text")

	for i := 0; i < 3; i++ {
		if res := f.history(f.mod, msg); res.status != http.StatusOK {
			t.Fatalf("history: %d", res.status)
		}
	}
	entries := entriesOf(t, f, "message.edit_history_view")
	if len(entries) != 1 {
		t.Fatalf("three reads by one moderator make one entry, got %d", len(entries))
	}
	e := entries[0]
	if e.ActorName != "msgmod" || e.TargetID != msg.String() || e.Details["author_name"] == nil {
		t.Errorf("entry: %+v", e)
	}
	if strings.Contains(string(mustJSON(e)), "first draft") {
		t.Error("the audit entry must not carry the text that was read")
	}

	// A second reader is a second entry.
	if res := f.history(f.admin, msg); res.status != http.StatusOK {
		t.Fatalf("owner history: %d", res.status)
	}
	if n := len(entriesOf(t, f, "message.edit_history_view")); n != 2 {
		t.Errorf("the Owner's read: %d entries", n)
	}
}

func TestReadingYourOwnEditHistoryLeavesNoEntry(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	msg := f.a.send(f.mod, f.channel, "mine")
	f.edit(f.mod, msg, "still mine")
	if res := f.history(f.mod, msg); res.status != http.StatusOK {
		t.Fatalf("history: %d", res.status)
	}
	if n := len(entriesOf(t, f, "message.edit_history_view")); n != 0 {
		t.Errorf("%d entries for a moderator reading their own message", n)
	}
}

func TestModeratorDeleteKeepsTheEarlierVersionsInTheAuditEntry(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	msg := f.a.send(f.user, f.channel, "first draft")
	f.edit(f.user, msg, "second draft")
	f.edit(f.user, msg, "final text")
	if res := f.mod.delete(f.path("/messages/" + msg.String())); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d", res.status)
	}
	var found *audit.Item
	for _, e := range entriesOf(t, f, "message.delete") {
		if e.TargetID == msg.String() {
			found = &e
		}
	}
	if found == nil {
		t.Fatal("no message.delete entry")
	}
	versions, _ := found.Details["edit_versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("edit_versions: %v", found.Details["edit_versions"])
	}
	newest, _ := versions[0].(map[string]any)
	oldest, _ := versions[1].(map[string]any)
	if newest["content"] != "second draft" || oldest["content"] != "first draft" {
		t.Errorf("versions newest first: %v", versions)
	}

	plain := f.a.send(f.user, f.channel, "never edited")
	if res := f.mod.delete(f.path("/messages/" + plain.String())); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d", res.status)
	}
	for _, e := range entriesOf(t, f, "message.delete") {
		if e.TargetID == plain.String() && e.Details["edit_versions"] != nil {
			t.Errorf("an unedited message has no versions: %v", e.Details)
		}
	}
}

func TestBulkDeleteKeepsEarlierVersionsToo(t *testing.T) {
	f := newMessageModerationFixture(t, true)
	msg := f.a.send(f.user, f.channel, "before")
	f.edit(f.user, msg, "after")
	res := f.mod.post(f.path("/messages/bulk-delete"), map[string]any{"user_id": f.user.user.ID, "count": 5})
	if res.status != http.StatusOK {
		t.Fatalf("bulk delete: %d %s", res.status, res.body)
	}
	for _, e := range entriesOf(t, f, "message.delete") {
		if e.TargetID == msg.String() {
			if v, _ := e.Details["edit_versions"].([]any); len(v) != 1 {
				t.Errorf("edit_versions: %v", e.Details["edit_versions"])
			}
			return
		}
	}
	t.Fatal("no message.delete entry")
}

func TestOnlyAModeratorEndingSomeoneElsesPollIsAudited(t *testing.T) {
	f := newPollFixture(t)
	own := f.basic(t, f.alice, nil)
	end := func(c *client, id uuid.UUID) response {
		return c.post(f.base()+"/messages/"+id.String()+"/poll/end", nil)
	}
	wantStatus(t, "author ends", end(f.alice, own.ID), http.StatusOK)
	foreign := f.basic(t, f.alice, nil)
	wantStatus(t, "bob may not", end(f.bob, foreign.ID), http.StatusForbidden)
	wantStatus(t, "owner ends", end(f.owner, foreign.ID), http.StatusOK)
	wantStatus(t, "ending twice changes nothing", end(f.owner, foreign.ID), http.StatusOK)

	var ends []audit.Item
	for _, e := range auditEntries(t, f.owner, "action=poll&limit=50").Entries {
		if e.Action == "poll.end" {
			ends = append(ends, e)
		}
	}
	if len(ends) != 1 || ends[0].TargetID != foreign.ID.String() || ends[0].Details["author_name"] == nil {
		t.Fatalf("poll.end entries: %+v", ends)
	}
}
