package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
)

// Cycle tests (lane 8, brief: "many rounds of join and leave, connect and
// disconnect ... assert that goroutines, structure sizes and heap return to the
// baseline"). Each test runs N rounds, waits for the hub to settle, and checks
// three oracles: runtime.NumGoroutine within a small constant of the baseline,
// every hub map listed in inventory I6 (rows 6-16) back to its baseline size,
// and HeapInuse after two GCs grown by no more than a stated bound. Each test
// counts the rounds it really performed.
//
// Smoke: a few hundred rounds. Deep: CYCLE_DEEP=1 (or CYCLE_ROUNDS=n) for tens
// of thousands, with the same assertions.

func cycleRounds(smoke int) int {
	if v := os.Getenv("CYCLE_ROUNDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	if os.Getenv("CYCLE_DEEP") == "1" {
		return 20_000
	}
	return smoke
}

// uuidAuth signs in whoever the X-Test-User header names, as a fresh account.
type uuidAuth struct{}

func (uuidAuth) AuthenticateRequest(r *http.Request) (*auth.User, int, error) {
	id, err := uuid.Parse(r.Header.Get("X-Test-User"))
	if err != nil {
		return nil, 0, err
	}
	return &auth.User{ID: id, Username: "u" + id.String()[:8]}, 0, nil
}

// hubSizes reports the size of every remote-keyed structure of the hub.
func hubSizes(h *Hub) map[string]int {
	s := map[string]int{}
	h.mu.RLock()
	s["clients"], s["pending"], s["online"] = len(h.clients), len(h.pending), len(h.online)
	s["botLive"], s["botState"], s["active"] = len(h.botLive), len(h.botState), len(h.active)
	s["chosen"], s["profiles"], s["voice"] = len(h.chosen), len(h.profiles), len(h.voice)
	s["rooms"], s["grace"], s["voiceSince"] = len(h.rooms), len(h.grace), len(h.voiceSince)
	s["seatClasses"], s["voiceMute"], s["roomSince"] = len(h.seatClasses), len(h.voiceMute), len(h.roomSince)
	h.mu.RUnlock()
	lock := func(name string, f func() int) { s[name] = f() }
	lock("voiceGate", func() int { h.voiceGate.mu.Lock(); defer h.voiceGate.mu.Unlock(); return len(h.voiceGate.m) })
	lock("voiceTimes", func() int { h.vtimes.mu.Lock(); defer h.vtimes.mu.Unlock(); return len(h.vtimes.m) })
	lock("takeover", func() int { h.taken.mu.Lock(); defer h.taken.mu.Unlock(); return len(h.taken.at) })
	lock("stageGate", func() int { h.stageGate.mu.Lock(); defer h.stageGate.mu.Unlock(); return len(h.stageGate.m) })
	lock("reactionGate", func() int { h.reactionGate.mu.Lock(); defer h.reactionGate.mu.Unlock(); return len(h.reactionGate.m) })
	lock("userBudgets", func() int { h.userBudgets.mu.Lock(); defer h.userBudgets.mu.Unlock(); return len(h.userBudgets.m) })
	lock("stage", func() int {
		h.stage.mu.Lock()
		defer h.stage.mu.Unlock()
		return len(h.stage.grant) + len(h.stage.hand) + len(h.stage.shown)
	})
	return s
}

// heapInuse is HeapInuse after two collections.
func heapInuse() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}

// settleGoroutines waits until the goroutine count is within slack of base.
func settleGoroutines(base, slack int) int {
	n := runtime.NumGoroutine()
	for deadline := time.Now().Add(10 * time.Second); n > base+slack && time.Now().Before(deadline); n = runtime.NumGoroutine() {
		time.Sleep(20 * time.Millisecond)
	}
	return n
}

// checkBaseline applies the three oracles.
//
// known names maps with a recorded leak (a finding): for those the inverse is
// asserted (the map did NOT return to baseline), so the test turns red the day
// the leak is fixed and the name must then be dropped from known.
func checkBaseline(t *testing.T, h *Hub, base map[string]int, goBase int, heapBase uint64, heapBound uint64, known map[string]bool) {
	t.Helper()
	gor := settleGoroutines(goBase, 4)
	var sizes map[string]int
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		sizes = hubSizes(h)
		if equalSizes(sizes, base, known) || time.Now().After(deadline) {
			break
		}
	}
	for k, v := range sizes {
		switch {
		case known[k] && v <= base[k]:
			t.Errorf("hub.%s returned to baseline (%d): the recorded leak is fixed, drop it from known", k, v)
		case !known[k] && v != base[k]:
			t.Errorf("hub.%s = %d after the cycle, baseline %d", k, v, base[k])
		}
	}
	if gor > goBase+4 {
		t.Errorf("goroutines = %d after the cycle, baseline %d", gor, goBase)
	}
	if heap := heapInuse(); heap > heapBase+heapBound {
		t.Errorf("heap in use grew by %d bytes (bound %d)", heap-heapBase, heapBound)
	}
	t.Logf("goroutines %d (baseline %d), sizes %v", gor, goBase, sizes)
}

func equalSizes(a, b map[string]int, known map[string]bool) bool {
	for k, v := range a {
		if !known[k] && b[k] != v {
			return false
		}
	}
	return true
}

// TestHubConnectTypeDisconnectCycleReturnsToBaseline: each round a fresh
// account connects over a real WebSocket, waits for its greeting, sends a
// typing notice and an idle flag, and disconnects.
func TestHubConnectTypeDisconnectCycleReturnsToBaseline(t *testing.T) {
	rounds := cycleRounds(300)
	h := NewHub(nil, uuidAuth{}, nil, nil, openPerms())
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)
	h.Origins = []string{srv.URL}
	ch := openChannel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	round := func() bool {
		hdr := http.Header{"Origin": []string{srv.URL}, "X-Test-User": []string{uuid.NewString()}}
		conn, _, err := websocket.DefaultDialer.Dial(url, hdr)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		// Close handshake: the server closes the TCP connection first, so the
		// TIME_WAIT lands on its side and deep runs do not use up the client's
		// ephemeral ports.
		defer func() {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					break
				}
			}
			_ = conn.Close()
		}()
		readUntil(t, conn, events.VoiceRooms)
		for _, f := range []map[string]any{
			{"type": events.ClientTyping, "payload": map[string]any{"channel_id": ch}},
			{"type": events.ClientPresenceIdle, "payload": map[string]any{"idle": true}},
		} {
			frame, _ := json.Marshal(f)
			if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
		return true
	}
	round() // warm-up: lazy pools, the HTTP server's own goroutines
	waitUntil(t, func() bool { return h.OnlineCount() == 0 })
	base, goBase, heapBase := hubSizes(h), settleGoroutines(runtime.NumGoroutine(), 0), heapInuse()

	done := 0
	for range rounds {
		if round() {
			done++
		}
	}
	if done != rounds {
		t.Fatalf("only %d of %d rounds ran", done, rounds)
	}
	// An account that went idle before it disconnected must not leave
	// active[user] = 0 behind (H-L8-04).
	checkBaseline(t, h, base, goBase, heapBase, 4<<20, nil)
	t.Logf("rounds=%d connections=%d", rounds, done)
}

// TestHubVoiceJoinLeaveCycleReturnsToBaseline: each round a fresh account
// registers, joins a voice room, leaves it (or drops and lets the grace timer
// end the seat, every other round), and goes offline.
func TestHubVoiceJoinLeaveCycleReturnsToBaseline(t *testing.T) {
	rounds := cycleRounds(400)
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	h.VoiceGrace = 5 * time.Millisecond
	ch := voiceCh()
	base, goBase, heapBase := hubSizes(h), runtime.NumGoroutine(), heapInuse()

	joins, leaves, drops := 0, 0, 0
	for i := range rounds {
		c := testClient(h, auth.User{ID: uuid.New(), Username: "v"})
		h.joinVoice(c, ch)
		if !inRoom(h, ch.ID, c.User.ID) {
			t.Fatalf("round %d: join did not seat the member", i)
		}
		joins++
		if i%2 == 0 {
			h.leaveCurrentVoice(c)
			leaves++
		} else {
			drops++ // the connection drops while seated: grace ends the seat
		}
		h.unregister(c)
	}
	if joins != rounds || leaves+drops != rounds {
		t.Fatalf("rounds did not run: joins %d leaves %d drops %d", joins, leaves, drops)
	}
	checkBaseline(t, h, base, goBase, heapBase, 4<<20, nil)
	t.Logf("rounds=%d joins=%d leaves=%d drops=%d", rounds, joins, leaves, drops)
}

// TestIdleThenOfflineLeavesNoActiveEntry: a client that goes idle and then
// disconnects leaves no active entry behind (H-L8-04).
func TestIdleThenOfflineLeavesNoActiveEntry(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	c := testClient(h, auth.User{ID: uuid.New(), Username: "idle"})
	h.setIdle(c, true)
	h.unregister(c)
	h.mu.RLock()
	n, kept := h.active[c.User.ID]
	_, online := h.online[c.User.ID]
	h.mu.RUnlock()
	if online {
		t.Fatal("setup: the user is still online")
	}
	if kept {
		t.Fatalf("offline account keeps active[user] = %d", n)
	}
}
