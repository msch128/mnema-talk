package ws

import (
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// How many rounds each stress test runs, bounded for CI. Before the removal
// of a seat was decided under the hub's lock, a round lost the seat in about
// 1 to 10 of 100 leaves, 1 to 5 of 100 dropped sockets and 1 to 6 of 1000
// clean-ups after a panic (far more often under the race detector).
const (
	leaveRounds = 1500
	panicRounds = 2500
	dropRounds  = 1000
)

// raceSeat seats one connection of a member in a Talk, then lets end run on
// it while a second connection of the member joins, rounds times. It returns
// in how many rounds the second connection ended with voice in a Talk the
// member is not listed in. The rounds are looked at in batches, once settle
// has passed.
func raceSeat(t *testing.T, h *Hub, rounds int, settle time.Duration, end func(first *Client)) int {
	t.Helper()
	// Every join and leave is logged: thousands of lines nobody reads.
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })
	const batch = 250
	ch := voiceCh()
	lost := 0
	for done := 0; done < rounds; done += batch {
		pairs := make([][2]*Client, batch)
		for i := range pairs {
			user := auth.User{ID: uuid.New(), Username: "twotabs"}
			first, second := testClient(h, user), testClient(h, user)
			pairs[i] = [2]*Client{first, second}
			h.joinVoice(first, ch)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); end(first) }()
			go func() { defer wg.Done(); h.joinVoice(second, ch) }()
			wg.Wait()
		}
		time.Sleep(settle)
		for _, pair := range pairs {
			second := pair[1]
			if second.currentVoice() != nil && !inRoom(h, ch.ID, second.User.ID) {
				lost++
			}
			h.leaveCurrentVoice(second)
			h.unregister(pair[0])
			h.unregister(second)
		}
	}
	return lost
}

// One tab of a member leaves a Talk in the moment another tab of theirs
// joins it. Whoever comes first, the tab that ends with voice is listed: the
// leave must not take the seat the join has just found.
func TestJoinDuringTheLeaveOfAnotherTabKeepsItsSeat(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	if lost := raceSeat(t, h, leaveRounds, 0, h.leaveCurrentVoice); lost != 0 {
		t.Fatalf("in %d of %d rounds a tab had voice in a Talk its member was not listed in", lost, leaveRounds)
	}
}

// The same when the first tab does not leave but panics and is cleaned up.
func TestJoinDuringTheCleanUpOfAPanickingTabKeepsItsSeat(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	if lost := raceSeat(t, h, panicRounds, 0, (*Client).abandon); lost != 0 {
		t.Fatalf("in %d of %d rounds a tab had voice in a Talk its member was not listed in", lost, panicRounds)
	}
}

// The same when the first tab's socket drops: its seat lingers for the grace
// time, which must not end the stay of the tab that joined meanwhile.
func TestJoinDuringTheDropOfAnotherTabOutlivesTheGraceTime(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	h.VoiceGrace = 10 * time.Millisecond
	if lost := raceSeat(t, h, dropRounds, 6*h.VoiceGrace, h.unregister); lost != 0 {
		t.Fatalf("in %d of %d rounds a tab had voice in a Talk its member was not listed in after the grace time", lost, dropRounds)
	}
}
