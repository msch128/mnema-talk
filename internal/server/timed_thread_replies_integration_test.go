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

// The writes that keep a copy (a report's context, a bulk delete's audit
// entry) and the conversation list leave out a reply of an ended thread too,
// and the reply cannot be edited any more.
func TestAReplyOfAnEndedThreadIsNotCopiedOrEdited(t *testing.T) {
	w := newTimedWorld(t)
	a := w.a
	root := sendTimed(t, w.alice, w.ch, map[string]any{"content": "short thread", "lifetime_seconds": 60})
	reply := sendTimed(t, w.bob, w.ch, map[string]any{"content": "zqcopyword", "parent_id": root.ID})
	neighbour := sendTimed(t, w.alice, w.ch, map[string]any{"content": "a later message"})
	dmRes := w.alice.post("/api/dms", map[string]any{"user_ids": []uuid.UUID{w.bob.user.ID}})
	wantStatus(t, "create dm", dmRes, http.StatusCreated)
	var dm struct {
		ID uuid.UUID `json:"id"`
	}
	dmRes.decode(t, &dm)
	dmRoot := sendTimed(t, w.alice, dm.ID, map[string]any{"content": "short dm thread", "lifetime_seconds": 60})
	sendTimed(t, w.bob, dm.ID, map[string]any{"content": "zqdmreply", "parent_id": dmRoot.ID})

	a.endLifetime(root.ID, dmRoot.ID)

	wantStatus(t, "edit the reply", w.bob.put(w.path("/messages/"+reply.ID.String()), map[string]any{"content": "again"}), http.StatusNotFound)
	wantStatus(t, "report", w.bob.post("/api/messages/"+neighbour.ID.String()+"/report", map[string]any{"category": "spam"}), http.StatusCreated)
	if n := a.countRows(`SELECT COUNT(*) FROM message_reports WHERE snapshot::text LIKE '%zqcopyword%'`); n != 0 {
		t.Errorf("a report copied the reply of the ended thread (%d)", n)
	}
	wantStatus(t, "bulk delete", w.owner.post(w.path("/messages/bulk-delete"), map[string]any{"user_id": w.bob.user.ID, "count": 5}), http.StatusOK)
	if n := a.countRows(`SELECT COUNT(*) FROM audit_log WHERE details::text LIKE '%zqcopyword%'`); n != 0 {
		t.Errorf("the bulk delete audited the reply of the ended thread (%d)", n)
	}
	if res := w.bob.get("/api/dms"); strings.Contains(string(res.body), "zqdmreply") {
		t.Errorf("the conversation list shows the reply of the ended thread: %s", res.body)
	}
}
