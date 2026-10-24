//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
)

type autoDeleteFixture struct {
	pollFixture
}

func newAutoDeleteFixture(t *testing.T) autoDeleteFixture {
	return autoDeleteFixture{newPollFixture(t)}
}

func (f autoDeleteFixture) set(c *client, channel uuid.UUID, days int) response {
	return c.put("/api/admin/channels/"+channel.String()+"/auto-delete", map[string]any{"days": days})
}

func (f autoDeleteFixture) age(t *testing.T, id uuid.UUID, days int) {
	t.Helper()
	if _, err := f.a.db.Exec(t.Context(), `UPDATE messages SET created_at = now() - make_interval(days => $2) WHERE id = $1`, id, days); err != nil {
		t.Fatal(err)
	}
}

func (f autoDeleteFixture) sweep(t *testing.T) {
	t.Helper()
	d := &chat.AutoDeleter{DB: f.a.db, Events: f.a.events, Objects: f.a.store, Audit: &audit.Log{DB: f.a.db}}
	if err := d.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func (f autoDeleteFixture) exists(id uuid.UUID) bool {
	return f.a.countRows(`SELECT COUNT(*) FROM messages WHERE id = $1`, id) == 1
}

func TestOnlyChannelManagersSetAutoDeleteAndOnlyToKnownPeriods(t *testing.T) {
	f := newAutoDeleteFixture(t)
	wantStatus(t, "member", f.set(f.alice, f.channel, 7), http.StatusForbidden)
	for _, days := range []int{-1, 2, 14, 365} {
		wantStatus(t, "invalid", f.set(f.owner, f.channel, days), http.StatusBadRequest)
	}
	res := f.set(f.owner, f.channel, 30)
	wantStatus(t, "owner", res, http.StatusOK)
	var ch chat.Channel
	res.decode(t, &ch)
	if ch.AutoDeleteDays != 30 {
		t.Fatalf("channel: %+v", ch)
	}
	var list chat.ChannelHierarchy
	f.alice.get("/api/channels").decode(t, &list)
	found := false
	for _, c := range append(list.Uncategorized, flatten(list.Categories)...) {
		if c.ID == f.channel {
			found = c.AutoDeleteDays == 30
		}
	}
	if !found {
		t.Fatalf("members do not see the setting: %+v", list)
	}
	wantStatus(t, "off", f.set(f.owner, f.channel, 0), http.StatusOK)

	// The change is in the audit log, with the before and after state.
	if n := f.a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'channel.auto_delete_set'`); n != 2 {
		t.Fatalf("audit entries for the setting: %d", n)
	}
	if f.a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'channel.auto_delete_set' AND details::text LIKE '%auto_delete_days%'`) == 0 {
		t.Fatal("the audit entry does not say what changed")
	}
}

func flatten(cats []chat.Category) []chat.Channel {
	var out []chat.Channel
	for _, c := range cats {
		out = append(out, c.Channels...)
	}
	return out
}

func TestAutoDeleteNeverAppliesToDirectMessages(t *testing.T) {
	f := newAutoDeleteFixture(t)
	dm := f.alice.mustDM(t, f.bob)
	wantStatus(t, "set on a DM", f.set(f.owner, dm.ID, 1), http.StatusNotFound)
	wantStatus(t, "set by the DM's member", f.set(f.alice, dm.ID, 1), http.StatusForbidden)

	wantStatus(t, "post", f.alice.dmPost(dm.ID, "old words"), http.StatusCreated)
	if _, err := f.a.db.Exec(t.Context(), `UPDATE messages SET created_at = now() - interval '400 days'`); err != nil {
		t.Fatal(err)
	}
	// Even a forced value is refused by the schema.
	if _, err := f.a.db.Exec(t.Context(), `UPDATE channels SET auto_delete_days = 1 WHERE id = $1`, dm.ID); err == nil {
		t.Fatal("the database accepted auto-delete on a direct message")
	}
	f.sweep(t)
	if n := f.a.countRows(`SELECT COUNT(*) FROM messages WHERE channel_id = $1`, dm.ID); n != 1 {
		t.Fatalf("direct messages left: %d", n)
	}
}

func TestSweeperDeletesExpiredMessagesAndKeepsTheRest(t *testing.T) {
	f := newAutoDeleteFixture(t)
	other := f.a.createChannel(f.owner, "keep", "text")
	old := f.a.send(f.alice, f.channel, "old")
	fresh := f.a.send(f.alice, f.channel, "fresh")
	pinned := f.a.send(f.alice, f.channel, "pinned and old")
	elsewhere := f.a.send(f.alice, other, "old but in a channel without the setting")
	for _, id := range []uuid.UUID{old, pinned, elsewhere} {
		f.age(t, id, 10)
	}
	wantStatus(t, "pin", f.owner.do(http.MethodPut, f.base()+"/messages/"+pinned.String()+"/pin", nil, nil), http.StatusOK)

	// Nothing happens before the opt-in.
	f.sweep(t)
	if !f.exists(old) {
		t.Fatal("a channel without the setting lost a message")
	}

	wantStatus(t, "7 days", f.set(f.owner, f.channel, 7), http.StatusOK)
	f.sweep(t)
	if f.exists(old) {
		t.Fatal("the expired message is still there")
	}
	for name, id := range map[string]uuid.UUID{"fresh": fresh, "pinned": pinned, "other channel": elsewhere} {
		if !f.exists(id) {
			t.Fatalf("%s message was deleted", name)
		}
	}

	var announced bool
	for _, e := range f.a.events.Snapshot() {
		if e.Type == events.MessageDelete {
			announced = true
		}
	}
	if !announced {
		t.Fatal("no message_delete event")
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'channel.auto_delete_run' AND channel_id = $1 AND details->>'messages' = '1'`, f.channel); n != 1 {
		t.Fatalf("audit entries of the run: %d", n)
	}
	// A run that removes nothing writes nothing.
	f.sweep(t)
	if n := f.a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'channel.auto_delete_run'`); n != 1 {
		t.Fatalf("an empty run was audited: %d", n)
	}
}

func TestSweeperKeepsAThreadWithARecentReply(t *testing.T) {
	f := newAutoDeleteFixture(t)
	root := f.a.send(f.alice, f.channel, "old root")
	res := f.bob.post(f.base()+"/messages", map[string]any{"content": "recent reply", "parent_id": root})
	wantStatus(t, "reply", res, http.StatusCreated)
	var rep struct{ ID uuid.UUID }
	res.decode(t, &rep)
	f.age(t, root, 10)
	wantStatus(t, "7 days", f.set(f.owner, f.channel, 7), http.StatusOK)
	f.sweep(t)
	if !f.exists(root) || !f.exists(rep.ID) {
		t.Fatal("a thread that is still being answered was cut")
	}
	f.age(t, rep.ID, 9)
	f.sweep(t)
	if f.exists(root) || f.exists(rep.ID) {
		t.Fatal("an entirely expired thread stayed")
	}
}

func TestSweeperRemovesAttachmentObjectsAndWorksInBatches(t *testing.T) {
	f := newAutoDeleteFixture(t)
	msg := f.a.send(f.alice, f.channel, "with a file")
	const key = "uploads/auto-delete-test.bin"
	f.a.store.Put(key, []byte("data"), time.Now())
	if _, err := f.a.db.Exec(t.Context(), `
		INSERT INTO media (uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, $3, 'b', $4, 'a.bin', 'application/octet-stream', 4)`, f.alice.user.ID, f.channel, msg, key); err != nil {
		t.Fatal(err)
	}
	f.age(t, msg, 3)
	// More than one batch of plain messages as well.
	if _, err := f.a.db.Exec(t.Context(), `
		INSERT INTO messages (channel_id, user_id, content, created_at)
		SELECT $1, $2, 'bulk ' || g, now() - interval '3 days' FROM generate_series(1, 450) g`, f.channel, f.alice.user.ID); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "1 day", f.set(f.owner, f.channel, 1), http.StatusOK)
	f.sweep(t)
	if n := f.a.countRows(`SELECT COUNT(*) FROM messages WHERE channel_id = $1`, f.channel); n != 0 {
		t.Fatalf("messages left: %d", n)
	}
	if f.a.store.Has(key) {
		t.Fatal("the attachment object was not deleted")
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'channel.auto_delete_run' AND details->>'messages' = '451' AND details->>'attachments' = '1'`); n != 1 {
		t.Fatalf("audit counts do not add up")
	}
}

func TestTurningAutoDeleteOffStopsTheSweeper(t *testing.T) {
	f := newAutoDeleteFixture(t)
	id := f.a.send(f.alice, f.channel, "old")
	f.age(t, id, 40)
	wantStatus(t, "30 days", f.set(f.owner, f.channel, 30), http.StatusOK)
	wantStatus(t, "off", f.set(f.owner, f.channel, 0), http.StatusOK)
	f.sweep(t)
	if !f.exists(id) {
		t.Fatal("deleted although the setting is off")
	}
}
