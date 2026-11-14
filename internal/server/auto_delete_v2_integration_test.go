//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/channels"
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
		NextRunAt             time.Time `json:"next_run_at"`
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
		Attachments []struct {
			IsDeleted bool `json:"is_deleted"`
		}
	}
	f.alice.get(f.base()+"/messages").decode(t, &list)
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

// What the dry run counts is what the run then removes, for both scopes: a
// pinned message, a thread kept by a young reply and a file removed before
// are left out of both.
func TestTheDryRunCountsWhatTheRunRemoves(t *testing.T) {
	for _, scope := range []string{"all", "attachments"} {
		t.Run(scope, func(t *testing.T) {
			f := newAutoDeleteFixture(t)
			upload := func(content string) uuid.UUID {
				res := f.alice.upload(f.base()+"/upload", "file", "a.png", "image/png", pngBytes, map[string]string{"content": content})
				wantStatus(t, "upload", res, http.StatusCreated)
				var m struct{ ID uuid.UUID }
				res.decode(t, &m)
				return m.ID
			}
			plain := f.a.send(f.alice, f.channel, "old text")
			withFile, removedFile, pinned := upload("old file"), upload("file removed before"), upload("pinned")
			root := f.a.send(f.alice, f.channel, "old root")
			wantStatus(t, "young reply", f.bob.post(f.base()+"/messages", map[string]any{"content": "new", "parent_id": root}), http.StatusCreated)
			wantStatus(t, "pin", f.owner.do(http.MethodPut, f.base()+"/messages/"+pinned.String()+"/pin", nil, nil), http.StatusOK)
			for _, id := range []uuid.UUID{plain, withFile, removedFile, pinned, root} {
				f.ageHours(t, id, 5)
			}
			if _, err := f.a.db.Exec(t.Context(), `UPDATE media SET is_deleted = TRUE WHERE message_id = $1`, removedFile); err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"hours": 2, "scope": scope}
			var counts struct{ Messages, Attachments int }
			f.owner.post("/api/admin/channels/"+f.channel.String()+"/auto-delete/preview", body).decode(t, &counts)
			messagesBefore := f.a.countRows(`SELECT COUNT(*) FROM messages WHERE channel_id = $1`, f.channel)
			filesBefore := f.a.countRows(`SELECT COUNT(*) FROM media WHERE channel_id = $1 AND NOT is_deleted`, f.channel)
			wantStatus(t, "set", f.setHours(f.owner, f.channel, body), http.StatusOK)
			f.sweep(t)
			gotMessages := messagesBefore - f.a.countRows(`SELECT COUNT(*) FROM messages WHERE channel_id = $1`, f.channel)
			gotFiles := filesBefore - f.a.countRows(`SELECT COUNT(*) FROM media WHERE channel_id = $1 AND NOT is_deleted`, f.channel)
			if counts.Messages != gotMessages || counts.Attachments != gotFiles {
				t.Fatalf("dry run %+v, the run removed %d messages and %d files", counts, gotMessages, gotFiles)
			}
			want := map[string][2]int{"all": {3, 1}, "attachments": {0, 1}}[scope]
			if gotMessages != want[0] || gotFiles != want[1] {
				t.Fatalf("removed %d messages and %d files, want %v", gotMessages, gotFiles, want)
			}
			var audit struct{ Messages, Attachments int }
			row := f.a.db.QueryRow(t.Context(), `SELECT (details->>'messages')::int, (details->>'attachments')::int FROM audit_log WHERE action = 'channel.auto_delete_run'`)
			if err := row.Scan(&audit.Messages, &audit.Attachments); err != nil || audit.Messages != gotMessages || audit.Attachments != gotFiles {
				t.Fatalf("the run's audit entry %+v (%v) does not match", audit, err)
			}
		})
	}
}

// Older clients accept auto_delete_days up to 90: a longer period answers 90.
// A conversation never follows the community default.
func TestTheOlderDaysFieldStaysWithinWhatOlderClientsAccept(t *testing.T) {
	f := newAutoDeleteFixture(t)
	var ch core.Channel
	f.setHours(f.owner, f.channel, map[string]any{"hours": 8760}).decode(t, &ch)
	if ch.AutoDeleteDays != 90 || ch.AutoDeleteHours != 8760 {
		t.Fatalf("a year: days %d, hours %d", ch.AutoDeleteDays, ch.AutoDeleteHours)
	}
	f.setHours(f.owner, f.channel, map[string]any{"hours": 48, "scope": "attachments"}).decode(t, &ch)
	if ch.AutoDeleteDays != 0 {
		t.Fatalf("files only answers %d days, want 0: no message is deleted", ch.AutoDeleteDays)
	}
	wantStatus(t, "an empty body", f.setHours(f.owner, f.channel, map[string]any{}), http.StatusBadRequest)
	dm := f.alice.mustDM(t, f.bob)
	var follows bool
	if err := f.a.db.QueryRow(t.Context(), `SELECT follows FROM (SELECT `+core.AutoDeleteSQL("c")+` FROM channels c WHERE c.id = $1) s(hours, scope, follows)`, dm.ID).Scan(&follows); err != nil || follows {
		t.Fatalf("a conversation follows the default: %v %v", follows, err)
	}
}

// A conversation's call Talk is listed with the channels, and never follows
// the community default: its payload says off (review round 2, finding 6).
func TestACallTalkNeverFollowsTheCommunityDefault(t *testing.T) {
	w := newCallChatWorld(t)
	wantStatus(t, "default", w.owner.put("/api/admin/message-settings/auto-delete", map[string]any{"hours": 24}), http.StatusOK)
	var list struct {
		Uncategorized []core.Channel
		Categories    []core.Category
	}
	w.alice.get("/api/channels").decode(t, &list)
	found := false
	for _, c := range append(list.Uncategorized, flatten(list.Categories)...) {
		if c.ID != w.call {
			continue
		}
		found = true
		if c.AutoDeleteFollowsDefault || c.AutoDeleteHours != 0 || c.AutoDeleteDays != 0 {
			t.Fatalf("the call Talk: follows %v, hours %d, days %d", c.AutoDeleteFollowsDefault, c.AutoDeleteHours, c.AutoDeleteDays)
		}
	}
	if !found {
		t.Fatal("the call Talk is not in the member's channel list: the test checks nothing")
	}
}

// Changing the setting while a run is under way stops it at its next batch:
// the batch reads the setting again after its share lock (stillSet). The run
// lists the channel with a period, the change commits before the batch, and
// nothing is deleted (review round 2, finding 6).
func TestAChangeOfTheSettingDuringARunStopsIt(t *testing.T) {
	f := newAutoDeleteFixture(t)
	old := f.a.send(f.alice, f.channel, "old")
	f.age(t, old, 10)
	wantStatus(t, "7 days", f.set(f.owner, f.channel, 7), http.StatusOK)
	hook := &beforeStatement{match: "SELECT 1 FROM channels WHERE id = $1 FOR SHARE", fn: func() {
		if _, err := f.a.db.Exec(t.Context(), `UPDATE channels SET auto_delete_hours = 0 WHERE id = $1`, f.channel); err != nil {
			t.Error(err)
		}
	}}
	d := &channels.AutoDeleter{DB: hookedPool(t, f.a.db, hook), Events: f.a.events, Objects: f.a.store, Audit: &audit.Log{DB: f.a.db}}
	if err := d.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !f.exists(old) {
		t.Fatal("the run deleted a message after the setting was turned off")
	}
}

// A channel that was just created or duplicated answers with the period it
// follows, like every later read: the web client refuses a channel without a
// valid scope.
func TestANewChannelAnswersTheDefaultItFollows(t *testing.T) {
	f := newAutoDeleteFixture(t)
	wantStatus(t, "default", f.owner.put("/api/admin/message-settings/auto-delete", map[string]any{"hours": 48, "scope": "attachments"}), http.StatusOK)
	check := func(what string, res response, status int) uuid.UUID {
		t.Helper()
		wantStatus(t, what, res, status)
		var ch core.Channel
		res.decode(t, &ch)
		if ch.AutoDeleteHours != 48 || ch.AutoDeleteScope != core.AutoDeleteScopeAttachments || !ch.AutoDeleteFollowsDefault || ch.AutoDeleteDays != 0 {
			t.Fatalf("%s: %+v", what, ch)
		}
		return ch.ID
	}
	id := check("create", f.owner.post("/api/admin/channels", map[string]any{"name": "fresh", "type": "text"}), http.StatusCreated)
	check("duplicate", f.owner.post("/api/admin/channels/"+id.String()+"/duplicate", nil), http.StatusCreated)
}
