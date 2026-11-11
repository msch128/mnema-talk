//go:build integration

package server

import (
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events/eventstest"
	"github.com/msch128/mnema-talk/internal/slash"
)

// The tests in the event_wire_*_integration_test.go files pin the JSON of the
// realtime events byte for byte (testdata/event_wire_*.golden). Each step
// calls the real route, so the frame is what that publisher sends; IDs and
// timestamps are replaced, nothing else. The frames are public API: a failure
// here means a client would see a different event.

// wireWorld is a community with an Owner, two members and a text channel
// whose publications are recorded.
type wireWorld struct {
	t                 *testing.T
	a                 *app
	g                 *eventstest.Golden
	admin, alice, bob *client
	ch                uuid.UUID
}

func newWireWorld(t *testing.T, golden string) *wireWorld {
	t.Helper()
	a := newApp(t, false)
	w := &wireWorld{t: t, a: a, g: eventstest.Open(t, golden)}
	// The background workers are driven by the steps, never by their tickers.
	w.admin = a.seedAdmin()
	w.alice = a.register(w.admin, "alice")
	w.bob = a.register(w.admin, "bob")
	w.ch = a.createChannel(w.admin, "general", "text")
	a.events.Take()
	t.Cleanup(w.g.Check)
	return w
}

// step runs one action and records everything it published.
func (w *wireWorld) step(name string, res response) {
	w.t.Helper()
	w.stepOf(name, 1, res)
}

// stepOf is step for an action whose events are published by a worker: it
// waits until at least n arrived and records them sorted, because workers
// publish in no fixed order.
func (w *wireWorld) stepOf(name string, n int, res response) {
	w.t.Helper()
	if res.status >= 300 {
		w.t.Fatalf("%s: %d %s", name, res.status, res.body)
	}
	w.recorded(name, n)
}

// recorded records what was published since the last step, waiting for n events.
func (w *wireWorld) recorded(name string, n int) {
	w.t.Helper()
	var frames []eventstest.Frame
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		for _, e := range w.a.events.Take() {
			frames = append(frames, eventstest.Frame{Site: e.Site, Wire: e.Wire()})
		}
		if len(frames) >= n || time.Now().After(deadline) {
			break
		}
	}
	if n > 1 {
		sort.Slice(frames, func(i, j int) bool { return string(frames[i].Wire) < string(frames[j].Wire) })
	}
	w.g.Step(name, frames...)
}

// quiet runs a preparation whose events are not part of the next step.
func (w *wireWorld) quiet(res response) response {
	w.t.Helper()
	if res.status >= 300 {
		w.t.Fatalf("preparation: %d %s", res.status, res.body)
	}
	w.a.events.Take()
	return res
}

func (w *wireWorld) id(res response) uuid.UUID {
	w.t.Helper()
	var out struct{ ID uuid.UUID }
	w.quiet(res).decode(w.t, &out)
	return out.ID
}

func userPath(c *client, suffix string) string {
	return "/api/admin/users/" + c.user.ID.String() + suffix
}

func TestWireFormatOfMemberEvents(t *testing.T) {
	w := newWireWorld(t, "event_wire_members")
	w.stepRegister()
	w.step("profile change", w.alice.put("/api/users/me/profile", map[string]string{"display_name": "Alice A", "bio": "hello"}))
	w.step("status change", w.alice.put("/api/users/me/status", map[string]string{"status_text": "away for lunch"}))
	w.step("avatar upload", w.alice.upload(avatarRoute, "avatar", "a.png", "image/png", []byte(bannerPNG(t, 64, 64)), nil))
	w.step("timeout", w.admin.post(userPath(w.bob, "/timeout"), map[string]any{"duration_seconds": 600, "reason": "calm down"}))
	w.step("timeout lifted", w.admin.delete(userPath(w.bob, "/timeout")))
	w.step("slash command /kick", w.admin.post("/api/channels/"+w.ch.String()+"/interactions", slashCall("kick", map[string]any{"member": w.bob.user.ID, "reason": "see you"})))
	w.step("ban", w.admin.post(userPath(w.bob, "/ban"), map[string]any{"reason": "spam"}))
	w.step("ban lifted", w.admin.do(http.MethodDelete, userPath(w.bob, "/ban"), nil, nil))
	w.step("account disabled", w.admin.post(userPath(w.bob, "/disable"), nil))
	w.step("account enabled", w.admin.post(userPath(w.bob, "/enable"), nil))
	pinned := w.say(w.alice, w.ch, map[string]any{"content": "pinned, then gone with its author"})
	w.quiet(w.admin.put(w.msgPath(w.ch, "/"+pinned.String()+"/pin"), nil))
	w.step("account deleted", w.a.deleteAccount(w.alice, "member-password-123", ""))
}

func (w *wireWorld) stepRegister() {
	w.t.Helper()
	w.a.register(w.admin, "carol")
	w.recorded("a member joins", 1)
}

func (w *wireWorld) msgPath(ch uuid.UUID, rest string) string {
	return "/api/channels/" + ch.String() + "/messages" + rest
}

// say posts a message without recording it.
func (w *wireWorld) say(c *client, ch uuid.UUID, body map[string]any) uuid.UUID {
	w.t.Helper()
	return w.id(c.post(w.msgPath(ch, ""), body))
}

func slashCall(name string, options map[string]any) map[string]any {
	return map[string]any{"command_id": slash.ID(name), "options": options}
}
