//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A reply ends with its thread: once the root's lifetime is over, no read
// shows a reply of it, also before the sweeper removed the root (and its
// replies with it). A reply needs no lifetime of its own for that.
func TestAReplyEndsWithItsTimedRootBeforeTheSweep(t *testing.T) {
	w := newTimedWorld(t)
	a := w.a
	root := sendTimed(t, w.alice, w.ch, map[string]any{"content": "short thread", "lifetime_seconds": 60})
	reply := sendTimed(t, w.bob, w.ch, map[string]any{"content": "the code is zqreplyword @alice", "parent_id": root.ID})
	res := w.bob.upload(w.path("/upload"), "file", "r.png", "image/png", pngBytes, map[string]string{"parent_id": root.ID.String(), "content": "zqreplyfile"})
	wantStatus(t, "upload into the thread", res, http.StatusCreated)
	var file struct {
		ID          uuid.UUID
		Attachments []struct{ URL string }
	}
	res.decode(t, &file)
	wantStatus(t, "save", w.alice.put("/api/me/saved/"+reply.ID.String(), nil), http.StatusOK)
	wantStatus(t, "remind", w.alice.post("/api/me/reminders", map[string]any{"message_id": reply.ID, "remind_at": time.Now().Add(time.Hour)}), http.StatusCreated)
	wantStatus(t, "the file while the thread lasts", w.alice.get(file.Attachments[0].URL), http.StatusOK)

	a.endLifetime(root.ID)

	for _, path := range []string{"/api/mentions", "/api/me/saved", "/api/me/reminders", "/api/search?q=zqreplyword", "/api/search?q=zqreplyfile"} {
		res := w.alice.get(path)
		if strings.Contains(string(res.body), "zqreply") || strings.Contains(string(res.body), reply.ID.String()) {
			t.Errorf("GET %s shows a reply of the ended thread: %d %s", path, res.status, res.body)
		}
	}
	wantStatus(t, "the reply's file", w.alice.get(file.Attachments[0].URL), http.StatusNotFound)
	wantStatus(t, "react to the reply", w.alice.post("/api/messages/"+reply.ID.String()+"/reactions", map[string]string{"emoji": "👍"}), http.StatusNotFound)
	if n := a.countRows(`SELECT COUNT(*) FROM messages WHERE parent_id = $1`, root.ID); n != 2 {
		t.Fatalf("the replies should still be stored before the sweep, got %d", n)
	}
}
