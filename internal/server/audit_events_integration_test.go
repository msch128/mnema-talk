//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

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

	byID := map[string]map[string]any{}
	var ownEntry, flaggedEntry, editEntry map[string]any
	for _, e := range auditEntries(t, admin, "action=message&limit=200").Entries {
		byID[e.TargetID] = e.Details
		switch e.TargetID {
		case own.String():
			ownEntry = e.Details
			if e.ActorName != "memb" {
				t.Errorf("self delete actor %q", e.ActorName)
			}
		case flagged.String():
			flaggedEntry = e.Details
			if e.ActorName != "moda" {
				t.Errorf("moderator delete actor %q", e.ActorName)
			}
		case edited.String():
			editEntry = e.Details
		}
	}
	if ownEntry == nil || ownEntry["content"] != "my own words" || ownEntry["moderator"] != false || ownEntry["channel_name"] != "general" || ownEntry["author_name"] != "memb" {
		t.Errorf("self delete entry %v", ownEntry)
	}
	if flaggedEntry == nil || flaggedEntry["moderator"] != true || !strings.Contains(flaggedEntry["content"].(string), "something rude") {
		t.Fatalf("moderator delete entry %v", flaggedEntry)
	}
	if !strings.Contains(flaggedEntry["content"].(string), "token=abc") {
		t.Error("deleted message text must be kept in full")
	}
	if replies, _ := flaggedEntry["replies"].([]any); len(replies) != 1 {
		t.Errorf("replies %v", flaggedEntry["replies"])
	}
	if editEntry == nil || editEntry["before"] != "before edit" || editEntry["after"] != "after edit" {
		t.Errorf("edit entry %v", editEntry)
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
	if e.ActorName != "maxi" || e.TargetName != "Lounge" || e.TargetID != voice.String() || e.ActorID == nil {
		t.Errorf("join entry %+v", e)
	}
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

	var failed, ok int
	var all strings.Builder
	for _, e := range auditEntries(t, admin, "action=auth&limit=200").Entries {
		all.WriteString(string(mustJSON(e)))
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
	if failed != 1 {
		t.Errorf("three quick failures made %d entries, want 1 (deduplicated)", failed)
	}
	if ok < 2 {
		t.Errorf("sign-ins recorded: %d", ok)
	}
	if strings.Contains(all.String(), "hunter2typo") || strings.Contains(all.String(), "wrong-password") {
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
