//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"
)

// TestAMentionNeverPushesToSomeoneWhoCannotRead names every member, @everyone
// and @here in places only some of them may read (a Staff channel, a thread
// there, an edit that adds the names, and a conversation) and requires the
// push service to reach readers only. visibility_surfaces covers plain
// messages; this covers the mention paths, which pick recipients by name.
func TestAMentionNeverPushesToSomeoneWhoCannotRead(t *testing.T) {
	w := newSurfaceWorld(t)
	names := "@alice @bobby @carol @everyone @here"
	staff := "/api/channels/" + w.loc[locStaff].String()
	cases := []struct {
		what string
		send func() response
		may  map[string]bool
	}{
		{"staff message", func() response {
			return w.admin.post(staff+"/messages", map[string]any{"content": names + " staffonly"})
		}, map[string]bool{"alice": true}},
		{"staff thread reply", func() response {
			root := w.post(t, locStaff, "root")
			return w.admin.post(staff+"/messages", map[string]any{"content": names + " threaded", "parent_id": root})
		}, map[string]bool{"alice": true}},
		{"staff edit that adds names", func() response {
			id := w.post(t, locStaff, "plain")
			return w.admin.put(staff+"/messages/"+id.String(), map[string]any{"content": names + " edited"})
		}, map[string]bool{"alice": true}},
		{"conversation", func() response {
			wantStatus(t, "answer", w.bob.post("/api/channels/"+w.loc[locDMAB].String()+"/messages", map[string]any{"content": "hi"}), http.StatusCreated)
			return w.alice.post("/api/channels/"+w.loc[locDMAB].String()+"/messages", map[string]any{"content": names + " @cool_guy private"})
		}, map[string]bool{"alice": true, "bobby": true}}, // alice is pushed bobby's answer
	}
	for _, c := range cases {
		time.Sleep(300 * time.Millisecond)
		before := len(w.push.received())
		if res := c.send(); res.status >= 300 {
			t.Fatalf("%s: %d %s", c.what, res.status, res.body)
		}
		time.Sleep(700 * time.Millisecond)
		got := map[string]bool{}
		for _, p := range w.push.received()[before:] {
			got[p[1:]] = true
		}
		for who := range got {
			if !c.may[who] {
				t.Errorf("%s: a push reached %s, who cannot read it (pushes: %v)", c.what, who, readerNames(got))
			}
		}
		if c.what != "staff edit that adds names" && len(got) == 0 {
			t.Errorf("%s: no push at all: the probe reaches nothing", c.what)
		}
	}
}
