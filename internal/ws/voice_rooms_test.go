package ws

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
)

// scannedRooms is what the room index stands for: every registered connection
// under the voice room it is in.
func scannedRooms(h *Hub) map[uuid.UUID]map[*Client]struct{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := map[uuid.UUID]map[*Client]struct{}{}
	for c := range h.clients {
		cur := c.currentVoice()
		if cur == nil {
			continue
		}
		if out[*cur] == nil {
			out[*cur] = map[*Client]struct{}{}
		}
		out[*cur][c] = struct{}{}
	}
	return out
}

func indexedRooms(h *Hub) map[uuid.UUID]map[*Client]struct{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[uuid.UUID]map[*Client]struct{}, len(h.rooms))
	for ch, room := range h.rooms {
		out[ch] = make(map[*Client]struct{}, len(room))
		for c := range room {
			out[ch][c] = struct{}{}
		}
	}
	return out
}

// Whatever members do (join, move, leave, get kicked, open a second tab, drop
// the connection, with or without a clean-up that fails halfway), the room
// index lists exactly the connections a scan of all of them finds.
func TestRoomIndexAgreesWithAScanOfAllConnections(t *testing.T) {
	seed := uint64(time.Now().UnixNano())
	t.Logf("seed %d", seed)
	rng := rand.New(rand.NewPCG(seed, 7))

	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = time.Hour // no grace leave fires during the test
	t.Cleanup(h.Close)
	talks := []*core.ChannelInfo{voiceCh(), voiceCh(), voiceCh()}
	users := make([]auth.User, 6)
	for i := range users {
		users[i] = auth.User{ID: uuid.New(), Username: fmt.Sprintf("member-%d", i)}
	}
	var live []*Client
	pick := func() (*Client, int) {
		i := rng.IntN(len(live))
		return live[i], i
	}
	steps := map[string]func(){
		"connect": func() { live = append(live, testClient(h, users[rng.IntN(len(users))])) },
		"join":    func() { c, _ := pick(); h.joinVoice(c, talks[rng.IntN(len(talks))]) },
		"take over": func() {
			c, _ := pick()
			h.takeOverVoice(c, talks[rng.IntN(len(talks))])
		},
		"leave": func() { c, _ := pick(); h.leaveCurrentVoice(c) },
		"kick":  func() { c, _ := pick(); h.KickFromVoice(c.User.ID) },
		"close the talk": func() {
			h.kickAllFromChannel(talks[rng.IntN(len(talks))].ID)
		},
		"disconnect": func() {
			c, i := pick()
			h.unregister(c)
			live = append(live[:i], live[i+1:]...)
		},
		// The voice clean-up of a dropped connection panicked: only the
		// deferred forget ran.
		"disconnect, clean-up failed": func() {
			c, i := pick()
			h.forget(c)
			live = append(live[:i], live[i+1:]...)
		},
	}
	names := make([]string, 0, len(steps))
	for name := range steps {
		names = append(names, name)
	}
	// Map order is random: sort so a seed replays the same run.
	slices.Sort(names)

	for step := 0; step < 2000; step++ {
		name := names[rng.IntN(len(names))]
		if len(live) == 0 || len(live) < 4 && rng.IntN(2) == 0 {
			name = "connect"
		}
		steps[name]()
		for _, c := range live {
			for len(c.send) > 0 {
				<-c.send
			}
		}
		if got, want := indexedRooms(h), scannedRooms(h); !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d (%s): index lists %d rooms %v, a scan finds %d rooms %v", step, name, len(got), got, len(want), want)
		}
	}
}

// A room event reaches the connections in that room, registered ones only,
// and nobody in another room.
func TestRoomEventReachesOnlyTheRoomsRegisteredConnections(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	room, other := voiceCh(), voiceCh()
	inside := testClient(h, auth.User{ID: uuid.New(), Username: "inside"})
	elsewhere := testClient(h, auth.User{ID: uuid.New(), Username: "elsewhere"})
	idle := testClient(h, auth.User{ID: uuid.New(), Username: "idle"})
	h.joinVoice(inside, room)
	h.joinVoice(elsewhere, other)
	unregistered := newClient(h, nil, auth.User{ID: uuid.New(), Username: "unregistered"}, 0)
	unregistered.setVoice(&room.ID)
	for _, c := range []*Client{inside, elsewhere, idle} {
		drainTypes(t, c)
	}

	h.sendToVoiceRoom(room.ID, events.Untyped("probe", map[string]any{}))

	if got := drainTypes(t, inside); count(got, "probe") != 1 {
		t.Fatalf("member of the room got %v, want the event once", got)
	}
	for _, c := range []*Client{elsewhere, idle, unregistered} {
		if got := len(c.send); got != 0 {
			t.Fatalf("%s got %d events of a room it is not in", c.User.Username, got)
		}
	}
}
