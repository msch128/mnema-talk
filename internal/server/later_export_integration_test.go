//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTheExportAndTheEraseCoverRemindersAndScheduledMessages(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(admin, ch, "someone else's wording")
	wantStatus(t, "remind", alice.post("/api/me/reminders", map[string]any{"message_id": msg, "remind_at": inFuture(time.Hour)}), http.StatusCreated)
	wantStatus(t, "schedule", alice.post("/api/channels/"+ch.String()+"/scheduled", map[string]any{"content": "alice's own words", "send_at": inFuture(time.Hour)}), http.StatusCreated)

	wantStatus(t, "request", a.requestExport(alice, memberPassword, ""), http.StatusAccepted)
	a.waitForExport(alice)
	files := unzipAll(t, alice.get("/api/users/me/export/download").body)
	if !strings.Contains(files["reminders.json"], msg.String()) || strings.Contains(files["reminders.json"], "wording") {
		t.Errorf("reminders.json lists the reference, not the text: %s", files["reminders.json"])
	}
	if !strings.Contains(files["scheduled_messages.json"], "alice's own words") {
		t.Errorf("scheduled_messages.json misses the person's text: %s", files["scheduled_messages.json"])
	}

	wantStatus(t, "erase", a.deleteAccount(alice, memberPassword, ""), http.StatusNoContent)
	var left int
	if err := a.db.QueryRow(context.Background(), `SELECT (SELECT COUNT(*) FROM message_reminders) + (SELECT COUNT(*) FROM scheduled_messages)`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("rows left after the erase: %d, %v", left, err)
	}
}
