//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
)

func TestAGroupTakesMembersUpToTenAndEveryoneLeavesIt(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob, f.carol)
	if !dm.Group || len(dm.Members) != 3 {
		t.Fatalf("group: %+v", dm)
	}
	wantStatus(t, "carol writes", f.carol.dmPost(dm.ID, "hello all"), http.StatusCreated)
	wantStatus(t, "dave is out", f.dave.get("/api/channels/"+dm.ID.String()+"/messages"), http.StatusNotFound)
	members := "/api/dms/" + dm.ID.String() + "/members"

	wantStatus(t, "a stranger adds", f.dave.post(members, map[string]any{"user_id": f.dave.user.ID}), http.StatusNotFound)
	wantStatus(t, "alice adds dave", f.alice.post(members, map[string]any{"user_id": f.dave.user.ID}), http.StatusOK)
	wantStatus(t, "dave reads the history", f.dave.get("/api/channels/"+dm.ID.String()+"/messages"), http.StatusOK)
	wantStatus(t, "adding twice changes nothing", f.bob.post(members, map[string]any{"user_id": f.dave.user.ID}), http.StatusOK)

	// Up to ten: dave makes four, the rest are filled in directly.
	if _, err := f.a.db.Exec(context.Background(), `
		WITH extra AS (
		    INSERT INTO users (username, display_name, password_hash)
		    SELECT 'filler' || n, 'Filler ' || n, 'x' FROM generate_series(1, $2::int) n RETURNING id)
		INSERT INTO dm_members (channel_id, user_id) SELECT $1, id FROM extra`, dm.ID, chat.MaxDMMembers-4); err != nil {
		t.Fatal(err)
	}
	one := f.a.register(f.admin, "onetoomany")
	wantStatus(t, "eleventh", f.carol.post(members, map[string]any{"user_id": one.user.ID}), http.StatusConflict)

	// A pair is not a group.
	pair := f.bob.mustDM(t, f.carol)
	wantStatus(t, "add to a pair", f.bob.post("/api/dms/"+pair.ID.String()+"/members", map[string]any{"user_id": f.dave.user.ID}), http.StatusBadRequest)
	wantStatus(t, "leave a pair", f.bob.delete("/api/dms/"+pair.ID.String()+"/members/@me"), http.StatusBadRequest)

	// Leaving takes the history away; the last one out deletes the group.
	wantStatus(t, "dave leaves", f.dave.delete("/api/dms/"+dm.ID.String()+"/members/@me"), http.StatusNoContent)
	wantStatus(t, "dave is out again", f.dave.get("/api/channels/"+dm.ID.String()+"/messages"), http.StatusNotFound)
	wantStatus(t, "dave cannot leave twice", f.dave.delete("/api/dms/"+dm.ID.String()+"/members/@me"), http.StatusNotFound)
	for _, c := range []*client{f.alice, f.bob, f.carol} {
		wantStatus(t, "leave", c.delete("/api/dms/"+dm.ID.String()+"/members/@me"), http.StatusNoContent)
	}
	// The fillers stay in it, so it stays; with nobody left it goes.
	if _, err := f.a.db.Exec(context.Background(), `DELETE FROM dm_members WHERE channel_id = $1 AND user_id IN (SELECT id FROM users WHERE username LIKE 'filler%')`, dm.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM channels WHERE id = $1`, dm.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("channel rows %d %v", n, err)
	}
}

func TestNobodyAddsAMemberWhoBlockedTheInviterOrClosedTheirDoor(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob, f.carol)
	members := "/api/dms/" + dm.ID.String() + "/members"
	wantStatus(t, "dave blocks alice", f.dave.put("/api/me/blocks/"+f.alice.user.ID.String(), nil), http.StatusNoContent)
	blocked := f.alice.post(members, map[string]any{"user_id": f.dave.user.ID})

	eve := f.a.register(f.admin, "eve")
	wantStatus(t, "eve closes", eve.put("/api/users/me/privacy", map[string]any{"dm_policy": "nobody"}), http.StatusOK)
	closed := f.alice.post(members, map[string]any{"user_id": eve.user.ID})

	if blocked.status != http.StatusForbidden || closed.status != http.StatusForbidden || blocked.errorBody(t) != closed.errorBody(t) {
		t.Fatalf("blocked %d %s, closed %d %s", blocked.status, blocked.body, closed.status, closed.body)
	}
	// A block between two members of a group does not end the group.
	wantStatus(t, "bob blocks carol", f.bob.put("/api/me/blocks/"+f.carol.user.ID.String(), nil), http.StatusNoContent)
	wantStatus(t, "carol still writes", f.carol.dmPost(dm.ID, "hi"), http.StatusCreated)
	wantStatus(t, "bob still writes", f.bob.dmPost(dm.ID, "hi"), http.StatusCreated)
}

func TestAMemberIsInAtMostOneHundredGroups(t *testing.T) {
	f := newDMFixture(t, false)
	for i := range chat.MaxGroupDMsPerMember {
		if _, err := f.a.db.Exec(context.Background(), `
			WITH c AS (INSERT INTO channels (number, name, type, dm_created_by) OVERRIDING SYSTEM VALUE
			           VALUES (nextval('dm_number_seq'), '', 'dm', $2) RETURNING id)
			INSERT INTO dm_members (channel_id, user_id) SELECT id, $1 FROM c`, f.alice.user.ID, f.bob.user.ID); err != nil {
			t.Fatal(i, err)
		}
	}
	res := f.carol.post("/api/dms", map[string]any{"user_ids": []uuid.UUID{f.alice.user.ID, f.dave.user.ID}})
	if res.status != http.StatusForbidden {
		t.Errorf("starting a group with a member who is in 100: %d %s", res.status, res.body)
	}
	wantStatus(t, "the member themselves", f.alice.post("/api/dms", map[string]any{"user_ids": []uuid.UUID{f.carol.user.ID, f.dave.user.ID}}), http.StatusConflict)
	// A pair is not a group.
	wantStatus(t, "a pair", f.carol.startDM(f.alice), http.StatusCreated)
}

func TestConversationsReachTheirMembersOnlyAndOnlyThoseWhoAskedForThem(t *testing.T) {
	f := newDMFixture(t, true)
	owner := f.admin.dialLiveWSQuery(f.a.origin(), "?dm=1")
	stranger := f.carol.dialLiveWSQuery(f.a.origin(), "?dm=1")
	bobNew := f.bob.dialLiveWSQuery(f.a.origin(), "?dm=1")
	bobOld := f.bob.dialLiveWS(f.a.origin())
	aliceNew := f.alice.dialLiveWSQuery(f.a.origin(), "?dm=1")

	dm := f.alice.mustDM(t, f.bob)
	if _, ok := bobNew.expect(events.DMCreated, 3*time.Second); !ok {
		t.Fatal("bob's current client is told about the conversation")
	}
	if _, ok := aliceNew.expect(events.DMCreated, 3*time.Second); !ok {
		t.Fatal("the starter's own sessions are told too")
	}
	wantStatus(t, "write", f.alice.dmPost(dm.ID, "private words"), http.StatusCreated)
	body, ok := bobNew.expect(events.MessageCreate, 3*time.Second)
	if !ok || !strings.Contains(string(body), "private words") {
		t.Fatalf("a member's current client gets the message: %s", body)
	}
	for name, ws := range map[string]*wsConn{"the Owner": owner, "another member": stranger, "a client from before conversations": bobOld} {
		if body, got := ws.expect(events.MessageCreate, 700*time.Millisecond); got {
			t.Errorf("%s received a conversation's message: %s", name, body)
		}
	}
	// Typing is a conversation event too.
	aliceNew.send(events.ClientTyping, map[string]any{"channel_id": dm.ID})
	if _, ok := bobNew.expect(events.Typing, 3*time.Second); !ok {
		t.Error("the other member sees typing")
	}
	if _, got := owner.expect(events.Typing, 500*time.Millisecond); got {
		t.Error("the Owner saw typing in a conversation")
	}
}

func TestOnlyMembersSearchTheirConversationsAndChannelsNeverShowThem(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	wantStatus(t, "dm", f.alice.dmPost(dm.ID, "needle in private"), http.StatusCreated)
	ch := f.a.createChannel(f.admin, "general", "text")
	f.a.send(f.alice, ch, "needle in public")

	count := func(c *client, query string) (int, string) {
		res := c.get("/api/search?" + query)
		wantStatus(t, "search "+query, res, http.StatusOK)
		var out struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		res.decode(t, &out)
		return len(out.Messages), string(res.body)
	}
	if n, _ := count(f.bob, "q=needle&dm=true"); n != 1 {
		t.Errorf("a member finds the private message: %d", n)
	}
	for name, c := range map[string]*client{"another member": f.carol, "the Owner": f.admin} {
		if n, body := count(c, "q=needle&dm=true"); n != 0 {
			t.Errorf("%s finds a private message: %s", name, body)
		}
		if n, body := count(c, "q=needle"); n != 1 || strings.Contains(body, "private") {
			t.Errorf("%s finds %d in channels: %s", name, n, body)
		}
		if res := c.get("/api/search?q=needle&dm=true&channel_id=" + dm.ID.String()); res.status != http.StatusNotFound {
			t.Errorf("%s searching a conversation by id: %d", name, res.status)
		}
	}
	if n, body := count(f.bob, "q=needle"); n != 1 || strings.Contains(body, "private") {
		t.Errorf("a member's channel search leaves conversations out: %s", body)
	}
}

func TestAReportOfADirectMessageIsTheOnlyWayInForModerators(t *testing.T) {
	f := newDMFixture(t, false)
	mod := f.a.register(f.admin, "reportmod")
	assignRoles(t, f.admin, mod, createRole(t, f.admin, "readers", 1<<21))
	dm := f.alice.mustDM(t, f.bob)
	wantStatus(t, "alice", f.alice.dmPost(dm.ID, "context before"), http.StatusCreated)
	res := f.alice.dmPost(dm.ID, "you are awful")
	wantStatus(t, "alice again", res, http.StatusCreated)
	var bad uuid.UUID
	res.decodeID(t, &bad)
	wantStatus(t, "bob answers", f.bob.dmPost(dm.ID, "no, you"), http.StatusCreated)

	// The moderator cannot open it any way but through a report.
	wantStatus(t, "moderator reads", mod.get("/api/channels/"+dm.ID.String()+"/messages"), http.StatusNotFound)
	wantStatus(t, "moderator reports", mod.post("/api/messages/"+bad.String()+"/report", map[string]any{"category": "spam"}), http.StatusNotFound)
	wantStatus(t, "carol reports", f.carol.post("/api/messages/"+bad.String()+"/report", map[string]any{"category": "spam"}), http.StatusNotFound)

	rep := f.bob.post("/api/messages/"+bad.String()+"/report", map[string]any{"category": "harassment", "note": "see for yourself"})
	wantStatus(t, "bob reports", rep, http.StatusCreated)
	var filed struct{ ID uuid.UUID }
	rep.decode(t, &filed)
	var detail reportDetail
	mod.get("/api/admin/reports/"+filed.ID.String()).decode(t, &detail)
	if detail.Kind != "dm" || detail.ChannelID != nil || detail.Snapshot.Message.Content != "you are awful" || len(detail.Snapshot.Context) != 2 {
		t.Fatalf("report: %+v", detail)
	}
	// The author can take the message back; the report stands.
	wantStatus(t, "delete", f.alice.delete("/api/channels/"+dm.ID.String()+"/messages/"+bad.String()), http.StatusNoContent)
	mod.get("/api/admin/reports/"+filed.ID.String()).decode(t, &detail)
	if detail.Snapshot.Message.Content != "you are awful" {
		t.Errorf("the copy outlives the message: %+v", detail.Snapshot.Message)
	}
}

func TestTheAuditLogNeverKeepsAConversationsWords(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	res := f.alice.dmPost(dm.ID, "secret original")
	var id uuid.UUID
	res.decodeID(t, &id)
	wantStatus(t, "edit", f.alice.put("/api/channels/"+dm.ID.String()+"/messages/"+id.String(), map[string]any{"content": "secret edited"}), http.StatusOK)
	wantStatus(t, "delete", f.alice.delete("/api/channels/"+dm.ID.String()+"/messages/"+id.String()), http.StatusNoContent)

	body := string(f.admin.get("/api/admin/audit-log?limit=200").body)
	for _, text := range []string{"secret original", "secret edited", dm.ID.String()} {
		if strings.Contains(body, text) {
			t.Errorf("the audit log mentions %q", text)
		}
	}
	if strings.Contains(body, "message.delete") || strings.Contains(body, "message.edit") {
		t.Errorf("a conversation's edits and deletions are not audited: %s", body)
	}
}

func TestErasingAnAccountLeavesTheOthersTheirHalfAndDropsEmptyConversations(t *testing.T) {
	f := newDMFixture(t, false)
	pair := f.alice.mustDM(t, f.bob)
	wantStatus(t, "alice writes", f.alice.dmPost(pair.ID, "alice's private words"), http.StatusCreated)
	wantStatus(t, "bob answers", f.bob.dmPost(pair.ID, "bob's reply"), http.StatusCreated)
	only := f.dave.mustDM(t, f.carol)
	wantStatus(t, "dave writes", f.dave.dmPost(only.ID, "dave alone"), http.StatusCreated)

	wantStatus(t, "erase alice", f.a.deleteAccount(f.alice, "member-password-123", ""), http.StatusNoContent)
	res := f.bob.get("/api/channels/" + pair.ID.String() + "/messages")
	wantStatus(t, "bob reads", res, http.StatusOK)
	if strings.Contains(string(res.body), "alice's private words") || !strings.Contains(string(res.body), "bob's reply") {
		t.Errorf("bob keeps his words and loses alice's: %s", res.body)
	}
	wantStatus(t, "a conversation with an erased member is closed", f.bob.dmPost(pair.ID, "anyone?"), http.StatusForbidden)

	wantStatus(t, "erase dave", f.a.deleteAccount(f.dave, "member-password-123", ""), http.StatusNoContent)
	var left int
	// carol is still in the second conversation, so it stays for her alone.
	if err := f.a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM dm_members WHERE channel_id = $1`, only.ID).Scan(&left); err != nil || left != 1 {
		t.Fatalf("members left: %d %v", left, err)
	}
	wantStatus(t, "carol erases", f.a.deleteAccount(f.carol, "member-password-123", ""), http.StatusNoContent)
	if err := f.a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM channels WHERE id = $1`, only.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("the conversation nobody is in goes: %d %v", left, err)
	}
}

func TestTheExportListsConversationsByIdAndOwnMessagesOnly(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice, bob := a.register(admin, "alice"), a.register(admin, "bobby")
	dm := alice.mustDM(t, bob)
	wantStatus(t, "alice", alice.dmPost(dm.ID, "alice's words"), http.StatusCreated)
	wantStatus(t, "bob", bob.dmPost(dm.ID, "bob's words"), http.StatusCreated)

	wantStatus(t, "request", a.requestExport(alice, memberPassword, ""), http.StatusAccepted)
	a.waitForExport(alice)
	files := unzipAll(t, alice.get("/api/users/me/export/download").body)
	if !strings.Contains(files["direct_messages.json"], dm.ID.String()) || !strings.Contains(files["direct_messages.json"], bob.user.ID.String()) ||
		strings.Contains(files["direct_messages.json"], "bobby") {
		t.Errorf("direct_messages.json lists the pair by id: %s", files["direct_messages.json"])
	}
	if !strings.Contains(files["messages.json"], "alice's words") || strings.Contains(files["messages.json"], "bob's words") ||
		!strings.Contains(files["messages.json"], `"direct_message": true`) && !strings.Contains(files["messages.json"], `"direct_message":true`) {
		t.Errorf("messages.json holds her own words, marked as private: %s", files["messages.json"])
	}
}

func TestPublicNumbersShowNothingOfPrivateTalk(t *testing.T) {
	f := newDMFixture(t, false)
	ch := f.a.createChannel(f.admin, "general", "text")
	number := func(content string) int64 {
		res := f.alice.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": content})
		wantStatus(t, "post", res, http.StatusCreated)
		var m struct{ Number int64 }
		res.decode(t, &m)
		return m.Number
	}
	before := number("one")
	dm := f.alice.mustDM(t, f.bob)
	for range 2 {
		wantStatus(t, "private", f.alice.dmPost(dm.ID, "x"), http.StatusCreated)
		wantStatus(t, "private", f.bob.dmPost(dm.ID, "y"), http.StatusCreated)
	}
	if after := number("two"); after != before+1 {
		t.Errorf("public message numbers jumped from %d to %d across private messages", before, after)
	}
}
