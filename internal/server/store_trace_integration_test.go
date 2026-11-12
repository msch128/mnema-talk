//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The store trace: random operations by several members, and what one member
// (the observer) received for them. A trace holds the frames of the observer's
// socket in the order they arrived and, after every operation, the REST
// answers its web client reads (with the number of frames received by then).
// web/src/stores/storeTrace.test.ts replays the frames into the real stores,
// answers their requests from these snapshots, perturbs the delivery (a
// dropped connection, a late answer, a repeated frame) and checks that the
// stores end where a client that has just loaded would start.
//
// STORE_TRACE_OUT=<dir> writes the traces there (the checked-in ones live in
// web/src/stores/traces/); STORE_TRACE_SEEDS=<n> runs n seeds (default 1)
// from STORE_TRACE_FIRST (default 1); STORE_TRACE_STEPS=<n> sets the
// operations per seed (default 60). The nightly job is .github/workflows/store-trace.yml.

// traceSnapshotPaths are the answers the replayed client asks for.
var traceSnapshotPaths = []string{
	"/api/read-state", "/api/threads/following", "/api/channels", "/api/members",
	"/api/me/reminders", "/api/me/scheduled", "/api/me/saved",
}

type traceFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// traceSnapshot is the server's state after operation Op, read when the
// observer had received Frames frames.
type traceSnapshot struct {
	Op     int                        `json:"op"`
	Frames int                        `json:"frames"`
	Bodies map[string]json.RawMessage `json:"bodies"`
}

type storeTrace struct {
	Seed      int             `json:"seed"`
	Me        map[string]any  `json:"me"`
	Snapshots []traceSnapshot `json:"snapshots"`
	Frames    []traceFrame    `json:"frames"`
	Ops       map[string]int  `json:"ops"`
	Log       []string        `json:"log"`
}

type traceMessage struct {
	id      uuid.UUID
	channel uuid.UUID
	author  *client
	parent  uuid.UUID // uuid.Nil for a top-level message
	content string
}

// traceWorld is the community the operations run in: the Owner and two
// members write, the observer reads, follows threads, keeps reminders and
// scheduled messages and writes a little.
type traceWorld struct {
	a        *app
	owner    *client
	writers  []*client
	observer *client
	channels []uuid.UUID
	live     []traceMessage
	mine     map[uuid.UUID][]uuid.UUID // the observer's own messages per channel: targets of replies
	rng      *rand.Rand
	trace    *storeTrace
	bodies   map[string]json.RawMessage // the last body of every snapshot path
	op       int                        // the operation running now

	mu     sync.Mutex
	frames []traceFrame
	last   time.Time
}

func newTraceWorld(t *testing.T, seed int) *traceWorld {
	t.Helper()
	a := newApp(t, true)
	owner := a.seedAdmin()
	w := &traceWorld{a: a, owner: owner, mine: map[uuid.UUID][]uuid.UUID{}, bodies: map[string]json.RawMessage{}, rng: rand.New(rand.NewPCG(uint64(seed), 0x5107e))}
	w.writers = []*client{owner, a.register(owner, "alice"), a.register(owner, "bob")}
	// The replayed client opens the first text channel; nobody writes there, so
	// the other two count as they would for a member looking elsewhere.
	a.createChannel(owner, "lobby", "text")
	w.channels = []uuid.UUID{a.createChannel(owner, "one", "text"), a.createChannel(owner, "two", "text")}
	w.observer = a.register(owner, "max")
	w.trace = &storeTrace{Seed: seed, Ops: map[string]int{},
		Me: map[string]any{"id": w.observer.user.ID, "username": w.observer.user.Username}}
	return w
}

// record collects the observer's frames in the background.
func (w *traceWorld) record(ws *wsConn) {
	go func() {
		for ev := range ws.events {
			w.mu.Lock()
			w.frames = append(w.frames, traceFrame{Type: ev.Type, Payload: ev.Payload})
			w.last = time.Now()
			w.mu.Unlock()
		}
	}()
}

// quiet waits until the observer's socket has received nothing for idle.
func (w *traceWorld) quiet(idle time.Duration) {
	for {
		time.Sleep(idle / 4)
		w.mu.Lock()
		since := time.Since(w.last)
		w.mu.Unlock()
		if since > idle {
			return
		}
	}
}

func (w *traceWorld) received() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.frames)
}

func (w *traceWorld) snapshot(t *testing.T, op int) {
	t.Helper()
	s := traceSnapshot{Op: op, Frames: w.received(), Bodies: map[string]json.RawMessage{}}
	for _, path := range traceSnapshotPaths {
		res := w.observer.get(path)
		wantStatus(t, "snapshot "+path, res, http.StatusOK)
		s.Bodies[path] = res.body
	}
	if after := w.received(); after != s.Frames {
		t.Fatalf("op %d: %d frames arrived while the snapshot was read", op, after-s.Frames)
	}
	// Only what changed is kept; the replay carries the rest forward.
	for path, body := range s.Bodies {
		if prev, ok := w.bodies[path]; ok && string(prev) == string(body) {
			delete(s.Bodies, path)
		} else {
			w.bodies[path] = body
		}
	}
	w.trace.Snapshots = append(w.trace.Snapshots, s)
}

func (w *traceWorld) note(kind, line string) {
	w.trace.Ops[kind]++
	w.trace.Log = append(w.trace.Log, strconv.Itoa(w.op)+" "+kind+" "+line)
}

func (w *traceWorld) pick(n int) int { return w.rng.IntN(n) }

func (w *traceWorld) roots(ch uuid.UUID) []uuid.UUID {
	var out []uuid.UUID
	for _, m := range w.live {
		if m.channel == ch && m.parent == uuid.Nil {
			out = append(out, m.id)
		}
	}
	return out
}

// content draws a message text: plain, naming the observer, or @all.
func (w *traceWorld) content(author *client) string {
	words := []string{"hello", "@max look", "@all heads up", "@here now", "nothing for max"}
	n := len(words)
	if author != w.owner {
		n = 2 // members may not mention everyone; keep their texts accepted
	}
	return words[w.pick(n)] + " " + strconv.Itoa(w.pick(1000))
}

func (w *traceWorld) post(t *testing.T) {
	author := w.writers[w.pick(len(w.writers))]
	ch := w.channels[w.pick(len(w.channels))]
	body := map[string]any{"content": w.content(author)}
	kind := "post"
	// A reply goes under someone's message, the observer's own included (its first reply makes the observer follow).
	if roots := append(w.roots(ch), w.mine[ch]...); len(roots) > 0 && w.pick(4) == 0 {
		body["parent_id"] = roots[w.pick(len(roots))]
		kind = "reply"
	} else if mine := w.mine[ch]; len(mine) > 0 && w.pick(5) == 0 {
		body["reply_to_id"] = mine[w.pick(len(mine))]
		kind = "answer"
	}
	id := postMessage(t, author, ch, body)
	parent, _ := body["parent_id"].(uuid.UUID)
	w.live = append(w.live, traceMessage{id: id, channel: ch, author: author, parent: parent, content: body["content"].(string)})
	w.note(kind, fmt.Sprintf("%s %s %v", id, author.user.Username, body))
}

func (w *traceWorld) edit(t *testing.T) {
	if len(w.live) == 0 {
		return
	}
	m := &w.live[w.pick(len(w.live))]
	next := "@max edited " + strconv.Itoa(w.pick(1000))
	if w.pick(2) == 0 {
		next = "edited, nobody named " + strconv.Itoa(w.pick(1000))
	}
	res := m.author.put("/api/channels/"+m.channel.String()+"/messages/"+m.id.String(), map[string]any{"content": next})
	wantStatus(t, "edit", res, http.StatusOK)
	w.note("edit", fmt.Sprintf("%s %q -> %q", m.id, m.content, next))
	m.content = next
}

func (w *traceWorld) remove(t *testing.T) {
	if len(w.live) == 0 {
		return
	}
	m := w.live[w.pick(len(w.live))]
	by := m.author
	if w.pick(3) == 0 {
		by = w.owner // a moderator removes it
	}
	res := by.delete("/api/channels/" + m.channel.String() + "/messages/" + m.id.String())
	if res.status != http.StatusNoContent && res.status != http.StatusOK {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	// A deleted root takes its replies with it.
	kept := w.live[:0]
	for _, x := range w.live {
		if x.id != m.id && x.parent != m.id {
			kept = append(kept, x)
		}
	}
	w.live = kept
	w.note("delete", fmt.Sprintf("%s by %s (%q)", m.id, by.user.Username, m.content))
}

// later makes a reminder or a scheduled message, or lets everything pending
// fall due and runs the sweep the background ticker would run.
func (w *traceWorld) later(t *testing.T) {
	ch := w.channels[w.pick(len(w.channels))]
	switch r := w.pick(3); {
	case r == 0 && len(w.live) > 0:
		m := w.live[w.pick(len(w.live))]
		res := w.observer.post("/api/me/reminders", map[string]any{"message_id": m.id, "remind_at": inFuture(time.Hour)})
		wantStatus(t, "remind", res, http.StatusCreated)
		w.note("remind", m.id.String())
	case r == 1:
		res := w.observer.post("/api/channels/"+ch.String()+"/scheduled", map[string]any{"content": "max later " + strconv.Itoa(w.pick(1000)), "send_at": inFuture(time.Hour)})
		wantStatus(t, "schedule", res, http.StatusCreated)
		w.note("schedule", ch.String())
	default:
		ctx := context.Background()
		for _, q := range []string{
			`UPDATE message_reminders SET remind_at = NOW() - INTERVAL '1 second' WHERE delivered_at IS NULL`,
			`UPDATE scheduled_messages SET send_at = NOW() - INTERVAL '1 second' WHERE state = 'pending'`,
		} {
			if _, err := w.a.db.Exec(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.a.router.Later.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
		w.note("due", "")
	}
}

// keep saves or unsaves a message for the observer.
func (w *traceWorld) keep(t *testing.T) {
	if len(w.live) == 0 {
		return
	}
	m := w.live[w.pick(len(w.live))]
	path := "/api/me/saved/" + m.id.String()
	if w.pick(3) == 0 {
		res := w.observer.delete(path)
		if res.status != http.StatusNoContent && res.status != http.StatusOK && res.status != http.StatusNotFound {
			t.Fatalf("unsave: %d %s", res.status, res.body)
		}
		w.note("unsave", m.id.String())
		return
	}
	wantStatus(t, "save", w.observer.put(path, nil), http.StatusOK)
	w.note("save", m.id.String())
}

func (w *traceWorld) observe(t *testing.T) {
	ch := w.channels[w.pick(len(w.channels))]
	switch w.pick(4) {
	case 3:
		w.keep(t)
	case 0:
		id := postMessage(t, w.observer, ch, map[string]any{"content": "max writes " + strconv.Itoa(w.pick(1000))})
		w.mine[ch] = append(w.mine[ch], id)
		w.note("own", id.String())
	case 1:
		res := w.observer.post("/api/channels/"+ch.String()+"/read", nil)
		wantStatus(t, "read", res, http.StatusNoContent)
		w.note("read", ch.String())
	default:
		roots := w.roots(ch)
		if len(roots) == 0 {
			return
		}
		root := roots[w.pick(len(roots))]
		res, kind := w.observer.put("/api/threads/"+root.String()+"/follow", nil), "follow"
		if w.pick(2) == 0 {
			res, kind = w.observer.delete("/api/threads/"+root.String()+"/follow"), "unfollow"
		}
		wantStatus(t, kind, res, http.StatusOK)
		w.note(kind, root.String())
	}
}

func (w *traceWorld) step(t *testing.T) {
	switch r := w.pick(20); {
	case r < 9:
		w.post(t)
	case r < 11:
		w.edit(t)
	case r < 13:
		w.remove(t)
	case r < 15:
		w.later(t)
	default:
		w.observe(t)
	}
}

func runStoreTrace(t *testing.T, seed, steps int) *storeTrace {
	t.Helper()
	w := newTraceWorld(t, seed)
	ws := w.observer.dialLiveWS(w.a.origin())
	w.record(ws)
	w.quiet(500 * time.Millisecond)
	w.mu.Lock()
	w.frames = nil // the greeting is what the replay's own socket open stands for
	w.mu.Unlock()
	w.snapshot(t, 0)
	for op := 1; op <= steps; op++ {
		w.op = op
		w.step(t)
		w.quiet(150 * time.Millisecond)
		w.snapshot(t, op)
	}
	w.quiet(500 * time.Millisecond)
	w.snapshot(t, steps+1)
	w.mu.Lock()
	w.trace.Frames = append([]traceFrame(nil), w.frames...)
	w.mu.Unlock()
	return w.trace
}

func envInt(name string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil && n > 0 {
		return n
	}
	return fallback
}

func TestStoreTrace(t *testing.T) {
	first, seeds, steps := envInt("STORE_TRACE_FIRST", 1), envInt("STORE_TRACE_SEEDS", 1), envInt("STORE_TRACE_STEPS", 60)
	out := os.Getenv("STORE_TRACE_OUT")
	for seed := first; seed < first+seeds; seed++ {
		t.Run("seed"+strconv.Itoa(seed), func(t *testing.T) {
			tr := runStoreTrace(t, seed, steps)
			// The run must have done what it claims: writes, edits, deletes and frames for them.
			for _, kind := range []string{"post", "edit", "delete"} {
				if tr.Ops[kind] == 0 && steps >= 40 {
					t.Fatalf("seed %d ran no %s (ops %v)", seed, kind, tr.Ops)
				}
			}
			if len(tr.Frames) < tr.Ops["post"] {
				t.Fatalf("seed %d: %d frames for %d posts", seed, len(tr.Frames), tr.Ops["post"])
			}
			if out == "" {
				return
			}
			raw, err := json.Marshal(tr)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(out, fmt.Sprintf("seed-%03d.json", seed)), raw, 0o644); err != nil {
				t.Fatal(err)
			}
		})
	}
}
