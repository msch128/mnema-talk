//go:build integration

package server

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/expiry"
)

// Every entry point that creates a message takes a lifetime or refuses it by
// decision (doc/timed-messages.md, "API"; the entry points are those of
// insertMessageCallers in internal/chat/messages/door_test.go).

func TestEveryWayToPostTakesALifetimeOrRefusesIt(t *testing.T) {
	w := newTimedWorld(t)
	a := w.a

	// An upload: the form field.
	res := w.alice.upload(w.path("/upload"), "file", "a.png", "image/png", pngBytes, map[string]string{"lifetime_seconds": "300"})
	wantStatus(t, "upload", res, http.StatusCreated)
	var up timedMessage
	res.decode(t, &up)
	if up.ExpiresAt == nil || up.ExpiresAt.Sub(up.CreatedAt) != 5*time.Minute {
		t.Errorf("upload with a lifetime: %+v", up)
	}
	wantStatus(t, "upload out of bounds", w.alice.upload(w.path("/upload"), "file", "b.png", "image/png", pngBytes, map[string]string{"lifetime_seconds": "10"}), http.StatusBadRequest)

	// A scheduled message keeps its lifetime until the server posts it.
	sched := w.alice.post(w.path("/scheduled"), map[string]any{"content": "later and short", "send_at": time.Now().Add(time.Hour), "lifetime_seconds": 600})
	wantStatus(t, "schedule", sched, http.StatusCreated)
	if n := a.countRows(`SELECT COUNT(*) FROM scheduled_messages WHERE lifetime_seconds = 600`); n != 1 {
		t.Errorf("scheduled messages with the lifetime: %d", n)
	}
	wantStatus(t, "schedule out of bounds", w.alice.post(w.path("/scheduled"), map[string]any{"content": "x", "send_at": time.Now().Add(time.Hour), "lifetime_seconds": 1}), http.StatusBadRequest)

	// A forum post is meant to last; a reply in it may have a lifetime.
	forum := a.createChannel(w.owner, "ideas", "forum")
	post := w.alice.post("/api/channels/"+forum.String()+"/posts", map[string]any{"title": "Kitchen", "content": "new kitchen", "lifetime_seconds": 60})
	wantStatus(t, "forum post with a lifetime", post, http.StatusBadRequest)
	post = w.alice.post("/api/channels/"+forum.String()+"/posts", map[string]any{"title": "Kitchen", "content": "new kitchen"})
	wantStatus(t, "forum post", post, http.StatusCreated)
	var root struct{ ID uuid.UUID }
	post.decode(t, &root)
	sendTimed(t, w.bob, forum, map[string]any{"content": "reply", "parent_id": root.ID, "lifetime_seconds": 60})
	wantStatus(t, "forum root upload with a lifetime", w.alice.upload("/api/channels/"+forum.String()+"/upload", "file", "c.png", "image/png", pngBytes,
		map[string]string{"title": "Pic", "lifetime_seconds": "60"}), http.StatusBadRequest)

	// A poll takes no lifetime (its own close time ends it).
	wantStatus(t, "poll", w.alice.post(w.path("/polls"), map[string]any{"question": "tea?", "options": []string{"yes", "no"}, "lifetime_seconds": 60}), http.StatusBadRequest)
}

func TestABotPostsWithALifetimeLikeAMember(t *testing.T) {
	f := newBotFixture(t, false)
	res := f.client.post(botPath(f.open, "/messages"), map[string]any{"content": "status: busy", "lifetime_seconds": 120})
	wantStatus(t, "bot post", res, http.StatusCreated)
	var m timedMessage
	res.decode(t, &m)
	if m.ExpiresAt == nil || m.ExpiresAt.Sub(m.CreatedAt) != 2*time.Minute {
		t.Errorf("bot message: %+v", m)
	}
	wantStatus(t, "bot out of bounds", f.client.post(botPath(f.open, "/messages"), map[string]any{"content": "x", "lifetime_seconds": 5}), http.StatusBadRequest)
}

// An ended message is gone for every read and every action on it before the
// sweeper removed it; the canary sweep checks the same across every route.
func TestAnEndedMessageIsGoneBeforeTheSweep(t *testing.T) {
	w := newTimedWorld(t)
	a := w.a
	root := sendTimed(t, w.alice, w.ch, map[string]any{"content": "ended root @bob zqendedword", "lifetime_seconds": 60})
	res := w.alice.upload(w.path("/upload"), "file", "x.png", "image/png", pngBytes, map[string]string{"lifetime_seconds": "60", "content": "ended file"})
	wantStatus(t, "upload", res, http.StatusCreated)
	var file struct {
		ID          uuid.UUID
		Attachments []struct{ URL string }
	}
	res.decode(t, &file)
	wantStatus(t, "save", w.bob.put("/api/me/saved/"+root.ID.String(), nil), http.StatusOK)
	wantStatus(t, "remind", w.bob.post("/api/me/reminders", map[string]any{"message_id": root.ID, "remind_at": time.Now().Add(time.Hour)}), http.StatusCreated)
	a.endLifetime(root.ID, file.ID)

	for _, path := range []string{
		w.path("/messages"), w.path("/pins"), "/api/mentions", "/api/me/saved", "/api/me/reminders",
		"/api/search?q=zqendedword", w.path("/messages?around=" + root.ID.String()),
	} {
		res := w.bob.get(path)
		if strings.Contains(string(res.body), "zqendedword") || strings.Contains(string(res.body), root.ID.String()) && !strings.Contains(path, root.ID.String()) {
			t.Errorf("GET %s shows the ended message: %d %s", path, res.status, res.body)
		}
	}
	wantStatus(t, "the file", w.bob.get(file.Attachments[0].URL), http.StatusNotFound)
	wantStatus(t, "edit", w.alice.put(w.path("/messages/"+root.ID.String()), map[string]any{"content": "again"}), http.StatusNotFound)
	wantStatus(t, "react", w.bob.post("/api/messages/"+root.ID.String()+"/reactions", map[string]string{"emoji": "👍"}), http.StatusNotFound)
	wantStatus(t, "report", w.bob.post("/api/messages/"+root.ID.String()+"/report", map[string]any{"category": "spam"}), http.StatusNotFound)
	wantStatus(t, "thread reply", w.bob.post(w.path("/messages"), map[string]any{"content": "late", "parent_id": root.ID}), http.StatusBadRequest)
	if n := a.countRows(`SELECT COUNT(*) FROM messages WHERE id = $1`, root.ID); n != 1 {
		t.Fatal("the row should still be there before the sweep: the reads filtered nothing")
	}
}

// The sweeper sleeps until the next end and is woken by an earlier one.
func TestTheSweeperWakesForAnEarlierEnd(t *testing.T) {
	w := newTimedWorld(t)
	a := w.a
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	s := expiry.New(expiry.Config{DB: a.db, Events: a.events, Objects: a.store})
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	later := sendTimed(t, w.alice, w.ch, map[string]any{"content": "a week", "lifetime_seconds": 7 * 24 * 3600})
	soon := sendTimed(t, w.alice, w.ch, map[string]any{"content": "a minute", "lifetime_seconds": 60})
	a.endLifetime(soon.ID)
	s.Wake(time.Now())
	deadline := time.Now().Add(5 * time.Second)
	for a.countRows(`SELECT COUNT(*) FROM messages WHERE id = $1`, soon.ID) == 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if a.countRows(`SELECT COUNT(*) FROM messages WHERE id = $1`, soon.ID) != 0 {
		t.Fatal("the woken sweeper did not remove the ended message")
	}
	if a.countRows(`SELECT COUNT(*) FROM messages WHERE id = $1`, later.ID) != 1 {
		t.Fatal("the sweeper removed a message that has not ended")
	}
	stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the sweeper did not stop with its context")
	}
}

// The server measures a picture's size at upload and keeps it on the
// attachment, so the "File removed" tile takes the same room.
func TestAPicturesSizeIsKeptOnItsAttachment(t *testing.T) {
	w := newTimedWorld(t)
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	img.Set(1, 1, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	res := w.alice.upload(w.path("/upload"), "file", "wide.png", "image/png", buf.Bytes(), nil)
	wantStatus(t, "upload", res, http.StatusCreated)
	var m struct {
		Attachments []struct{ Width, Height *int }
	}
	res.decode(t, &m)
	if len(m.Attachments) != 1 || m.Attachments[0].Width == nil || *m.Attachments[0].Width != 40 || *m.Attachments[0].Height != 30 {
		t.Fatalf("attachment size: %+v", m.Attachments)
	}
	text := w.alice.upload(w.path("/upload"), "file", "notes.txt", "text/plain", []byte("hello"), nil)
	wantStatus(t, "text upload", text, http.StatusCreated)
	if strings.Contains(string(text.body), `"width"`) {
		t.Errorf("a text file got a size: %s", text.body)
	}
}

// A moderator's bulk delete of a member's latest messages leaves out an ended
// message the sweeper has not removed yet: it neither counts towards the
// number nor copies its text into the audit log (review round 2, finding 2).
func TestABulkDeleteLeavesAnEndedMessageToTheSweeper(t *testing.T) {
	w := newTimedWorld(t)
	ended := sendTimed(t, w.alice, w.ch, map[string]any{"content": "ended-secret-words", "lifetime_seconds": 60})
	kept := sendTimed(t, w.alice, w.ch, map[string]any{"content": "a plain message"})
	w.a.endLifetime(ended.ID)
	res := w.owner.post(w.path("/messages/bulk-delete"), map[string]any{"user_id": w.alice.user.ID, "count": 5})
	wantStatus(t, "bulk delete", res, http.StatusOK)
	var out struct{ Deleted int }
	res.decode(t, &out)
	if out.Deleted != 1 {
		t.Errorf("deleted %d, want the one live message", out.Deleted)
	}
	if n := w.a.countRows(`SELECT COUNT(*) FROM audit_log WHERE details::text LIKE '%ended-secret-words%'`); n != 0 {
		t.Errorf("the ended message's text reached the audit log %d times", n)
	}
	if n := w.a.countRows(`SELECT COUNT(*) FROM messages WHERE id = $1`, kept.ID); n != 0 {
		t.Error("the live message was not deleted")
	}
}
