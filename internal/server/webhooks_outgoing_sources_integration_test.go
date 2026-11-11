//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/webhook"
)

// sources drives every way a message event comes about and checks what the
// webhook of the channel gets for it.
type sources struct {
	t     *testing.T
	a     *app
	owner *client
	rc    *receiver
	seen  map[string]bool
}

// delivered takes the next delivery: it is of event, new (no event arrives
// twice) and, when body is given, carries exactly that JSON as d.
func (s *sources) delivered(what, event string, body []byte) json.RawMessage {
	s.t.Helper()
	got := s.rc.next(s.t)
	var env webhook.Envelope
	if err := json.Unmarshal(got.body, &env); err != nil {
		s.t.Fatal(err)
	}
	if env.T != event {
		s.t.Fatalf("%s: delivered %s, want %s", what, env.T, event)
	}
	if s.seen[env.ID] {
		s.t.Fatalf("%s: event %s delivered twice", what, env.ID)
	}
	s.seen[env.ID] = true
	if body != nil && !bytes.Equal(env.D, bytes.TrimSpace(body)) {
		s.t.Fatalf("%s: d is not what the API answered\n   d %s\nbody %s", what, env.D, body)
	}
	return env.D
}

func (s *sources) ok(what string, res response, want int) response {
	s.t.Helper()
	if res.status != want {
		s.t.Fatalf("%s: %d %s", what, res.status, res.body)
	}
	return res
}

func (s *sources) id(res response) uuid.UUID {
	s.t.Helper()
	var m struct{ ID uuid.UUID }
	res.decode(s.t, &m)
	return m.ID
}

func newSources(t *testing.T, channelKind string) (*sources, uuid.UUID) {
	a := outgoingApp(t)
	owner := a.seedAdmin()
	ch := a.createChannel(owner, "alerts", channelKind)
	rc := newReceiver(t, nil)
	makeOutgoing(t, owner, ch, rc.srv.URL+"/hook", "MESSAGE_CREATE", "MESSAGE_UPDATE", "MESSAGE_DELETE", "MESSAGE_REACTIONS_UPDATE")
	return &sources{t: t, a: a, owner: owner, rc: rc, seen: map[string]bool{}}, ch
}

// Every change to a message reaches the channel's webhook once, with the
// object the API answers and the WebSocket sends.
func TestOutgoingDeliversEveryMessageChangeOnce(t *testing.T) {
	s, ch := newSources(t, "text")
	base := "/api/channels/" + ch.String() + "/messages"

	res := s.ok("send", s.owner.post(base, map[string]any{"content": "hello @cool_guy", "nonce": uuid.New()}), http.StatusCreated)
	id := s.id(res)
	s.delivered("send", "MESSAGE_CREATE", res.body)

	res = s.ok("poll", s.owner.post("/api/channels/"+ch.String()+"/polls", map[string]any{"question": "Q?", "options": []string{"a", "b"}}), http.StatusCreated)
	s.delivered("poll", "MESSAGE_CREATE", res.body)

	res = s.ok("edit", s.owner.put(base+"/"+id.String(), map[string]any{"content": "edited"}), http.StatusOK)
	s.delivered("edit", "MESSAGE_UPDATE", res.body)

	res = s.ok("pin", s.owner.put(base+"/"+id.String()+"/pin", nil), http.StatusOK)
	s.delivered("pin", "MESSAGE_UPDATE", res.body)
	s.ok("pin again", s.owner.put(base+"/"+id.String()+"/pin", nil), http.StatusOK) // no change, no event
	res = s.ok("unpin", s.owner.delete(base+"/"+id.String()+"/pin"), http.StatusOK)
	s.delivered("unpin", "MESSAGE_UPDATE", res.body)

	res = s.ok("react", s.owner.post("/api/messages/"+id.String()+"/reactions", map[string]any{"emoji": "👍"}), http.StatusOK)
	s.delivered("react", "MESSAGE_REACTIONS_UPDATE", res.body)

	s.ok("delete", s.owner.delete(base+"/"+id.String()), http.StatusNoContent)
	want, _ := json.Marshal(map[string]any{"id": id, "channel_id": ch, "parent_id": nil})
	s.delivered("delete", "MESSAGE_DELETE", want)

	bob := s.a.register(s.owner, "bob")
	gone := map[string]bool{}
	for _, content := range []string{"one", "two"} {
		gone[s.a.send(bob, ch, content).String()] = true
		s.delivered(content, "MESSAGE_CREATE", nil)
	}
	s.ok("bulk delete", s.owner.post(base+"/bulk-delete", map[string]any{"user_id": bob.user.ID, "count": 5}), http.StatusOK)
	for range 2 {
		var d struct{ ID string }
		_ = json.Unmarshal(s.delivered("bulk delete", "MESSAGE_DELETE", nil), &d)
		if !gone[d.ID] {
			t.Fatalf("bulk delete told of %q, want one of %v", d.ID, gone)
		}
		delete(gone, d.ID)
	}
	s.rc.none(t)
}

// A forum post's update and a message removed by auto-delete are message
// events like the others. The sweep here publishes to nobody, as a process
// that died after its commit would: the event is delivered all the same.
func TestOutgoingDeliversForumUpdatesAndAutoDeletes(t *testing.T) {
	s, forum := newSources(t, "forum")
	posts := "/api/channels/" + forum.String() + "/posts"

	res := s.ok("post", s.owner.post(posts, map[string]any{"title": "a post", "content": "body"}), http.StatusCreated)
	id := s.id(res)
	s.delivered("post", "MESSAGE_CREATE", res.body)
	res = s.ok("retitle", s.owner.patch(posts+"/"+id.String(), map[string]any{"title": "a better title"}), http.StatusOK)
	s.delivered("retitle", "MESSAGE_UPDATE", res.body)

	s.ok("auto-delete", s.owner.put("/api/admin/channels/"+forum.String()+"/auto-delete", map[string]any{"days": 7}), http.StatusOK)
	s.a.runSQL(`UPDATE messages SET created_at = now() - interval '30 days' WHERE id = $1`, id)
	sweeper := &channels.AutoDeleter{DB: s.a.db, Events: s.a.events, Objects: s.a.store, Audit: &audit.Log{DB: s.a.db}}
	if err := sweeper.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(map[string]any{"id": id, "channel_id": forum, "parent_id": nil})
	s.delivered("auto-delete", "MESSAGE_DELETE", want)
	s.rc.none(t)
}

// batches is an outbox that keeps how the events were handed to it.
type batches struct{ calls [][]events.Event }

func (*batches) Wants(uuid.UUID, string) bool { return true }

func (b *batches) Record(_ context.Context, _ pgx.Tx, _ uuid.UUID, evs ...events.Event) error {
	b.calls = append(b.calls, evs)
	return nil
}

// deletes takes the one batch recorded since the last look and returns the
// ids of the deleted messages in it.
func (b *batches) deletes(t *testing.T, what string) []uuid.UUID {
	t.Helper()
	if len(b.calls) != 1 {
		t.Fatalf("%s: recorded in %d batches, want 1", what, len(b.calls))
	}
	var ids []uuid.UUID
	for _, ev := range b.calls[0] {
		ids = append(ids, ev.(wire.MessageDelete).ID)
	}
	b.calls = nil
	return ids
}

// A bulk delete and a sweep hold the row locks of every message they remove.
// They hand all the deletions to the outbox at once, so the queue is written
// with one statement and not with one per message.
func TestOutgoingRecordsTheDeletesOfABatchTogether(t *testing.T) {
	a := outgoingApp(t)
	owner := a.seedAdmin()
	ch := a.createChannel(owner, "alerts", "text")
	bob := a.register(owner, "bob")
	out := &batches{}
	core.AttachOutbox(a.db, out)
	t.Cleanup(func() { core.DetachOutbox(a.db, out) })

	sent := map[uuid.UUID]bool{}
	send := func(n int) {
		for range n {
			sent[a.send(bob, ch, "soon gone")] = true
		}
		out.calls = nil
	}
	gone := func(what string, n int) {
		t.Helper()
		ids := out.deletes(t, what)
		for _, id := range ids {
			if !sent[id] {
				t.Fatalf("%s: recorded %s, which it did not remove", what, id)
			}
			delete(sent, id)
		}
		if len(ids) != n || len(sent) != 0 {
			t.Fatalf("%s: recorded %d deletions, want %d", what, len(ids), n)
		}
	}

	send(4)
	if res := owner.post("/api/channels/"+ch.String()+"/messages/bulk-delete", map[string]any{"user_id": bob.user.ID, "count": 10}); res.status != http.StatusOK {
		t.Fatalf("bulk delete: %d %s", res.status, res.body)
	}
	gone("bulk delete", 4)

	send(3)
	if res := owner.put("/api/admin/channels/"+ch.String()+"/auto-delete", map[string]any{"days": 7}); res.status != http.StatusOK {
		t.Fatalf("auto-delete: %d %s", res.status, res.body)
	}
	a.runSQL(`UPDATE messages SET created_at = now() - interval '30 days' WHERE channel_id = $1`, ch)
	sweeper := &channels.AutoDeleter{DB: a.db, Events: a.events, Objects: a.store, Audit: &audit.Log{DB: a.db}}
	if err := sweeper.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	gone("sweep", 3)
}
