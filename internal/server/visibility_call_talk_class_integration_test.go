//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/perm"
)

// H-L6-07 (the class: code that treats a conversation by its type 'dm' and
// forgets that its call Talk is a voice channel with a dm_id). The probes
// below are the rows of the table in findings/H-L6-07.md that needed the same
// treatment as the conversation; the fixes go through core.PrivateChannelSQL.

// callChatWorld: two members and a moderator who reads reports, a call named
// after a canary word and a message of bob in the call's chat.
type callChatWorld struct {
	a          *app
	owner      *client
	alice, bob *client
	mod        *client
	conv, call uuid.UUID
	message    uuid.UUID
}

func newCallChatWorld(t *testing.T) callChatWorld {
	t.Helper()
	a := newApp(t, false)
	owner := a.seedAdmin()
	alice, bob, mod := a.register(owner, "alice"), a.register(owner, "bobby"), a.register(owner, "moder")
	assignRoles(t, owner, mod, createRole(t, owner, "Mods", perm.ManageReports|perm.ViewAuditLog))
	conv := alice.mustDM(t, bob).ID
	wantStatus(t, "the conversation works", alice.dmPost(conv, "hi"), http.StatusCreated)
	res := alice.post("/api/dms/"+conv.String()+"/talk", nil)
	wantStatus(t, "start a call", res, http.StatusCreated)
	var call struct {
		ID uuid.UUID `json:"channel_id"`
	}
	res.decode(t, &call)
	wantStatus(t, "name the call", alice.patch("/api/talks/"+call.ID.String(), map[string]any{"name": "okapicall"}), http.StatusOK)
	msg := bob.post("/api/channels/"+call.ID.String()+"/messages", map[string]any{"content": "okapiword in the call"})
	wantStatus(t, "write in the call", msg, http.StatusCreated)
	return callChatWorld{a: a, owner: owner, alice: alice, bob: bob, mod: mod, conv: conv, call: call.ID, message: idOf(t, msg)}
}

// H-L6-07 row "report kind" (known failure). A report of a message in a
// conversation is filed as kind dm and names no channel; one of a message in
// the conversation's call chat is filed as a channel report that names the
// call, so the moderators read its name and id.
//
// The assertion is inverted and turns red when the fix lands.
func TestKnownFailureAReportOfACallChatNamesTheCall(t *testing.T) {
	w := newCallChatWorld(t)
	wantStatus(t, "alice reports", w.alice.post("/api/messages/"+w.message.String()+"/report", map[string]any{"category": "spam"}), http.StatusCreated)
	list := w.mod.get("/api/admin/reports")
	wantStatus(t, "the moderator lists reports", list, http.StatusOK)
	if !strings.Contains(string(list.body), "okapicall") {
		t.Fatal("H-L6-07 (reports) no longer reproduces: the report names no call. Flip this test into a guard")
	}
	t.Logf("H-L6-07 still reproduces: %s", list.body)
}

// H-L6-07 row "auto-delete sweep" (known failure). A call Talk that kept an
// auto-delete setting from before the admin routes refused it is swept like a
// channel, and the run is audited (channel.auto_delete_run) with the call's id
// and name, which the Owner reads. Conversations are never swept.
//
// The assertion is inverted and turns red when the fix lands.
func TestKnownFailureTheAutoDeleteSweepAuditsACallTalk(t *testing.T) {
	w := newCallChatWorld(t)
	if _, err := w.a.db.Exec(t.Context(), `UPDATE channels SET auto_delete_days = 1 WHERE id = $1`, w.call); err != nil {
		t.Fatal(err)
	}
	if _, err := w.a.db.Exec(t.Context(), `UPDATE messages SET created_at = now() - interval '3 days' WHERE id = $1`, w.message); err != nil {
		t.Fatal(err)
	}
	d := &channels.AutoDeleter{DB: w.a.db, Events: w.a.events, Objects: w.a.store, Audit: &audit.Log{DB: w.a.db}}
	if err := d.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	log := w.owner.get("/api/admin/audit-log")
	wantStatus(t, "the Owner reads the log", log, http.StatusOK)
	if !strings.Contains(string(log.body), w.call.String()) {
		t.Fatal("H-L6-07 (auto-delete) no longer reproduces: the sweep leaves the call alone. Flip this test into a guard")
	}
}
