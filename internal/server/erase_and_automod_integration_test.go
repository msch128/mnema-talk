//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

func newForumPost(t *testing.T, c *client, forum uuid.UUID, title string) uuid.UUID {
	t.Helper()
	res := c.post("/api/channels/"+forum.String()+"/posts", map[string]any{"title": title, "content": "body of the post"})
	wantStatus(t, "forum post", res, http.StatusCreated)
	var m struct{ ID uuid.UUID }
	res.decode(t, &m)
	return m.ID
}

func TestAutomodScreensForumTitlesPollOptionsAndTitleEdits(t *testing.T) {
	f := newAutomodFixture(t)
	f.age(f.member)
	f.addRule(wordRule("words", "block", "spam"))
	forum := f.a.createChannel(f.admin, "ideas", "forum")
	base := "/api/channels/" + forum.String()

	wantStatus(t, "blocked title", f.member.post(base+"/posts", map[string]any{"title": "buy s͏pam now", "content": "fine"}), http.StatusBadRequest)
	post := newForumPost(t, f.member, forum, "A clean title")
	wantStatus(t, "blocked title edit", f.member.patch(base+"/posts/"+post.String(), map[string]any{"title": "spam"}), http.StatusBadRequest)
	wantStatus(t, "clean title edit", f.member.patch(base+"/posts/"+post.String(), map[string]any{"title": "Still clean"}), http.StatusOK)

	text := f.a.createChannel(f.admin, "talk", "text")
	poll := map[string]any{"question": "Which?", "options": []string{"fine", "SPAM"}}
	wantStatus(t, "blocked poll option", f.member.post("/api/channels/"+text.String()+"/polls", poll), http.StatusBadRequest)
	wantStatus(t, "blocked tag", f.admin.post(base+"/tags", map[string]any{"name": "ok"}), http.StatusCreated)
}

func TestForumAuthorNeedsSendMessagesAndAnOpenPostToRetitle(t *testing.T) {
	f := newAutomodFixture(t)
	forum := f.a.createChannel(f.admin, "ideas", "forum")
	base := "/api/channels/" + forum.String()
	post := newForumPost(t, f.member, forum, "Mine")
	edit := map[string]any{"title": "Renamed"}
	wantStatus(t, "author edit", f.member.patch(base+"/posts/"+post.String(), edit), http.StatusOK)

	wantStatus(t, "lock", f.mod.patch(base+"/posts/"+post.String(), map[string]any{"is_locked": true}), http.StatusOK)
	wantStatus(t, "author edit, locked", f.member.patch(base+"/posts/"+post.String(), edit), http.StatusForbidden)
	wantStatus(t, "moderator edit, locked", f.mod.patch(base+"/posts/"+post.String(), edit), http.StatusOK)
	wantStatus(t, "unlock", f.mod.patch(base+"/posts/"+post.String(), map[string]any{"is_locked": false}), http.StatusOK)

	wantStatus(t, "mute", putOverwrites(f.admin, "channels", forum, overwrite("role", f.everyoneRole, 0, perm.SendMessages)), http.StatusNoContent)
	wantStatus(t, "author edit without Send messages", f.member.patch(base+"/posts/"+post.String(), edit), http.StatusForbidden)
	wantStatus(t, "moderator edit", f.mod.patch(base+"/posts/"+post.String(), edit), http.StatusOK)
}

func TestEraseClearsForumTitlesAndImportNamesAndExportListsThem(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	forum := a.createChannel(admin, "ideas", "forum")
	post := newForumPost(t, alice, forum, "Alice private title")
	text := a.createChannel(admin, "talk", "text")
	res := admin.post("/api/channels/"+text.String()+"/polls", map[string]any{"question": "Secret?", "options": []string{"a", "b"}, "anonymous": true})
	wantStatus(t, "poll", res, http.StatusCreated)
	var pollMsg struct {
		ID   string `json:"id"`
		Poll struct {
			Options []struct {
				ID string `json:"id"`
			} `json:"options"`
		} `json:"poll"`
	}
	res.decode(t, &pollMsg)
	wantStatus(t, "vote", alice.put("/api/channels/"+text.String()+"/messages/"+pollMsg.ID+"/poll/votes", map[string]any{"option_ids": []string{pollMsg.Poll.Options[0].ID}}), http.StatusOK)
	ctx := context.Background()
	if _, err := a.db.Exec(ctx, `INSERT INTO discord_imports (template_code, created_by, created_by_name) VALUES ('abc', $1, 'Alice Admin')`, alice.user.ID); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, "request", a.requestExport(alice, memberPassword, ""), http.StatusAccepted)
	a.waitForExport(alice)
	files := unzipAll(t, alice.get("/api/users/me/export/download").body)
	if !strings.Contains(files["messages.json"], "Alice private title") {
		t.Errorf("messages.json misses the forum title: %s", files["messages.json"])
	}
	if !strings.Contains(files["poll_votes.json"], pollMsg.ID) {
		t.Errorf("poll_votes.json misses the anonymous participation: %s", files["poll_votes.json"])
	}
	if strings.Contains(files["poll_votes.json"], pollMsg.Poll.Options[0].ID) {
		t.Errorf("poll_votes.json reveals the choice: %s", files["poll_votes.json"])
	}

	wantStatus(t, "erase", a.deleteAccount(alice, memberPassword, ""), http.StatusNoContent)
	var title *string
	var name string
	if err := a.db.QueryRow(ctx, `SELECT title FROM messages WHERE id = $1`, post).Scan(&title); err != nil || title != nil {
		t.Errorf("forum title after erase: %v, %v", title, err)
	}
	if err := a.db.QueryRow(ctx, `SELECT created_by_name FROM discord_imports`).Scan(&name); err != nil || name != "" {
		t.Errorf("import name after erase: %q, %v", name, err)
	}
}

// The retraction compares with the snapshot handed to it, not with whatever the
// permission service remembers by the time it runs: a second change in between
// must not hide who lost the Talk with the first.
func TestRetractionUsesTheSnapshotFromBeforeTheChange(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	outsider := a.register(admin, "outsider")
	open := a.createChannel(admin, "open-talk", "voice")
	start := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	res := admin.post("/api/events", map[string]any{"title": "In open talk", "channel_id": open, "start_time": start})
	wantStatus(t, "create in talk", res, http.StatusCreated)
	var ev communityevents.CommunityEvent
	res.decode(t, &ev)

	ctx := context.Background()
	perms := perm.NewService(a.db)
	before, err := perms.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "hide talk", putOverwrites(admin, "channels", open, overwrite("role", everyoneID(t, admin), 0, perm.ViewChannel)), http.StatusNoContent)
	// Two more changes follow before the retraction runs: Previous() now
	// already knows the Talk as hidden.
	perms.Invalidate()
	if _, err := perms.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	perms.Invalidate()
	if perms.Previous().Channel(perm.Subject{ID: outsider.user.ID}, open).Has(perm.ViewChannel) {
		t.Fatal("test setup: the remembered snapshot should already be the changed one")
	}

	rec := &events.Recorder{}
	h := &communityevents.Handler{DB: a.db, Store: communityevents.NewStore(a.db), Events: rec, Perms: perms}
	if err := h.RetractHidden(ctx, before, nil); err != nil {
		t.Fatal(err)
	}
	for _, e := range rec.Snapshot() {
		if p, ok := e.Payload.(communityevents.EventDeletePayload); ok && p.ID == ev.ID {
			for _, r := range e.Recipients {
				if r == outsider.user.ID {
					return
				}
			}
		}
	}
	t.Fatal("the member who lost the Talk was not told to drop the event")
}
