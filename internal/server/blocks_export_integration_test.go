//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestTheExportListsOwnBlocksAndReportsWithoutNamesOrReportedText(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice, bob := a.register(admin, "alice"), a.register(admin, "bobby")
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(bob, ch, "bobby's reported wording")
	wantStatus(t, "block", alice.put("/api/me/blocks/"+bob.user.ID.String(), nil), http.StatusNoContent)
	wantStatus(t, "report", alice.post("/api/messages/"+msg.String()+"/report", map[string]any{"category": "spam", "note": "alice's note"}), http.StatusCreated)

	wantStatus(t, "request", a.requestExport(alice, memberPassword, ""), http.StatusAccepted)
	a.waitForExport(alice)
	files := unzipAll(t, alice.get("/api/users/me/export/download").body)

	if !strings.Contains(files["blocks.json"], bob.user.ID.String()) || strings.Contains(files["blocks.json"], "bobby") {
		t.Errorf("blocks.json lists the id and no name: %s", files["blocks.json"])
	}
	if !strings.Contains(files["reports.json"], msg.String()) || !strings.Contains(files["reports.json"], "alice's note") ||
		strings.Contains(files["reports.json"], "wording") || strings.Contains(files["reports.json"], "bobby") {
		t.Errorf("reports.json holds the reporter's words only: %s", files["reports.json"])
	}
	// The blocked member's export says nothing about being blocked.
	wantStatus(t, "bob request", a.requestExport(bob, memberPassword, ""), http.StatusAccepted)
	a.waitForExport(bob)
	theirs := unzipAll(t, bob.get("/api/users/me/export/download").body)
	if strings.TrimSpace(theirs["blocks.json"]) != "[]" || strings.TrimSpace(theirs["reports.json"]) != "[]" {
		t.Errorf("the blocked and reported member's export: blocks %s reports %s", theirs["blocks.json"], theirs["reports.json"])
	}
}
