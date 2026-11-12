//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// A member who leaves a group conversation while seated in its Talk loses the
// conversation and the call together: no seat, no room events, no key
// messages, no messages, no history. Leaving used to drop only the cached
// permission state, so the hub never re-checked the seat and kept relaying
// the Talk's room traffic, MLS handshakes included, for as long as the socket
// lived (hunt finding H-L4-01).
func TestLeavingAGroupConversationEndsItsCall(t *testing.T) {
	w := newConversationWorld(t)
	wantStatus(t, "carol leaves", w.carol.delete("/api/dms/"+w.dm.String()+"/members/@me"), http.StatusNoContent)

	wantStatus(t, "alice writes", w.alice.dmPost(w.dm, "after-carol-left"), http.StatusCreated)
	if p, ok := w.carolWS.expect("message_create", time.Second); ok && strings.Contains(string(p), "after-carol-left") {
		t.Errorf("carol still receives the conversation's messages after leaving: %s", p)
	}
	if st := w.carol.get("/api/channels/" + w.dm.String() + "/messages").status; st == http.StatusOK {
		t.Errorf("carol still reads the history: %d", st)
	}
	got := w.measure()
	for _, cell := range conversationCells {
		if !got[cell].stopped {
			t.Errorf("%s stayed open after carol left: %s", cell, got[cell])
		}
	}
	w.aliceWS.send("voice_mute_state", map[string]any{"muted": true, "deafened": false})
	if p, ok := w.carolWS.expect("voice_mute_state", time.Second); ok {
		t.Errorf("carol still receives the Talk's channel events after leaving: %s", p)
	}
	if !w.seated(w.alice) {
		t.Error("alice, still a member, lost her seat when carol left")
	}
}
