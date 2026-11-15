//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ended adds messages whose lifetime has ended but which the sweeper (held in
// the canary world) has not removed: their words are of the class nobody
// reads. A message in the open channel that names observers, a thread reply,
// one that is quoted by a live reply, saved and with a reminder, and one in the
// conversation. They went out on the sockets while they lived, so the frames
// do not count for them; every route must leave them out from their end on.
func (w *canaryWorld) ended(t *testing.T) {
	t.Helper()
	timed := func(c *client, ch uuid.UUID, what string, body map[string]any) uuid.UUID {
		body["lifetime_seconds"] = 3600
		return w.say(t, c, clsEnded, ch, what, body)
	}
	mention := timed(w.owner, w.open, "ended mention", map[string]any{
		"content": "@carol @outsider @insider @moderator " + w.word(clsEnded, "ended mention"),
	})
	root := w.say(t, w.owner, clsOpen, w.open, "open root of an ended reply", map[string]any{})
	timed(w.insider, w.open, "ended thread reply", map[string]any{"parent_id": root})
	quoted := timed(w.carol, w.open, "ended quoted", map[string]any{})
	// A reply keeps the id of what it quoted, as for a deleted original, and
	// shows it as deleted: the id is no leak, the words would be.
	delete(w.ids, quoted.String())
	w.say(t, w.insider, clsOpen, w.open, "open quote of an ended message", map[string]any{"reply_to_id": quoted})
	wantStatus(t, "save ended", w.insider.put("/api/me/saved/"+mention.String(), map[string]string{"note": ""}), http.StatusOK)
	wantStatus(t, "remind ended", w.insider.post("/api/me/reminders", map[string]any{"message_id": quoted, "remind_at": time.Now().Add(time.Hour)}), http.StatusCreated)
	timed(w.insider, w.dm, "ended dm", map[string]any{})
	ids := []uuid.UUID{quoted}
	for id, c := range w.ids {
		if c == clsEnded {
			ids = append(ids, uuid.MustParse(id))
		}
	}
	w.a.endLifetime(ids...)
}
