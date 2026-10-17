//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A body labelled text/plain, or one that does not fit the capture limit,
// still leaves the diff: it is computed from the state before and after.
func TestAuditKeepsTheDiffWhateverTheContentType(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	role := createRole(t, admin, "Plain", perm.SendMessages)

	res := admin.do(http.MethodPatch, "/api/admin/roles/"+role.String(), strings.NewReader(`{"name":"Renamed"}`), map[string]string{"Content-Type": "text/plain"})
	if res.status != http.StatusOK {
		t.Fatalf("text/plain patch refused (%d)", res.status)
	}
	big := `{"name":"Huge","color":1,"pad":"` + strings.Repeat("x", 100<<10) + `"}`
	if r := admin.do(http.MethodPatch, "/api/admin/roles/"+role.String(), strings.NewReader(big), map[string]string{"Content-Type": "application/octet-stream"}); r.status != http.StatusOK && r.status != http.StatusBadRequest {
		t.Fatalf("big: %d", r.status)
	}
	for _, e := range auditEntries(t, admin, "action=role.update").Entries {
		if ch, _ := e.Details["changes"].(map[string]any); ch == nil {
			t.Errorf("entry %d lost its diff: %v", e.ID, e.Details)
		}
	}
	first := auditEntries(t, admin, "action=role.update&limit=200")
	if len(first.Entries) == 0 {
		t.Fatal("no entry for the mislabelled request")
	}
}

func TestAuditHidesSensitiveEntriesFromPlainAuditors(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	out := admin.post("/api/admin/channels/"+ch.String()+"/outgoing-webhooks", map[string]any{
		"name": "out", "url": "https://hooks.example.com/services/T000/B000/TOKENINPATH", "events": []string{"MESSAGE_CREATE"},
	})
	if out.status != http.StatusCreated {
		t.Fatalf("outgoing webhook not creatable here: %d %s", out.status, out.body)
	}
	if res := admin.put("/api/admin/upload-limit", map[string]any{"max_upload_mb": 5}); res.status != http.StatusOK && res.status != http.StatusNoContent {
		t.Fatalf("upload limit: %d %s", res.status, res.body)
	}
	createRole(t, admin, "visible", 0)

	auditor := a.register(admin, "carol")
	assignRoles(t, admin, auditor, createRole(t, admin, "auditor", perm.ViewAuditLog))

	owner := auditEntries(t, admin, "limit=200")
	seen := map[string]bool{}
	var all strings.Builder
	for _, e := range owner.Entries {
		seen[e.Action] = true
		all.WriteString(string(mustJSON(e)))
	}
	if !seen["outgoing_webhook.create"] || !seen["settings.upload_limit"] {
		t.Fatalf("owner misses sensitive entries: %v", seen)
	}
	if strings.Contains(all.String(), "TOKENINPATH") || !strings.Contains(all.String(), "hooks.example.com") {
		t.Errorf("webhook address not reduced to its host: %s", all.String())
	}

	for _, e := range auditEntries(t, auditor, "limit=200").Entries {
		for _, p := range audit.Sensitive {
			if strings.HasSuffix(p, "%") && strings.HasPrefix(e.Action, strings.TrimSuffix(p, "%")) {
				t.Errorf("auditor sees %s", e.Action)
			}
		}
	}
	if got := auditEntries(t, auditor, "action=role"); len(got.Entries) == 0 {
		t.Error("auditor sees no role entries")
	}

	// An Administrator is privileged.
	adminRole := createRole(t, admin, "adm", perm.Administrator)
	assignRoles(t, admin, auditor, adminRole)
	if got := auditEntries(t, auditor, "action=outgoing_webhook"); len(got.Entries) == 0 {
		t.Error("an Administrator must read webhook entries")
	}
}
