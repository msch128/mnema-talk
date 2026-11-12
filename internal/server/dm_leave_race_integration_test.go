//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/chat/dm"
)

// Lane 7 of the defect hunt. An add that waits for the lock of a member's
// leave runs after it, so the member who left must not be able to add
// anybody, and its answer must agree with what it did (H-L7-06). The add is held at the conversation's lock while the leave is
// between its lock and its commit (a real interleaving, forced with pgx's
// tracing hook).
func TestAMemberWhoLeftCannotAddAnybodyWithAnAddThatWaitedForTheLeave(t *testing.T) {
	f := newDMFixture(t, false)
	group := f.alice.mustDM(t, f.bob, f.carol)
	added := make(chan int, 1)
	hook := &beforeStatement{match: "DELETE FROM dm_members WHERE channel_id = $1 AND user_id = $2", fn: func() {
		go func() {
			added <- f.alice.post("/api/dms/"+group.ID.String()+"/members", map[string]any{"user_id": f.dave.user.ID}).status
		}()
		// Long enough for the add to reach the lock and wait there.
		time.Sleep(500 * time.Millisecond)
	}}
	if _, _, err := dm.LeaveDM(context.Background(), hookedPool(t, f.a.db, hook), group.ID, f.alice.user.ID); err != nil {
		t.Fatal(err)
	}
	status := <-added
	inGroup := f.a.count(`SELECT COUNT(*) FROM dm_members WHERE channel_id = $1 AND user_id = $2`, group.ID, f.dave.user.ID) == 1
	t.Logf("add during the leave answered %d; dave in the group: %v", status, inGroup)
	// H-L7-06: membership is read after the lock, so alice is no longer in
	// the conversation and nobody is added.
	if inGroup || status != http.StatusNotFound {
		t.Errorf("add after the leave answered %d and dave in the group is %v, want 404 and false", status, inGroup)
	}
}
