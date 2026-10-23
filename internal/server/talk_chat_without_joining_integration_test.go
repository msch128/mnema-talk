//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
)

// A Talk's chat is read and written with the channel permissions alone:
// View channel to read, Send messages to write. Neither Connect nor being in
// the call is needed.
func TestTalkChatNeedsNoCallMembership(t *testing.T) {
	f := newAccessFixture(t, true)
	lounge := f.a.createChannel(f.owner, "Lounge", "voice")
	base := "/api/channels/" + lounge.String()
	// Nobody may connect to the Lounge; everybody may still see and write in it.
	wantStatus(t, "deny connect", putOverwrites(f.owner, "channels", lounge, overwrite("role", f.everyone, 0, perm.Connect)), http.StatusNoContent)
	outWS := mustDial(t, f.outsider)

	first := postMessage(t, f.owner, lounge, map[string]any{"content": "who is reading along?"})
	if p, ok := outWS.expect("message_create", soon); !ok || !strings.Contains(string(p), "who is reading along?") {
		t.Fatalf("a viewer outside the call misses the live message: %s", p)
	}
	wantStatus(t, "read history", f.outsider.get(base+"/messages"), http.StatusOK)
	wantStatus(t, "read marker", f.outsider.post(base+"/read", nil), http.StatusNoContent)
	postMessage(t, f.outsider, lounge, map[string]any{"content": "only read, not joined"})
	if got, status := f.owner.search(t, map[string]string{"q": "joined"}); status != http.StatusOK || len(got.Messages) != 1 {
		t.Fatalf("search of the Talk chat: %d %+v", status, got.Messages)
	}
	wantStatus(t, "thread", f.outsider.get("/api/messages/"+first.String()+"/thread"), http.StatusOK)

	// Without Send messages the chat stays readable but not writable.
	wantStatus(t, "deny send", putOverwrites(f.owner, "channels", lounge, overwrite("role", f.everyone, 0, perm.Connect|perm.SendMessages)), http.StatusNoContent)
	wantStatus(t, "read-only history", f.outsider.get(base+"/messages"), http.StatusOK)
	if res := f.outsider.post(base+"/messages", map[string]any{"content": "not allowed"}); res.status != http.StatusForbidden {
		t.Fatalf("writing without Send messages: %d %s", res.status, res.body)
	}
}

// A private Talk's chat is invisible to members without View channel: REST,
// search, read state and live events.
func TestPrivateTalkChatStaysHidden(t *testing.T) {
	f := newAccessFixture(t, true)
	staffTalk := f.privateChannel(t, "Staff Talk", "voice")
	base := "/api/channels/" + staffTalk.String()
	outWS, inWS := mustDial(t, f.outsider), mustDial(t, f.insider)

	postMessage(t, f.owner, staffTalk, map[string]any{"content": "geheimes Wort"})
	if _, ok := inWS.expect("message_create", soon); !ok {
		t.Fatal("the insider misses the message")
	}
	if p, ok := outWS.expect("message_create", none); ok {
		t.Fatalf("a private Talk's chat leaked live: %s", p)
	}
	wantStatus(t, "insider reads", f.insider.get(base+"/messages"), http.StatusOK)

	hidden := map[string]response{
		"history": f.outsider.get(base + "/messages"),
		"read":    f.outsider.post(base+"/read", nil),
		"write":   f.outsider.post(base+"/messages", map[string]any{"content": "x"}),
	}
	for what, res := range hidden {
		if res.status != http.StatusNotFound && res.status != http.StatusForbidden {
			t.Errorf("%s of a hidden Talk chat: %d %s", what, res.status, res.body)
		}
	}
	if res := f.outsider.get("/api/read-state"); res.status != http.StatusOK || strings.Contains(string(res.body), staffTalk.String()) {
		t.Errorf("read states reveal the private Talk: %d %s", res.status, res.body)
	}
	if got, status := f.outsider.search(t, map[string]string{"q": "geheimes"}); status != http.StatusOK || len(got.Messages) != 0 {
		t.Errorf("search leaks the private Talk chat: %d %+v", status, got.Messages)
	}
}
