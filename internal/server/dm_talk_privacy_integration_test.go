//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestWhatIsEditedOrDeletedInAConversationsTalkIsNotInTheAuditLog(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)
	msg := f.a.send(f.dmFixture.alice, talk, "private plans")
	base := "/api/channels/" + talk.String() + "/messages/" + msg.String()
	wantStatus(t, "edit", f.dmFixture.alice.put(base, map[string]any{"content": "other private plans"}), http.StatusOK)
	wantStatus(t, "delete", f.dmFixture.alice.delete(base), http.StatusNoContent)

	// A control: the same in an ordinary channel is audited, after the Talk's entries.
	open := f.a.createChannel(f.admin, "general", "text")
	ctrl := f.a.send(f.dmFixture.alice, open, "public plans")
	wantStatus(t, "control delete", f.dmFixture.alice.delete("/api/channels/"+open.String()+"/messages/"+ctrl.String()), http.StatusNoContent)
	waitForAudit(t, f.admin, "action=message.delete", 1)
	for _, e := range auditEntries(t, f.admin, "limit=200").Entries {
		if e.TargetID == msg.String() || e.TargetID == talk.String() {
			t.Errorf("the Owner reads %s of a conversation's Talk", e.Action)
		}
	}
}

func TestNoRuleReportsWhatIsSaidInAConversationsTalk(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)
	af := &automodFixture{a: f.a, admin: f.admin}
	af.mod = f.admin
	af.addRule(wordRule("No spam", "flag", "spam"))
	wantStatus(t, "say it", f.dmFixture.alice.post("/api/channels/"+talk.String()+"/messages", map[string]any{"content": "spam spam"}), http.StatusCreated)
	if q := af.queue(); len(q) != 0 {
		t.Fatalf("the report queue holds the text of a call chat: %+v", q)
	}
}

func TestAdministratorsCannotAttachWebhooksToAConversationsTalk(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)
	for name, res := range map[string]response{
		"incoming": f.admin.post("/api/admin/channels/"+talk.String()+"/webhooks", map[string]any{"name": "spy"}),
		"outgoing": f.admin.post("/api/admin/channels/"+talk.String()+"/outgoing-webhooks", map[string]any{
			"name": "out", "url": "https://hooks.example.com/services/T000/B000/TOKEN", "events": []string{"MESSAGE_CREATE"},
		}),
	} {
		wantStatus(t, name, res, http.StatusNotFound)
	}
}

func TestManageMessagesDoesNotReachTheChatOfACall(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)
	assignRoles(t, f.admin, f.dmFixture.bob, createRole(t, f.admin, "mods", perm.ManageMessages))
	msg := f.a.send(f.dmFixture.alice, talk, "mine")
	wantStatus(t, "bob deletes alice's message", f.dmFixture.bob.delete("/api/channels/"+talk.String()+"/messages/"+msg.String()), http.StatusForbidden)
}

func TestAProfileTotalLeavesOutConversationsAndChannelsTheViewerCannotSee(t *testing.T) {
	f := newCallFixture(t)
	alice, bob := f.dmFixture.alice, f.dmFixture.bob
	open := f.a.createChannel(f.admin, "general", "text")
	secret := f.a.createChannel(f.admin, "secret", "text")
	wantStatus(t, "hide", putOverwrites(f.admin, "channels", secret, overwrite("member", bob.user.ID, 0, perm.ViewChannel)), http.StatusNoContent)
	f.a.send(alice, open, "hi")
	f.a.send(alice, secret, "hidden from bob")
	wantStatus(t, "a conversation message", alice.dmPost(f.dm.ID, "private"), http.StatusCreated)

	var prof struct {
		MessageCount int64 `json:"message_count"`
	}
	bob.get("/api/users/"+alice.user.ID.String()).decode(t, &prof)
	var members []struct {
		ID           uuid.UUID `json:"id"`
		MessageCount int64     `json:"message_count"`
	}
	bob.get("/api/members").decode(t, &members)
	for _, m := range members {
		if m.ID == alice.user.ID && (m.MessageCount != 1 || prof.MessageCount != m.MessageCount) {
			t.Fatalf("member list shows %d, profile shows %d, want 1", m.MessageCount, prof.MessageCount)
		}
	}
}

func TestTimeInAConversationsCallIsNotInThePublicVoiceTotal(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)
	conn, _, err := f.dmFixture.alice.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	conn.send(events.ClientVoiceJoin, map[string]any{"channel_id": talk})
	if _, ok := f.bob.expect(events.DMCallRing, 3*time.Second); !ok {
		t.Fatal("no ring")
	}
	time.Sleep(1200 * time.Millisecond)
	conn.send(events.ClientVoiceLeave, nil)
	if _, ok := f.bob.expect(events.DMCallEnd, 5*time.Second); !ok {
		t.Fatal("the call did not end")
	}
	time.Sleep(500 * time.Millisecond)
	var secs int64
	if err := f.a.db.QueryRow(context.Background(), `SELECT voice_seconds FROM users WHERE id = $1`, f.dmFixture.alice.user.ID).Scan(&secs); err != nil || secs != 0 {
		t.Fatalf("voice_seconds = %d (%v), want 0", secs, err)
	}
}

// The call is private from the moment the seat is taken: when the
// conversation is deleted mid-call (its Talk goes with it), the stay is still
// kept out of the public voice total.
func TestTimeInACallWhoseConversationWasDeletedMidCallIsNotInThePublicVoiceTotal(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)
	conn, _, err := f.dmFixture.alice.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	conn.send(events.ClientVoiceJoin, map[string]any{"channel_id": talk})
	if _, ok := f.bob.expect(events.DMCallRing, 3*time.Second); !ok {
		t.Fatal("no ring")
	}
	time.Sleep(1200 * time.Millisecond)
	ctx := context.Background()
	if _, err := f.a.db.Exec(ctx, `DELETE FROM channels WHERE id = $1`, talk); err != nil {
		t.Fatal(err)
	}
	f.a.router.Hub.Perms.Invalidate()
	conn.send(events.ClientVoiceLeave, nil)
	time.Sleep(time.Second)
	var secs int64
	if err := f.a.db.QueryRow(ctx, `SELECT voice_seconds FROM users WHERE id = $1`, f.dmFixture.alice.user.ID).Scan(&secs); err != nil || secs != 0 {
		t.Fatalf("voice_seconds = %d (%v), want 0", secs, err)
	}
}

func TestAThreadReplyInACallChatTellsTheRootsAuthorTheyFollowIt(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)
	root := f.a.send(f.dmFixture.alice, talk, "root")
	wantStatus(t, "reply", f.dmFixture.bob.post("/api/channels/"+talk.String()+"/messages", map[string]any{"content": "reply", "parent_id": root.String()}), http.StatusCreated)
	if _, ok := f.alice.expect(events.ThreadFollowUpdated, 5*time.Second); !ok {
		t.Fatal("the root's author was not told about following the thread")
	}
}
