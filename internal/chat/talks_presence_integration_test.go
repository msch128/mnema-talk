//go:build integration

package chat

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPresenceEventsAreNeverDroppedUnderAFlood(t *testing.T) {
	w := newTalksWorld(t)
	w.mgr.EmptyGrace = time.Hour
	talk, err := w.mgr.Open(t.Context(), w.alice.ID, w.lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Far more events than any fixed queue holds, none applied yet.
	for i := 0; i < 3000; i++ {
		guest := uuid.New()
		w.mgr.OnPresence(guest, talk.ID, true)
		w.mgr.OnPresence(guest, talk.ID, false)
	}
	w.mgr.OnPresence(w.bob.ID, talk.ID, true)
	for w.mgr.applyNext() {
	}
	w.mgr.mu.Lock()
	members := append([]uuid.UUID(nil), w.mgr.talks[talk.ID].members...)
	w.mgr.mu.Unlock()
	if len(members) != 1 || members[0] != w.bob.ID {
		t.Fatalf("members after the flood: %v", members)
	}
}

func TestAnEmptyTalkWithAJoinOnItsWayIsNotRemoved(t *testing.T) {
	w := newTalksWorld(t)
	w.mgr.EmptyGrace = time.Hour
	talk, err := w.mgr.Open(t.Context(), w.alice.ID, w.lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.mgr.OnPresence(w.bob.ID, talk.ID, true) // queued, not applied when the countdown ends
	w.mgr.removeIfEmpty(talk.ID)
	if !w.exists(t, talk.ID) {
		t.Fatal("the countdown removed a Talk somebody was just joining")
	}
	for w.mgr.applyNext() {
	}
	w.mgr.removeIfEmpty(talk.ID)
	if !w.exists(t, talk.ID) {
		t.Fatal("an occupied Talk was removed")
	}
	w.leave(talk.ID, w.bob.ID)
	w.mgr.removeIfEmpty(talk.ID)
	if w.exists(t, talk.ID) {
		t.Fatal("an empty Talk with nothing pending should go")
	}
}
