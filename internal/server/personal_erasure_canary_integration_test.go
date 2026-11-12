//go:build integration

package server

import (
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The canary oracle of the erasure: the member writes a distinct word into
// each of the places in canarySites through the routes, the account is
// erased, and every row of every table (as JSON) is searched for each word. A
// word may survive only where a documented rule keeps it, listed in canaryKept
// with that rule. A forward by another member is not a copy: its quote is
// read from the original, so the word goes with it.

// canaryKept are the places whose words stay after the erasure. None today.
var canaryKept = map[string]string{}

type canarySite struct {
	name  string
	write func(f *erasureWorld, word string) response
}

type erasureWorld struct {
	a                 *app
	admin, alice, bob *client
	ch, bobMsg, dm    uuid.UUID
	aliceMsg          uuid.UUID
}

var canarySites = []canarySite{
	{"message", func(f *erasureWorld, w string) response {
		return f.alice.post("/api/channels/"+f.ch.String()+"/messages", map[string]any{"content": w})
	}},
	{"edited away", func(f *erasureWorld, w string) response {
		return f.alice.put("/api/channels/"+f.ch.String()+"/messages/"+f.aliceMsg.String(), map[string]any{"content": w})
	}},
	{"bio", func(f *erasureWorld, w string) response {
		return f.alice.put("/api/users/me/profile", map[string]string{"bio": w})
	}},
	{"status", func(f *erasureWorld, w string) response {
		return f.alice.put("/api/users/me/status", map[string]string{"status_text": w})
	}},
	{"file name", func(f *erasureWorld, w string) response {
		return f.alice.upload("/api/channels/"+f.ch.String()+"/upload", "file", w+".png", "image/png", pngBytes, map[string]string{"content": "a file"})
	}},
	{"direct message", func(f *erasureWorld, w string) response { return f.alice.dmPost(f.dm, w) }},
	{"saved note", func(f *erasureWorld, w string) response {
		return f.alice.put("/api/me/saved/"+f.bobMsg.String(), map[string]string{"note": w})
	}},
	{"report note", func(f *erasureWorld, w string) response {
		return f.alice.post("/api/messages/"+f.bobMsg.String()+"/report", map[string]any{"category": "spam", "note": w})
	}},
	{"scheduled", func(f *erasureWorld, w string) response {
		return f.alice.post("/api/channels/"+f.ch.String()+"/scheduled", map[string]any{"content": w, "send_at": time.Now().Add(time.Hour)})
	}},
	{"forwarded", func(f *erasureWorld, w string) response {
		m := f.a.send(f.alice, f.ch, w)
		return f.bob.forward(m, f.ch, "")
	}},
}

func TestErasureLeavesNoWordOfThePersonOutsideTheDocumentedPlaces(t *testing.T) {
	a, _ := mailApp(t)
	f := &erasureWorld{a: a, admin: a.seedAdmin()}
	f.alice, f.bob = a.register(f.admin, "alice"), a.register(f.admin, "bob")
	ch := a.createChannel(f.admin, "general", "text")
	f.ch, f.bobMsg = ch, a.send(f.bob, ch, "bob's words")
	f.aliceMsg = a.send(f.alice, ch, "to be edited")
	f.dm = f.alice.mustDM(t, f.bob).ID

	words := map[string]string{}
	for _, s := range canarySites {
		word := "canary" + strings.ReplaceAll(s.name, " ", "") + "x7q"
		words[s.name] = word
		if res := s.write(f, word); res.status >= 300 {
			t.Fatalf("%s: %d %s", s.name, res.status, res.body)
		}
		if len(a.everyRowMentioning(word)) == 0 {
			t.Fatalf("%s: the word was stored nowhere, the site proves nothing", s.name)
		}
	}
	// The edit keeps the earlier version: edit once more so the word is history.
	if res := f.alice.put("/api/channels/"+f.ch.String()+"/messages/"+f.aliceMsg.String(), map[string]any{"content": "final"}); res.status != http.StatusOK {
		t.Fatalf("edit: %d %s", res.status, res.body)
	}
	if res := a.deleteAccount(f.alice, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	names := make([]string, 0, len(words))
	for n := range words {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, site := range names {
		hits := a.everyRowMentioning(words[site])
		_, kept := canaryKept[site]
		switch {
		case len(hits) > 0 && !kept:
			t.Errorf("%s: the erased person's word is still in %v", site, hits)
		case len(hits) == 0 && kept:
			t.Errorf("%s: listed as kept, but the word is gone: drop the entry", site)
		}
	}
}
