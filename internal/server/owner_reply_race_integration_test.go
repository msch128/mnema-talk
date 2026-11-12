//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Lane 7 of the defect hunt. "The Owner's words (the message or a reply of
// theirs in its thread) are only the Owner's to remove" (lockMessageForDelete).
// A moderator deletes a thread root while the Owner's reply to it is being
// committed: the delete waits for the reply's lock on the root, so it runs
// after the reply and must see it. The reply's transaction is held open by
// the test until the delete is waiting (a real interleaving).
func TestAModeratorsDeleteThatWaitedForTheOwnersReplyKeepsIt(t *testing.T) {
	o := newOwnerReplyRace(t)
	root := o.a.send(o.alice, o.ch, "a root")
	status, kept := o.whileTheOwnerReplies(t, root, func() int {
		return o.mod.delete("/api/channels/" + o.ch.String() + "/messages/" + root.String()).status
	})
	t.Logf("the moderator's delete answered %d; the Owner's reply kept: %v", status, kept)
	// H-L7-07: the Owner check reads the thread after the root's lock, so it
	// sees the reply that the delete waited for.
	if !kept || status != http.StatusForbidden {
		t.Errorf("the delete that waited for the reply answered %d and the reply kept is %v, want 403 and true", status, kept)
	}

	// Control: once the reply is in, the same delete is refused.
	again := o.a.send(o.alice, o.ch, "another root")
	o.a.runSQL(`INSERT INTO messages (channel_id, user_id, content, parent_id) VALUES ($1, $2, 'the Owner answers', $3)`, o.ch, o.owner.user.ID, again)
	wantStatus(t, "delete after the reply", o.mod.delete("/api/channels/"+o.ch.String()+"/messages/"+again.String()), http.StatusForbidden)
}

// The bulk delete keeps a message with a reply of the Owner as well, also
// when it waited for that reply (the same rule, queryLatestMessages).
func TestAModeratorsBulkDeleteThatWaitedForTheOwnersReplyKeepsIt(t *testing.T) {
	o := newOwnerReplyRace(t)
	root := o.a.send(o.alice, o.ch, "a root")
	var res response
	status, kept := o.whileTheOwnerReplies(t, root, func() int {
		res = o.mod.post("/api/channels/"+o.ch.String()+"/messages/bulk-delete", map[string]any{"user_id": o.alice.user.ID, "count": 5})
		return res.status
	})
	var deleted struct{ Deleted int }
	res.decode(t, &deleted)
	rootKept := o.a.count(`SELECT COUNT(*) FROM messages WHERE id = $1`, root) == 1
	t.Logf("the bulk delete answered %d, deleted %d; root kept %v, the Owner's reply kept %v", status, deleted.Deleted, rootKept, kept)
	if status != http.StatusOK || deleted.Deleted != 0 || !rootKept || !kept {
		t.Errorf("bulk delete that waited for the reply: answer %d, deleted %d, root kept %v, reply kept %v; want 200, 0, true, true",
			status, deleted.Deleted, rootKept, kept)
	}
}

type ownerReplyRace struct {
	a                 *app
	owner, alice, mod *client
	ch                uuid.UUID
}

func newOwnerReplyRace(t *testing.T) ownerReplyRace {
	a := newApp(t, false)
	owner := a.seedAdmin()
	o := ownerReplyRace{a: a, owner: owner, alice: a.register(owner, "alice"), mod: a.register(owner, "moddy")}
	assignRoles(t, owner, o.mod, createRole(t, owner, "mods", perm.ManageMessages))
	o.ch = a.createChannel(owner, "general", "text")
	return o
}

// whileTheOwnerReplies inserts the Owner's reply to root in a transaction it
// holds open, runs req until it waits on the root's lock, commits the reply
// and returns req's answer and whether the reply still exists.
func (o ownerReplyRace) whileTheOwnerReplies(t *testing.T, root uuid.UUID, req func() int) (int, bool) {
	t.Helper()
	ctx := context.Background()
	tx, err := o.a.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var reply uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO messages (channel_id, user_id, content, parent_id) VALUES ($1, $2, 'the Owner answers', $3) RETURNING id`,
		o.ch, o.owner.user.ID, root).Scan(&reply); err != nil {
		t.Fatal(err)
	}
	answered := make(chan int, 1)
	began := time.Now()
	go func() { answered <- req() }()
	time.Sleep(500 * time.Millisecond) // the request reaches the root's lock and waits
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	status := <-answered
	if took := time.Since(began); took < 400*time.Millisecond {
		t.Fatalf("the request answered after %v: it did not wait for the reply, the interleaving did not happen", took)
	}
	return status, o.a.count(`SELECT COUNT(*) FROM messages WHERE id = $1`, reply) == 1
}
