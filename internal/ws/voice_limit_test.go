package ws

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/perm"
)

func limited(w *permWorld, limit int) {
	w.public.UserLimit = limit
}

func roomSize(h *Hub, ch uuid.UUID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.voice[ch])
}

func refusedRoomFull(t *testing.T, c *Client) bool {
	t.Helper()
	return count(drainTypes(t, c), "voice_kicked") == 1
}

func TestFullTalkRefusesMembersWithoutTheBypass(t *testing.T) {
	w := newPermWorld(t)
	limited(w, 1)
	w.h.joinVoice(w.insider, w.public)
	drainTypes(t, w.outsider)
	w.h.joinVoice(w.outsider, w.public)
	if w.outsider.currentVoice() != nil || roomSize(w.h, w.public.ID) != 1 {
		t.Fatal("a member without the bypass joined a full Talk")
	}
	if !refusedRoomFull(t, w.outsider) {
		t.Fatal("the refused member was not told")
	}
}

func TestDisconnectMembersAndAdministratorsExceedTheLimit(t *testing.T) {
	w := newPermWorld(t)
	limited(w, 1)
	mod := testClient(w.h, auth.User{ID: uuid.New()})
	w.f.Assign(mod.User.ID, w.f.Role(2, perm.DisconnectMembers))
	w.h.joinVoice(w.insider, w.public)
	w.h.joinVoice(mod, w.public)
	w.h.joinVoice(w.owner, w.public)
	if got := roomSize(w.h, w.public.ID); got != 3 {
		t.Fatalf("room holds %d, want 3 (1 + two bypassing joins)", got)
	}
}

func TestNoLimitMeansUnlimited(t *testing.T) {
	w := newPermWorld(t)
	limited(w, 0)
	for i := 0; i < 5; i++ {
		w.h.joinVoice(testClient(w.h, auth.User{ID: uuid.New()}), w.public)
	}
	if got := roomSize(w.h, w.public.ID); got != 5 {
		t.Fatalf("room holds %d, want 5", got)
	}
}

// Many joins race for the last seat of a Talk with limit 3: exactly 3 get in.
func TestLastSeatGoesToExactlyOneJoin(t *testing.T) {
	w := newPermWorld(t)
	limited(w, 3)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		c := testClient(w.h, auth.User{ID: uuid.New()})
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.h.joinVoice(c, w.public)
		}()
	}
	wg.Wait()
	if got := roomSize(w.h, w.public.ID); got != 3 {
		t.Fatalf("room holds %d, want exactly the limit 3", got)
	}
}
