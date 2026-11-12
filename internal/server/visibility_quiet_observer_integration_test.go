//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestHiddenActivityChangesNothingAnObserverCanRead is a differential test
// for counts, badges, typing and presence. Observers read every GET route
// that needs no parameter (and the open channel's), twice while nothing
// happens (what differs then is noise and is ignored), then Staff members
// are busy in the Staff channels only: messages that mention the observers
// and @everyone, a thread, reactions, a pin, an edit, a delete, a rename,
// typing, a seat in the Staff Talk, an event there. Afterwards no answer an
// observer reads may differ from before, and no socket frame may have
// reached them meanwhile. A count that moved is a leak even when it carries
// no word.
func TestHiddenActivityChangesNothingAnObserverCanRead(t *testing.T) {
	w := newCanaryWorld(t, 7)
	w.build(t)
	w.a.router.Hub.VoiceGrace = testGrace
	insiderWS := mustDial(t, w.insider)
	observers := map[string]*client{"outsider": w.outsider, "carol": w.carol, "exstaff": w.exstaff}
	paths := quietPaths(t, w)
	before, noise := map[string]map[string]any{}, map[string]map[string]bool{}
	logs := map[string]*frameLog{}
	for name, c := range observers {
		logs[name] = recordFrames(t, c)
	}
	time.Sleep(500 * time.Millisecond)
	for name, c := range observers {
		first := readAll(c, paths)
		time.Sleep(1100 * time.Millisecond)
		before[name] = readAll(c, paths)
		noise[name] = diffKeys(first, before[name])
		logs[name].reset()
	}
	acted := w.busyInStaff(t, insiderWS)
	time.Sleep(time.Second)
	moved, hints := 0, 0
	for name, c := range observers {
		after := readAll(c, paths)
		for key := range diffKeys(before[name], after) {
			if !noise[name][key] {
				moved++
				t.Errorf("%s: %s changed while only hidden things happened", name, key)
			}
		}
		for _, ev := range logs[name].all() {
			if invalidationHints[ev.Type] && string(ev.Payload) == "null" {
				hints++ // suspicion S-L6-a: an empty hint that something changed, no channel named
				continue
			}
			t.Errorf("%s: socket frame %s %s during hidden activity", name, ev.Type, ev.Payload)
		}
	}
	// Control: one visible mention must move the outsider's answers, so a quiet
	// result is not a comparison that cannot see anything.
	w.say(t, w.owner, clsOpen, w.open, "control mention", map[string]any{"content": "@outsider control"})
	time.Sleep(500 * time.Millisecond)
	if len(diffKeys(before["outsider"], readAll(w.outsider, paths))) == 0 {
		t.Fatal("a visible mention changed nothing the outsider reads: the comparison is blind")
	}
	if acted < 12 || len(paths) < 40 {
		t.Fatalf("too little to compare: %d hidden actions, %d paths", acted, len(paths))
	}
	t.Logf("%d hidden actions, %d paths per observer, noise ignored %v, %d changes, %d empty hints", acted, len(paths), noiseCount(noise), moved, hints)
}

// invalidationHints are the frames that tell every member to reload a list
// without naming anything; a hidden channel's creation or rename sends them to
// all (suspicion S-L6-a: they carry the time of a hidden change, nothing else).
var invalidationHints = map[string]bool{"channels_changed": true, "permissions_changed": true}

// quietPaths are the GET routes without parameters, plus the open channel's.
func quietPaths(t *testing.T, w *canaryWorld) []string {
	t.Helper()
	var out []string
	for _, r := range apiRoutes(t, w.a) {
		if r.method != http.MethodGet || skipRoutes[r.pattern] || strings.HasPrefix(r.pattern, "/api/bot/") || strings.Contains(r.pattern, "/admin/") {
			continue
		}
		switch {
		case !strings.Contains(r.pattern, "{"):
			out = append(out, r.pattern)
		case strings.HasPrefix(r.pattern, "/api/channels/{channelID}/") && strings.Count(r.pattern, "{") == 1:
			out = append(out, strings.Replace(r.pattern, "{channelID}", w.open.String(), 1))
		}
	}
	return append(out, "/api/search?q=staff", "/api/search?q=after")
}

// readAll reads every path and flattens each JSON answer to key -> value.
func readAll(c *client, paths []string) map[string]any {
	out := map[string]any{}
	for _, p := range paths {
		res := c.get(p)
		out[p+" status"] = res.status
		var v any
		if json.Unmarshal(res.body, &v) != nil {
			out[p+" body"] = string(res.body)
			continue
		}
		flattenJSON(p, v, out)
	}
	return out
}

// flattenJSON keys every leaf by its path; arrays also record their length.
func flattenJSON(prefix string, v any, out map[string]any) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			flattenJSON(prefix+"."+k, e, out)
		}
	case []any:
		out[prefix+".#"] = len(x)
		for i, e := range x {
			// An element with an id is keyed by it: lists whose order is not
			// defined (roles of one position) must not look changed.
			key := fmt.Sprintf("%s[%d]", prefix, i)
			if obj, ok := e.(map[string]any); ok && obj["id"] != nil {
				key = fmt.Sprintf("%s[%v]", prefix, obj["id"])
			}
			flattenJSON(key, e, out)
		}
	default:
		out[prefix] = x
	}
}

func diffKeys(a, b map[string]any) map[string]bool {
	out := map[string]bool{}
	for k, v := range a {
		if bv, ok := b[k]; !ok || !reflect.DeepEqual(v, bv) {
			out[k] = true
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out[k] = true
		}
	}
	return out
}

func noiseCount(n map[string]map[string]bool) map[string]int {
	out := map[string]int{}
	for k, v := range n {
		out[k] = len(v)
	}
	return out
}

func (l *frameLog) reset() {
	l.mu.Lock()
	l.frames = nil
	l.mu.Unlock()
}

func (l *frameLog) all() []wsEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]wsEvent(nil), l.frames...)
}

// busyInStaff does everything Staff can do where the observers cannot see,
// and returns how many actions it took (each must have succeeded).
func (w *canaryWorld) busyInStaff(t *testing.T, insiderWS *wsConn) int {
	t.Helper()
	n := 0
	ok := func(what string, res response) {
		t.Helper()
		if res.status >= 300 {
			t.Fatalf("%s: %d %s", what, res.status, res.body)
		}
		n++
	}
	ch := "/api/channels/" + w.staffText.String()
	m1 := w.say(t, w.owner, clsStaff, w.staffText, "quiet mention", map[string]any{"content": "@outsider @carol @exstaff @everyone @here quiet"})
	m2 := w.say(t, w.insider, clsStaff, w.staffText, "quiet reply", map[string]any{"parent_id": m1})
	n += 2
	ok("react", w.insider.post("/api/messages/"+m1.String()+"/reactions", map[string]string{"emoji": "🎉"}))
	ok("pin", w.owner.put(ch+"/messages/"+m1.String()+"/pin", nil))
	ok("edit", w.insider.put(ch+"/messages/"+m2.String(), map[string]any{"content": "after all"}))
	ok("delete", w.owner.delete(ch+"/messages/"+m2.String()))
	ok("rename", w.owner.patch("/api/admin/channels/"+w.staffText.String(), map[string]any{"name": "staff-quiet"}))
	ok("poll", w.owner.post(ch+"/polls", map[string]any{"question": "quiet?", "options": []string{"a", "b"}}))
	ok("read", w.insider.post(ch+"/read", map[string]any{"message_id": m1}))
	ok("event", w.owner.post("/api/events", map[string]any{"title": "quiet event", "channel_id": w.staffTalk,
		"start_time": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}))
	ok("new private channel", w.owner.post("/api/admin/channels", map[string]any{"name": "quiet-new", "type": "text", "category_id": w.staffCat}))
	ok("talk chat", w.insider.post("/api/channels/"+w.staffTalk.String()+"/messages", map[string]any{"content": "in the talk @outsider"}))
	insiderWS.send("typing", map[string]any{"channel_id": w.staffText})
	insiderWS.send("voice_join", map[string]any{"channel_id": w.staffTalk})
	deadline := time.Now().Add(3 * time.Second)
	for !slices.Contains(w.a.router.Hub.VoiceMembers(w.staffTalk), w.insider.user.ID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !slices.Contains(w.a.router.Hub.VoiceMembers(w.staffTalk), w.insider.user.ID) {
		t.Fatal("insider could not take a seat in the Staff Talk")
	}
	return n + 2
}
