//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// Auto-delete in hours, with the scope "attachments" and a community default
// (doc/timed-messages.md, "As a moderator" and "As an operator").

func (f autoDeleteFixture) setHours(c *client, channel uuid.UUID, body map[string]any) response {
	return c.put("/api/admin/channels/"+channel.String()+"/auto-delete", body)
}

func (f autoDeleteFixture) ageHours(t *testing.T, id uuid.UUID, hours int) {
	t.Helper()
	if _, err := f.a.db.Exec(t.Context(), `UPDATE messages SET created_at = now() - make_interval(hours => $2) WHERE id = $1`, id, hours); err != nil {
		t.Fatal(err)
	}
}

func TestAutoDeleteTakesHoursAndAnswersTheOlderDaysField(t *testing.T) {
	f := newAutoDeleteFixture(t)
	old := f.a.send(f.alice, f.channel, "three hours old")
	young := f.a.send(f.alice, f.channel, "one hour old")
	f.ageHours(t, old, 3)
	f.ageHours(t, young, 1)

	res := f.setHours(f.owner, f.channel, map[string]any{"hours": 2})
	wantStatus(t, "2 hours", res, http.StatusOK)
	var ch core.Channel
	res.decode(t, &ch)
	if ch.AutoDeleteHours != 2 || ch.AutoDeleteDays != 1 || ch.AutoDeleteScope != core.AutoDeleteScopeAll || ch.AutoDeleteFollowsDefault {
		t.Fatalf("channel: hours %d, days %d, scope %q, default %v", ch.AutoDeleteHours, ch.AutoDeleteDays, ch.AutoDeleteScope, ch.AutoDeleteFollowsDefault)
	}
	for _, bad := range []map[string]any{{"hours": -1}, {"hours": 8761}, {"hours": 2, "scope": "everything"}, {}} {
		wantStatus(t, "invalid", f.setHours(f.owner, f.channel, bad), http.StatusBadRequest)
	}
	f.sweep(t)
	if f.exists(old) || !f.exists(young) {
		t.Fatalf("after the sweep: old %v, young %v", f.exists(old), f.exists(young))
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'channel.auto_delete_run' AND details->>'hours' = '2' AND details->>'scope' = 'all'`); n != 1 {
		t.Fatalf("audit entries of the run: %d", n)
	}
	// Whole weeks answer in days, exactly.
	res = f.setHours(f.owner, f.channel, map[string]any{"hours": 14 * 24})
	res.decode(t, &ch)
	if ch.AutoDeleteDays != 14 {
		t.Fatalf("two weeks answer %d days", ch.AutoDeleteDays)
	}
}

func TestAutoDeleteOfFilesKeepsTheTextAndMarksTheFilesRemoved(t *testing.T) {
	f := newAutoDeleteFixture(t)
	res := f.alice.upload(f.base()+"/upload", "file", "old.png", "image/png", pngBytes, map[string]string{"content": "the words stay"})
	wantStatus(t, "upload", res, http.StatusCreated)
	var m struct{ ID uuid.UUID }
	res.decode(t, &m)
	pinned := f.alice.upload(f.base()+"/upload", "file", "kept.png", "image/png", pngBytes, nil)
	var p struct{ ID uuid.UUID }
	pinned.decode(t, &p)
	wantStatus(t, "pin", f.owner.do(http.MethodPut, f.base()+"/messages/"+p.ID.String()+"/pin", nil, nil), http.StatusOK)
	f.age(t, m.ID, 2)
	f.age(t, p.ID, 2)

	preview := f.owner.post("/api/admin/channels/"+f.channel.String()+"/auto-delete/preview", map[string]any{"hours": 24, "scope": "attachments"})
	wantStatus(t, "preview", preview, http.StatusOK)
	var counts struct {
		Messages, Attachments int
		NextRunAt            time.Time `json:"next_run_at"`
	}
	preview.decode(t, &counts)
	if counts.Messages != 0 || counts.Attachments != 1 || !counts.NextRunAt.After(time.Now()) {
		t.Fatalf("preview: %+v", counts)
	}
	if !f.exists(m.ID) || f.a.countRows(`SELECT COUNT(*) FROM media WHERE is_deleted`) != 0 {
		t.Fatal("the preview changed something")
	}
	wantStatus(t, "files only", f.setHours(f.owner, f.channel, map[string]any{"hours": 24, "scope": "attachments"}), http.StatusOK)
	f.sweep(t)
	if !f.exists(m.ID) {
		t.Fatal("the scope attachments deleted the message")
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM media WHERE message_id = $1 AND is_deleted`, m.ID); n != 1 {
		t.Fatalf("removed files of the old message: %d", n)
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM media WHERE message_id = $1 AND NOT is_deleted`, p.ID); n != 1 {
		t.Fatal("the pinned message lost its file")
	}
	var list []struct {
		ID          uuid.UUID
		Content     string
		Attachments []struct{ IsDeleted bool `json:"is_deleted"` }
	}
	f.alice.get(f.base() + "/messages").decode(t, &list)
	found := false
	for _, got := range list {
		if got.ID == m.ID {
			found = got.Content == "the words stay" && len(got.Attachments) == 1 && got.Attachments[0].IsDeleted
		}
	}
	if !found {
		t.Fatalf("the message does not keep its text with its file shown as removed: %+v", list)
	}
}

func TestTheCommunityDefaultReachesChannelsWithoutTheirOwnSetting(t *testing.T) {
	f := newAutoDeleteFixture(t)
	optOut := f.a.createChannel(f.owner, "keep-everything", "text")
	wantStatus(t, "opt out", f.setHours(f.owner, optOut, map[string]any{"hours": 0}), http.StatusOK)
	dm := f.alice.mustDM(t, f.bob)
	follows := f.a.send(f.alice, f.channel, "follows the default")
	kept := f.a.send(f.alice, optOut, "opted out")
	wantStatus(t, "dm", f.alice.dmPost(dm.ID, "private"), http.StatusCreated)
	if _, err := f.a.db.Exec(t.Context(), `UPDATE messages SET created_at = now() - interval '10 days'`); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, "a member", f.alice.put("/api/admin/message-settings/auto-delete", map[string]any{"hours": 24}), http.StatusForbidden)
	preview := f.owner.post("/api/admin/message-settings/auto-delete/preview", map[string]any{"hours": 24})
	wantStatus(t, "preview", preview, http.StatusOK)
	var counts struct{ Messages int }
	preview.decode(t, &counts)
	if counts.Messages != 1 {
		t.Fatalf("the default would delete %d messages, want the one that follows it", counts.Messages)
	}
	res := f.owner.put("/api/admin/message-settings/auto-delete", map[string]any{"hours": 24})
	wantStatus(t, "default", res, http.StatusOK)
	var s struct {
		Hours int    `json:"auto_delete_default_hours"`
		Scope string `json:"auto_delete_default_scope"`
	}
	res.decode(t, &s)
	if s.Hours != 24 || s.Scope != "all" {
		t.Fatalf("settings: %+v", s)
	}
	var list struct {
		Uncategorized []core.Channel
		Categories    []core.Category
	}
	f.alice.get("/api/channels").decode(t, &list)
	for _, c := range append(list.Uncategorized, flatten(list.Categories)...) {
		if c.ID == f.channel && (!c.AutoDeleteFollowsDefault || c.AutoDeleteHours != 24 || c.AutoDeleteDays != 1) {
			t.Fatalf("a channel that follows the default: %+v", c)
		}
	}
	f.sweep(t)
	if f.exists(follows) || !f.exists(kept) {
		t.Fatalf("follows the default %v, opted out %v", f.exists(follows), f.exists(kept))
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM messages WHERE channel_id = $1`, dm.ID); n != 1 {
		t.Fatalf("the default reached a conversation: %d left", n)
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'settings.auto_delete_default_set'`); n != 1 {
		t.Fatalf("audit entries of the default: %d", n)
	}
	wantStatus(t, "back to the default", f.setHours(f.owner, optOut, map[string]any{"use_default": true}), http.StatusOK)
	f.sweep(t)
	if f.exists(kept) {
		t.Fatal("a channel that follows the default again kept its old messages")
	}
}
