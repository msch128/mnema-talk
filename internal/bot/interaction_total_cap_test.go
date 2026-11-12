package bot

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Lane 8, inventory I6 row 41: the total cap of the interaction store, which no
// test named. At maxPending nothing is evicted: whoever comes next is refused,
// whatever bot or member, until runs reach interactionTTL. An answered run
// counts as long as a run in flight (its bot may still follow up), so the store
// holds at most maxPending runs per interactionTTL for the whole community. The
// fill counts what it added so it cannot pass by doing nothing.
func TestInteractionStoreTotalCapRefusesNewcomersUntilRunsExpire(t *testing.T) {
	now := time.Now()
	s := &interactionStore{m: map[uuid.UUID]*interaction{}, now: func() time.Time { return now }}
	add := func(bot, invoker uuid.UUID) bool {
		_, ok := s.add(uuid.New(), &interaction{botID: bot, invoker: invoker})
		return ok
	}
	// The total takes maxPending/maxPendingPerBot bots at their own cap, and
	// maxPending/maxPendingPerInvoker members at theirs.
	added := 0
	for b := 0; b < maxPending/maxPendingPerBot; b++ {
		bot := uuid.New()
		for i := 0; i < maxPendingPerBot; i += maxPendingPerInvoker {
			member := uuid.New()
			for j := 0; j < maxPendingPerInvoker; j++ {
				if !add(bot, member) {
					t.Fatalf("refused at %d, below every cap", added)
				}
				added++
			}
		}
	}
	if added != maxPending || len(s.m) != maxPending {
		t.Fatalf("filled %d (store %d), want %d", added, len(s.m), maxPending)
	}
	// A member who never ran anything, on a bot nobody used: refused.
	if add(uuid.New(), uuid.New()) {
		t.Fatal("a newcomer was accepted past the total cap")
	}
	if len(s.m) != maxPending {
		t.Fatalf("a refused run changed the store: %d entries", len(s.m))
	}
	// Still refused just before the oldest runs expire, accepted after.
	now = now.Add(interactionTTL)
	if add(uuid.New(), uuid.New()) {
		t.Fatal("accepted before any run expired")
	}
	now = now.Add(time.Second)
	if !add(uuid.New(), uuid.New()) {
		t.Fatal("still refused after every run expired")
	}
	if len(s.m) != 1 {
		t.Fatalf("expired runs not dropped: %d entries", len(s.m))
	}
	t.Logf("filled %d runs; newcomers refused for %s; capacity %d runs per %s", added, interactionTTL, maxPending, interactionTTL)
}
