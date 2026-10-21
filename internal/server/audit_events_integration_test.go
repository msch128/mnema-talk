//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/perm"
)

func waitForAudit(t *testing.T, c *client, query string, n int) []string {
	t.Helper()
	var actions []string
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		actions = actions[:0]
		for _, e := range auditEntries(t, c, query+"&limit=200").Entries {
			actions = append(actions, e.Action)
		}
		if len(actions) >= n {
			return actions
		}
	}
	t.Fatalf("audit %q has %v, wanted %d entries", query, actions, n)
	return nil
}

type messageAuditEntries struct {
	own     map[string]any
	flagged map[string]any
	edited  map[string]any
}

func collectMessageAuditEntries(t *testing.T, entries []audit.Item, ownID, flaggedID, editedID string) messageAuditEntries {
	t.Helper()
	var res messageAuditEntries
	for _, e := range entries {
		switch e.TargetID {
		case ownID:
			res.own = e.Details
			if e.ActorName != "memb" {
				t.Errorf("self delete actor %q", e.ActorName)
			}
		case flaggedID:
			res.flagged = e.Details
			if e.ActorName != "moda" {
				t.Errorf("moderator delete actor %q", e.ActorName)
			}
		case editedID:
			res.edited = e.Details
		}
	}
	return res
}

func assertSelfDeleteAudit(t *testing.T, entry map[string]any) {
	t.Helper()
	if entry == nil {
		t.Fatal("missing self delete entry")
	}
	if entry["content"] != "my own words" || entry["moderator"] != false {
		t.Fatalf("unexpected content or moderator flag: %v", entry)
	}
	if entry["channel_name"] != "general" || entry["author_name"] != "memb" {
		t.Fatalf("unexpected channel or author name: %v", entry)
	}
}

func assertModeratorDeleteAudit(t *testing.T, entry map[string]any) {
	t.Helper()
	if entry == nil || entry["moderator"] != true {
		t.Fatalf("missing or invalid moderator delete entry: %v", entry)
	}
	content, _ := entry["content"].(string)
	if !strings.Contains(content, "something rude") || !strings.Contains(content, "token=abc") {
		t.Fatalf("deleted message text incomplete: %q", content)
	}
	ids, _ := entry["reply_ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("unexpected reply ids: %v", ids)
	}
	if strings.Contains(string(mustJSON(entry)), "a reply") {
		t.Error("reply text must not be copied into the parent's entry")
	}
}

func TestAuditKeepsDeletedAndEditedMessages(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	mod := a.register(admin, "moda")
	member := a.register(admin, "memb")
	assignRoles(t, admin, mod, createRole(t, admin, "mods", perm.ManageMessages))
	ch := a.createChannel(admin, "general", "text")

	own := a.send(member, ch, "my own words")
	if res := member.delete("/api/channels/" + ch.String() + "/messages/" + own.String()); res.status != http.StatusNoContent {
		t.Fatal(res.status)
	}
	flagged := a.send(member, ch, "something rude https://example.com/x?token=abc")
	reply := member.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "a reply", "parent_id": flagged})
	if reply.status != http.StatusCreated {
		t.Fatalf("reply: %d %s", reply.status, reply.body)
	}
	edited := a.send(member, ch, "before edit")
	if res := member.put("/api/channels/"+ch.String()+"/messages/"+edited.String(), map[string]any{"content": "after edit"}); res.status != http.StatusOK {
		t.Fatalf("edit: %d %s", res.status, res.body)
	}
	if res := mod.delete("/api/channels/" + ch.String() + "/messages/" + flagged.String()); res.status != http.StatusNoContent {
		t.Fatal(res.status)
	}

	collected := collectMessageAuditEntries(t, auditEntries(t, admin, "action=message&limit=200").Entries, own.String(), flagged.String(), edited.String())
	assertSelfDeleteAudit(t, collected.own)
	assertModeratorDeleteAudit(t, collected.flagged)
	if collected.edited == nil || collected.edited["before"] != "before edit" || collected.edited["after"] != "after edit" {
		t.Errorf("edit entry %v", collected.edited)
	}
}

func TestAuditRecordsTalkPresence(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()
	max := a.register(admin, "maxi")
	voice := a.createChannel(admin, "Lounge", "voice")
	conn, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	conn.send("voice_join", map[string]any{"channel_id": voice})
	waitForAudit(t, admin, "action=voice.join", 1)
	conn.send("voice_leave", nil)
	waitForAudit(t, admin, "action=voice.leave", 1)
	e := auditEntries(t, admin, "action=voice.join").Entries[0]
	if e.ActorName != "maxi" || e.TargetName != "Lounge" {
		t.Errorf("join actor or target name mismatch: %+v", e)
	}
	if e.TargetID != voice.String() || e.ActorID == nil {
		t.Errorf("join target ID or actor ID mismatch: %+v", e)
	}
}

func countAuthEvents(t *testing.T, entries []audit.Item) (failed, ok int, all string) {
	t.Helper()
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(string(mustJSON(e)))
		switch e.Action {
		case "auth.login_failed":
			failed++
			if e.TargetName != "dora" {
				t.Errorf("failure target %q", e.TargetName)
			}
		case "auth.login":
			ok++
		}
	}
	return failed, ok, b.String()
}

func TestAuditRecordsSignInsWithoutLeakingAttempts(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	a.register(admin, "dora")
	a.login("dora", "member-password-123")

	bad := a.anon()
	for i := 0; i < 3; i++ {
		bad.post("/api/auth/login", map[string]string{"username": "dora", "password": "wrong-password-1"})
	}
	// A name that is no account (it may be a password typed in the wrong
	// field) is not stored.
	bad.post("/api/auth/login", map[string]string{"username": "hunter2typo", "password": "wrong-password-1"})

	failed, ok, all := countAuthEvents(t, auditEntries(t, admin, "action=auth&limit=200").Entries)
	if failed != 1 {
		t.Errorf("three quick failures made %d entries, want 1 (deduplicated)", failed)
	}
	if ok < 2 {
		t.Errorf("sign-ins recorded: %d", ok)
	}
	if strings.Contains(all, "hunter2typo") || strings.Contains(all, "wrong-password") {
		t.Error("an attempted name or password reached the log")
	}
	hasRegister := false
	for _, e := range auditEntries(t, admin, "action=auth.register").Entries {
		hasRegister = hasRegister || e.TargetName == "dora"
	}
	if !hasRegister {
		t.Error("registration not recorded")
	}

	// Own-account events.
	dora := a.login("dora", "member-password-123")
	if res := dora.put("/api/auth/password", map[string]string{"current_password": "member-password-123", "new_password": "another-password-456"}); res.status != http.StatusNoContent {
		t.Fatalf("password change: %d %s", res.status, res.body)
	}
	if res := dora.post("/api/auth/logout-all", nil); res.status != http.StatusNoContent {
		t.Fatalf("logout-all: %d %s", res.status, res.body)
	}
	waitForAudit(t, admin, "action=auth.password_change", 1)
	waitForAudit(t, admin, "action=auth.logout_all", 1)
}
