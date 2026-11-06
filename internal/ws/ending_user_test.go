package ws

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// A banned member's socket stays open for a moment so the notice is written.
// Meanwhile it must not re-seat them in a Talk (which would leave a ghost seat
// in the grace period after the disconnect) or act on any other frame.
func TestEndingUserCannotReseatInATalkWhileTheNoticeFlushes(t *testing.T) {
	h, _ := newSFUHub(t)
	t.Cleanup(h.Close)
	u := auth.User{ID: uuid.New(), Username: "banned"}
	c := testClient(h, u)
	ch := voiceCh()
	h.joinVoice(c, ch)
	if !inRoom(h, ch.ID, u.ID) {
		t.Fatal("the member could not join")
	}
	drainTypes(t, c)

	h.EndingUser(u.ID)
	h.KickFromVoice(u.ID)
	h.joinVoice(c, ch)
	if inRoom(h, ch.ID, u.ID) {
		t.Fatal("a member being ended took a seat again")
	}

	var budget eventBudget
	if !c.dispatchFrame(&budget, []byte(`{"type":"ping","payload":{}}`)) {
		t.Fatal("the connection must stay open to flush the notice")
	}
	if got := drainTypes(t, c); count(got, "pong") != 0 {
		t.Fatalf("a member being ended was still served: %v", got)
	}

	h.DisconnectUser(u.ID)
	h.mu.RLock()
	graces := len(h.grace)
	h.mu.RUnlock()
	if graces != 0 {
		t.Fatalf("a ghost seat lingers in the grace period: %d", graces)
	}
}
