//go:build integration

package server

import (
	"net/http"
	"testing"
)

type firstRunView struct {
	Steps []struct {
		ID       string `json:"id"`
		Done     bool   `json:"done"`
		Optional bool   `json:"optional"`
	} `json:"steps"`
	Hidden   bool `json:"hidden"`
	Complete bool `json:"complete"`
}

func (v firstRunView) done(id string) bool {
	for _, s := range v.Steps {
		if s.ID == id {
			return s.Done
		}
	}
	return false
}

func firstRun(t *testing.T, c *client) firstRunView {
	t.Helper()
	res := c.get("/api/admin/first-run")
	wantStatus(t, "get first-run", res, http.StatusOK)
	var v firstRunView
	res.decode(t, &v)
	return v
}

func TestFirstRunChecklistTicksFromRealStateAndCanBeClosedAndReopened(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()

	v := firstRun(t, owner)
	if len(v.Steps) != 6 || v.Hidden || v.Complete {
		t.Fatalf("fresh community: %+v", v)
	}
	for _, s := range v.Steps {
		if s.Done {
			t.Fatalf("step %s ticked on a fresh community", s.ID)
		}
		if s.Optional != (s.ID == "roles_rules") {
			t.Fatalf("step %s optional=%v", s.ID, s.Optional)
		}
	}

	wantStatus(t, "name", owner.put("/api/admin/onboarding", map[string]any{"community_name": "Lantern Fox", "welcome_text": "", "rules_text": ""}), http.StatusOK)
	wantStatus(t, "invite", owner.post("/api/admin/invites", map[string]any{}), http.StatusCreated)
	lobby := a.createChannel(owner, "Calls", "voice")
	if _, err := a.db.Exec(t.Context(), `UPDATE channels SET is_lobby = TRUE WHERE id = $1`, lobby); err != nil {
		t.Fatal(err)
	}
	v = firstRun(t, owner)
	if !v.done("name") || !v.done("invite") || !v.done("voice") || v.done("channels") || v.done("status") || v.Complete {
		t.Fatalf("after name, invite and lobby: %+v", v)
	}

	res := owner.put("/api/admin/first-run", map[string]any{"visited": []string{"channels", "status", "channels"}})
	wantStatus(t, "visited", res, http.StatusOK)
	res.decode(t, &v)
	if !v.done("channels") || !v.done("status") || !v.Complete || v.done("roles_rules") {
		t.Fatalf("after visiting channels and status: %+v", v)
	}

	res = owner.put("/api/admin/first-run", map[string]any{"hidden": true})
	wantStatus(t, "hide", res, http.StatusOK)
	res.decode(t, &v)
	if !v.Hidden || !firstRun(t, owner).Hidden {
		t.Fatalf("hide did not stick: %+v", v)
	}
	// Other stored first-run progress survives the toggle, and so does the tips record.
	wantStatus(t, "tip", owner.put("/api/onboarding/tips", map[string]any{"dismissed_tips": []string{"search"}}), http.StatusOK)
	res = owner.put("/api/admin/first-run", map[string]any{"hidden": false})
	wantStatus(t, "reopen", res, http.StatusOK)
	res.decode(t, &v)
	if v.Hidden || !v.done("channels") {
		t.Fatalf("reopen: %+v", v)
	}
	st := onboardingState(t, owner)
	if tips, _ := st["dismissed_tips"].([]any); len(tips) != 1 {
		t.Fatalf("the tips record was disturbed: %v", st["dismissed_tips"])
	}
}

func TestFirstRunChecklistIsOwnerOnlyAndRejectsUnknownSteps(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	member := a.register(owner, "alice")

	wantStatus(t, "anon get", a.anon().get("/api/admin/first-run"), http.StatusUnauthorized)
	wantStatus(t, "member get", member.get("/api/admin/first-run"), http.StatusForbidden)
	wantStatus(t, "member put", member.put("/api/admin/first-run", map[string]any{"hidden": true}), http.StatusForbidden)
	wantStatus(t, "unknown step", owner.put("/api/admin/first-run", map[string]any{"visited": []string{"name"}}), http.StatusBadRequest)
	wantStatus(t, "unknown field", owner.put("/api/admin/first-run", map[string]any{"done": []string{"name"}}), http.StatusBadRequest)
}

func TestFirstRunChecklistStaysOutOfAnEstablishedCommunityButNotOutOfANewOne(t *testing.T) {
	// A community that already had members when the Owner first looks starts closed ...
	old := newApp(t, false)
	oldOwner := old.seedAdmin()
	old.register(oldOwner, "alice")
	if v := firstRun(t, oldOwner); !v.Hidden || !v.done("invite") {
		t.Fatalf("established community: %+v", v)
	}
	wantStatus(t, "reopen", oldOwner.put("/api/admin/first-run", map[string]any{"hidden": false}), http.StatusOK)
	if v := firstRun(t, oldOwner); v.Hidden {
		t.Fatalf("an established community could not reopen the checklist: %+v", v)
	}

	// ... while the first member joining a new community does not close what the Owner already sees.
	fresh := newApp(t, false)
	owner := fresh.seedAdmin()
	if v := firstRun(t, owner); v.Hidden {
		t.Fatalf("new community: %+v", v)
	}
	fresh.register(owner, "bob")
	if v := firstRun(t, owner); v.Hidden || !v.done("invite") {
		t.Fatalf("after the first member joined: %+v", v)
	}
}
