//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/forum"
	"github.com/msch128/mnema-talk/internal/chat/messages"
)

// Lane 7 of the defect hunt, follow-up probes of the shape behind H-L7-06 and
// H-L7-07: a subquery inside a statement that takes a row lock is evaluated on
// the snapshot from before the lock wait. Each test forces the wait with a
// transaction the test holds open and proves that the statement waited.

// Auto-delete keeps a thread root that has a pinned reply (sweepBatch). The
// pin of a reply does not lock the root, and the sweep skips (SKIP LOCKED) the
// reply the pin holds, so the sweep chooses the root while the pin is still
// uncommitted. When the pin commits before the root's delete cascades to the
// reply, the pinned reply is deleted. H-L7-09.
func TestAReplyPinnedDuringTheSweepSelectIsDeletedWithItsRoot(t *testing.T) {
	f := newAutoDeleteFixture(t)
	wantStatus(t, "auto-delete", f.set(f.owner, f.channel, 7), http.StatusOK)
	root := f.a.send(f.alice, f.channel, "old root")
	reply := f.replyTo(t, root)
	f.age(t, root, 30)
	f.age(t, reply, 30)

	ctx := context.Background()
	pin, err := f.a.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pin.Rollback(ctx) }()
	// The statement of writePin (internal/chat/messages/pins.go), held open.
	if _, err := pin.Exec(ctx, `UPDATE messages SET is_pinned = TRUE, pinned_at = NOW(), pinned_by = $2 WHERE id = $1`, reply, f.owner.user.ID); err != nil {
		t.Fatal(err)
	}
	// The pin commits after the sweep chose its rows and before it deletes them.
	var commitErr error
	hook := &beforeStatement{match: "DELETE FROM messages WHERE id = ANY", fn: func() { commitErr = pin.Commit(ctx) }}
	d := &channels.AutoDeleter{DB: hookedPool(t, f.a.db, hook), Events: f.a.events, Objects: f.a.store, Audit: &audit.Log{DB: f.a.db}}
	if err := d.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if commitErr != nil {
		t.Fatal(commitErr)
	}
	pinned := f.a.countRows(`SELECT COUNT(*) FROM messages WHERE id = $1 AND is_pinned`, reply) == 1
	t.Logf("pin committed during the sweep; pinned reply kept %v, root kept %v", pinned, f.exists(root))
	// Expected failure until H-L7-09 is fixed (inverse assertion).
	if pinned || f.exists(reply) {
		t.Errorf("known failure H-L7-09 no longer reproduces (pinned reply kept %v): turn this into the plain assertion", pinned)
	}

	// Control: a pin that is committed before the sweep starts is respected.
	root2 := f.a.send(f.alice, f.channel, "old root 2")
	reply2 := f.replyTo(t, root2)
	f.age(t, root2, 30)
	f.age(t, reply2, 30)
	f.a.runSQL(`UPDATE messages SET is_pinned = TRUE, pinned_at = NOW(), pinned_by = $2 WHERE id = $1`, reply2, f.owner.user.ID)
	f.sweep(t)
	if !f.exists(root2) || !f.exists(reply2) {
		t.Errorf("control: a reply pinned before the sweep: root kept %v, reply kept %v", f.exists(root2), f.exists(reply2))
	}
}

// The reply case differs: the reply's insert holds a key-share lock on the
// root, and the sweep takes the root with SKIP LOCKED, so it does not wait and
// does not choose the root while the reply is in flight. The next pass sees the
// young reply and keeps the root. (A reply that commits between the statement's
// snapshot and its lock attempt on the root, microseconds, cannot be forced
// from outside the statement.)
func TestARootWithAReplyInFlightIsSkippedBySweepAndKeptAfterwards(t *testing.T) {
	f := newAutoDeleteFixture(t)
	wantStatus(t, "auto-delete", f.set(f.owner, f.channel, 7), http.StatusOK)
	root := f.a.send(f.alice, f.channel, "old root")
	f.age(t, root, 30)

	ctx := context.Background()
	tx, err := f.a.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var reply uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO messages (channel_id, user_id, content, parent_id) VALUES ($1, $2, 'fresh reply', $3) RETURNING id`,
		f.channel, f.bob.user.ID, root).Scan(&reply); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	f.sweep(t)
	if took := time.Since(began); took > 3*time.Second {
		t.Fatalf("the sweep took %v: it waited for the lock instead of skipping the row", took)
	}
	if !f.exists(root) {
		t.Fatal("the root was deleted while a reply to it was being inserted")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	f.sweep(t)
	if !f.exists(root) || !f.exists(reply) {
		t.Fatalf("after the reply committed: root kept %v, reply kept %v", f.exists(root), f.exists(reply))
	}
}

func (f autoDeleteFixture) replyTo(t *testing.T, root uuid.UUID) uuid.UUID {
	t.Helper()
	res := f.bob.post(f.base()+"/messages", map[string]any{"content": "old reply", "parent_id": root})
	wantStatus(t, "reply", res, http.StatusCreated)
	var m struct{ ID uuid.UUID }
	res.decode(t, &m)
	return m.ID
}

// UpdateForumTag writes only the fields that are set "so two admins changing
// different fields of one tag cannot undo each other". The statement reads the
// locked row in a subquery; an update that waited for another admin's change
// must keep that change.
func TestATagUpdateThatWaitedForAnotherAdminKeepsTheirField(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	forumID := a.createChannel(admin, "board", "forum")
	ctx := context.Background()
	var tag uuid.UUID
	if err := a.db.QueryRow(ctx, `INSERT INTO forum_tags (channel_id, name) VALUES ($1, 'before') RETURNING id`, forumID).Scan(&tag); err != nil {
		t.Fatal(err)
	}
	hold, err := a.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if _, err := hold.Exec(ctx, `UPDATE forum_tags SET emoji = 'x' WHERE id = $1`, tag); err != nil {
		t.Fatal(err)
	}
	name := "renamed"
	type result struct {
		err  error
		took time.Duration
	}
	done := make(chan result, 1)
	go func() {
		began := time.Now()
		_, _, err := forum.UpdateForumTag(ctx, a.db, forumID, tag, forum.TagPatch{Name: &name})
		done <- result{err, time.Since(began)}
	}()
	time.Sleep(500 * time.Millisecond)
	if err := hold.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || got.took < 400*time.Millisecond {
		t.Fatalf("update: err %v after %v: it did not wait, the interleaving did not happen", got.err, got.took)
	}
	var gotName, emoji string
	if err := a.db.QueryRow(ctx, `SELECT name, COALESCE(emoji, '') FROM forum_tags WHERE id = $1`, tag).Scan(&gotName, &emoji); err != nil {
		t.Fatal(err)
	}
	if gotName != "renamed" || emoji != "x" {
		t.Errorf("tag = %q / %q, want renamed / x: one admin's change undid the other's", gotName, emoji)
	}
}

// EditMessageTx decides "empty text is fine when the message has an
// attachment" from an EXISTS subquery of the statement that takes the message
// lock. Removing an attachment from a live message has no route (only the
// account erasure and the message's own delete do), so this is a latent probe:
// the writer that removed the attachment and held the lock is simulated by SQL.
func TestAnEditThatWaitedForTheRemovalOfTheLastAttachmentIsAcceptedEmpty(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(alice, ch, "with a file")
	ctx := context.Background()
	a.runSQL(`INSERT INTO media (uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, $3, 'b', 'k-edit-probe', 'c.png', 'image/png', 1)`, alice.user.ID, ch, msg)
	hold, err := a.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if _, err := hold.Exec(ctx, `UPDATE messages SET updated_at = NOW() WHERE id = $1`, msg); err != nil {
		t.Fatal(err)
	}
	if _, err := hold.Exec(ctx, `DELETE FROM media WHERE message_id = $1`, msg); err != nil {
		t.Fatal(err)
	}
	type result struct {
		err  error
		took time.Duration
	}
	done := make(chan result, 1)
	go func() {
		began := time.Now()
		err := pgx.BeginFunc(ctx, a.db, func(tx pgx.Tx) error {
			_, err := messages.EditMessageTx(ctx, tx, nil, msg, alice.user.ID, "")
			return err
		})
		done <- result{err, time.Since(began)}
	}()
	time.Sleep(500 * time.Millisecond)
	if err := hold.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.took < 400*time.Millisecond {
		t.Fatalf("edit answered after %v: it did not wait, the interleaving did not happen", got.took)
	}
	t.Logf("edit to empty text after the attachment went: %v", got.err)
	// Expected failure until H-L7-10 is fixed (inverse assertion): the edit
	// is accepted on the attachment count from before the wait.
	if got.err != nil {
		t.Errorf("known failure H-L7-10 no longer reproduces (%v): turn this into the plain assertion", got.err)
	}
}
