//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestWritesThatNameAHiddenObjectAnswerLikeAMissingOne covers the requests
// that are not reads but take another object by id in their body or path:
// a reply, a thread answer, an attachment, a scheduled reply, an event in a
// Talk, a reminder, and the admin routes a moderator holds without seeing the
// Staff channels. Each is sent once with the hidden object's id and once with
// an id nobody has: the two answers must be the same bytes, a refusal, and
// free of canary words. Every probe is also checked to have named a hidden id,
// so a probe that names none fails instead of passing.
func TestWritesThatNameAHiddenObjectAnswerLikeAMissingOne(t *testing.T) {
	w := newCanaryWorld(t, 3)
	w.build(t)
	msg := w.firstOf(t, clsStaff, "message")
	file := w.firstOf(t, clsStaff, "media")
	event := w.firstOf(t, clsStaff, "event")
	open := "/api/channels/" + w.open.String()
	later := inFuture(time.Hour)
	probes := []echoProbe{
		{"reply to a hidden message", "POST", open + "/messages", map[string]any{"content": "re", "reply_to_id": msg}},
		{"answer in a hidden thread", "POST", open + "/messages", map[string]any{"content": "re", "parent_id": msg}},
		{"attach a hidden file", "POST", open + "/messages", map[string]any{"content": "look", "attachment_ids": []string{file}}},
		{"scheduled reply to a hidden message", "POST", open + "/scheduled", map[string]any{"content": "re", "send_at": later, "reply_to_id": msg}},
		{"scheduled answer in a hidden thread", "POST", open + "/scheduled", map[string]any{"content": "re", "send_at": later, "parent_id": msg}},
		{"event in a hidden Talk", "POST", "/api/events", map[string]any{"title": "x", "start_time": later, "channel_id": w.staffTalk.String()}},
		{"reminder on a hidden message", "POST", "/api/me/reminders", map[string]any{"message_id": msg, "remind_at": later}},
		{"answer a hidden event", "PUT", "/api/events/" + event + "/rsvp", map[string]any{"response": "going"}},
		{"comment a hidden event", "POST", "/api/events/" + event + "/comments", map[string]any{"content": "hi"}},
		{"edit a hidden event", "PATCH", "/api/events/" + event, map[string]any{"title": "x"}},
		{"cancel a hidden event", "POST", "/api/events/" + event + "/cancel", nil},
		{"forward a hidden message", "POST", "/api/messages/" + msg + "/forward", map[string]any{"channel_id": w.open.String()}},
	}
	mod := []echoProbe{
		{"overwrites of a hidden channel", "GET", "/api/admin/channels/" + w.staffText.String() + "/overwrites", nil},
		{"rename a hidden channel", "PATCH", "/api/admin/channels/" + w.staffText.String(), map[string]any{"name": "x"}},
		{"duplicate a hidden channel", "POST", "/api/admin/channels/" + w.staffText.String() + "/duplicate", map[string]any{}},
		{"auto-delete in a hidden channel", "PUT", "/api/admin/channels/" + w.staffText.String() + "/auto-delete", map[string]any{"seconds": 3600}},
		{"open a hidden channel", "PUT", "/api/admin/channels/" + w.staffText.String() + "/overwrites", []any{}},
		{"delete a hidden channel", "DELETE", "/api/admin/channels/" + w.staffText.String(), nil},
		{"channel in a hidden category", "POST", "/api/admin/channels", map[string]any{"name": "x", "type": "text", "category_id": w.staffCat.String()}},
		{"rename a hidden category", "PATCH", "/api/admin/categories/" + w.staffCat.String(), map[string]any{"name": "x"}},
		{"overwrites of a hidden category", "GET", "/api/admin/categories/" + w.staffCat.String() + "/overwrites", nil},
		{"bulk delete in a hidden channel", "POST", "/api/channels/" + w.staffText.String() + "/messages/bulk-delete", map[string]any{"message_ids": []string{msg}}},
		{"edit history of a hidden message", "GET", "/api/channels/" + w.staffText.String() + "/messages/" + msg + "/edits", nil},
		{"disconnect from a hidden Talk", "POST", "/api/channels/" + w.staffTalk.String() + "/voice/" + w.insider.user.ID.String() + "/disconnect", nil},
		{"kick from a hidden Talk", "POST", "/api/admin/users/" + w.insider.user.ID.String() + "/kick", map[string]any{"channel_id": w.staffTalk.String()}},
	}
	hidden := w.hiddenFrom("outsider")
	for _, p := range probes {
		p.mustMatchMissing(t, w, "outsider", w.outsider, hidden)
	}
	hidden = w.hiddenFrom("mod")
	for _, p := range mod {
		p.mustMatchMissing(t, w, "mod", w.mod, hidden)
	}
	// The same objects are reachable for Staff, so a refusal above is about the
	// observer and not a broken probe.
	wantStatus(t, "insider reply", w.insider.post("/api/channels/"+w.staffText.String()+"/messages",
		map[string]any{"content": "re", "reply_to_id": msg}), http.StatusCreated)
	wantStatus(t, "owner overwrites", w.owner.get("/api/admin/channels/"+w.staffText.String()+"/overwrites"), http.StatusOK)
}

// TestAnExportCarriesNothingOutsideTheMembersClasses builds each observer's
// data export in the canary world and requires every word in it to be one the
// observer may read: exstaff's own words from the Staff channel belong in it,
// the words of others there (the message it answered, the thread root, the
// channel's names) do not, and the moderator note about outsider never does.
func TestAnExportCarriesNothingOutsideTheMembersClasses(t *testing.T) {
	w := newCanaryWorld(t, 5)
	w.build(t)
	for who, c := range map[string]*client{"exstaff": w.exstaff, "outsider": w.outsider, "carol": w.carol} {
		wantStatus(t, who+" export", w.a.requestExport(c, "member-password-123", ""), http.StatusAccepted)
		if st := w.a.waitForExport(c); st.State != "ready" {
			t.Fatalf("%s: export %s", who, st.State)
		}
		res := c.get("/api/users/me/export/download")
		wantStatus(t, who+" download", res, http.StatusOK)
		own := 0
		for name, body := range unzipAll(t, res.body) {
			for _, word := range canaryWord.FindAllString(body, -1) {
				cls, ok := w.words[word]
				switch {
				case !ok:
				case !mayRead(who, cls):
					t.Errorf("%s: export file %s carries %q of %q (class %s)", who, name, word, w.wordWhat[word], cls)
				case cls != clsOpen:
					own++
				}
			}
		}
		if who == "exstaff" && own == 0 {
			t.Error("exstaff's own Staff words are missing from its export: the probe reads nothing")
		}
	}
}

// echoKnownDifferences are the probes of a recorded finding whose hidden and
// missing answers differ today. The list may only shrink.
var echoKnownDifferences = map[string]string{
	// The message checks say "in the same conversation" / "in the same channel"
	// for a message that exists elsewhere and "not found" for one that does not.
	"reply to a hidden message":           "H-L6-02",
	"answer in a hidden thread":           "H-L6-02",
	"scheduled reply to a hidden message": "H-L6-02",
	"scheduled answer in a hidden thread": "H-L6-02",
	// 404 "category not found" for a hidden category, 400 for a missing one.
	"channel in a hidden category": "H-L6-02",
}

type echoProbe struct {
	what, method, path string
	body               any
}

// firstOf returns the first object id of a class and pool kind.
func (w *canaryWorld) firstOf(t *testing.T, c canaryClass, kind string) string {
	t.Helper()
	for _, id := range w.pool[kind] {
		if w.ids[id] == c {
			return id
		}
	}
	t.Fatalf("no %s %s in the world", c, kind)
	return ""
}

// hiddenFrom lists the ids of objects the observer may not see.
func (w *canaryWorld) hiddenFrom(who string) []string {
	var out []string
	for id, c := range w.ids {
		if !mayRead(who, c) {
			out = append(out, id)
		}
	}
	return out
}

// swap replaces every hidden id in s by its fresh twin.
func swap(s string, twins map[string]string) string {
	for id, fresh := range twins {
		s = strings.ReplaceAll(s, id, fresh)
	}
	return s
}

func swapBody(v any, twins map[string]string) any {
	switch x := v.(type) {
	case string:
		return swap(x, twins)
	case []string:
		out := make([]string, len(x))
		for i, e := range x {
			out[i] = swap(e, twins)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = swapBody(e, twins)
		}
		return out
	}
	return v
}

func (p echoProbe) mustMatchMissing(t *testing.T, w *canaryWorld, who string, c *client, hidden []string) {
	t.Helper()
	twins := map[string]string{}
	for _, id := range hidden {
		twins[id] = uuid.NewString()
	}
	twinPath, twinBody := swap(p.path, twins), swapBody(p.body, twins)
	if twinPath == p.path && fmtAny(twinBody) == fmtAny(p.body) {
		t.Fatalf("%s: names no hidden id", p.what)
	}
	res := c.do(p.method, p.path, p.body, nil)
	twin := c.do(p.method, twinPath, twinBody, nil)
	if res.status < 400 {
		t.Errorf("%s (%s): a hidden object was accepted: %d %s", p.what, who, res.status, res.body)
	}
	same := res.status == twin.status && bytes.Equal(res.body, twin.body)
	if id := echoKnownDifferences[p.what]; id != "" {
		// Known failure, inverted: it must still differ, and turns red when fixed.
		if same {
			t.Errorf("%s (%s): %s no longer reproduces: drop it from echoKnownDifferences", p.what, who, id)
		}
		return
	}
	if !same {
		t.Errorf("%s (%s): a hidden object answers %d %s, a missing one %d %s", p.what, who, res.status, res.body, twin.status, twin.body)
	}
	for _, word := range canaryWord.FindAllString(string(res.body), -1) {
		if cls, ok := w.words[word]; ok && !mayRead(who, cls) {
			t.Errorf("%s (%s): the answer carries %q of %q", p.what, who, word, w.wordWhat[word])
		}
	}
}

func fmtAny(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
