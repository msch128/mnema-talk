//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAnExportListsTheThreadsTheMemberFollowsOrTurnedOff(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")
	ch := a.createChannel(admin, "general", "text")

	followed := a.send(bob, ch, "thread bob started")
	muted := a.send(bob, ch, "another thread of bob")
	a.send(bob, ch, "no thread for alice")
	for _, root := range []uuid.UUID{followed, muted} {
		if r := alice.put("/api/threads/"+root.String()+"/follow", nil); r.status != http.StatusOK {
			t.Fatalf("follow: %d %s", r.status, r.body)
		}
	}
	if r := alice.delete("/api/threads/" + muted.String() + "/follow"); r.status != http.StatusOK {
		t.Fatalf("unfollow: %d", r.status)
	}

	if r := a.requestExport(alice, memberPassword, ""); r.status != http.StatusAccepted {
		t.Fatalf("request: %d %s", r.status, r.body)
	}
	a.waitForExport(alice)
	files := unzipAll(t, alice.get("/api/users/me/export/download").body)

	var rows []struct {
		ThreadID  uuid.UUID `json:"thread_id"`
		Following bool      `json:"following"`
		Since     string    `json:"since"`
	}
	if err := json.Unmarshal([]byte(files["thread_follows.json"]), &rows); err != nil || len(rows) != 2 {
		t.Fatalf("thread_follows.json: %v %s", err, files["thread_follows.json"])
	}
	state := map[uuid.UUID]bool{}
	for _, r := range rows {
		state[r.ThreadID] = r.Following
		if r.Since == "" {
			t.Error("a row without a date")
		}
	}
	if v, ok := state[followed]; !ok || !v {
		t.Errorf("followed thread: %v", state)
	}
	if v, ok := state[muted]; !ok || v {
		t.Errorf("unfollowed thread is listed as not following: %v", state)
	}
	if strings.Contains(files["thread_follows.json"], "bob") || !strings.Contains(files["README.txt"], "thread_follows.json") {
		t.Errorf("no other member, and the README names the file: %s", files["thread_follows.json"])
	}
}
