//go:build integration

package discordimport

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// A message that is being posted while the undo checks the channels must be
// seen by that check, so it cannot be deleted without the extra confirmation.
func TestUndoSeesAMessageThatIsBeingPosted(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	rec, err := w.im.Apply(ctx, w.admin, "adm", applyReq(w.preview(w.admin)))
	w.must(err)

	tx, err := w.p.Begin(ctx)
	w.must(err)
	defer func() { _ = tx.Rollback(ctx) }()
	// The message is inserted but not committed yet.
	_, err = tx.Exec(ctx, `INSERT INTO messages (channel_id, user_id, content) SELECT c.id, $1, 'late' FROM channels c WHERE c.name = 'rules'`, w.admin.ID)
	w.must(err)

	done := make(chan error, 1)
	go func() {
		_, err := w.im.Undo(ctx, w.admin, rec.ID, UndoRequest{})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the undo did not wait for the message being posted: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	w.must(tx.Commit(ctx))
	wantStatus(t, "undo after the message landed", <-done, http.StatusConflict)

	var msgs int
	w.must(w.p.QueryRow(ctx, `SELECT COUNT(*) FROM messages`).Scan(&msgs))
	if msgs != 1 {
		t.Fatalf("the message was deleted without the confirmation: %d left", msgs)
	}
}
