//go:build integration

package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/msch128/mnema-talk/internal/events"
)

func publicNames(t *testing.T, a *app) (health, options string) {
	t.Helper()
	anon := a.anon()
	var h struct {
		CommunityName string `json:"community_name"`
	}
	res := anon.get("/api/health")
	wantStatus(t, "health", res, http.StatusOK)
	res.decode(t, &h)
	var o struct {
		CommunityName string `json:"community_name"`
	}
	res = anon.get("/api/auth/options")
	wantStatus(t, "options", res, http.StatusOK)
	res.decode(t, &o)
	return h.CommunityName, o.CommunityName
}

func TestCommunityNameIsPublicFollowsTheSettingAndFallsBackToTheHost(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	u, err := url.Parse(a.origin())
	if err != nil {
		t.Fatal(err)
	}

	if h, o := publicNames(t, a); h != u.Hostname() || o != u.Hostname() {
		t.Fatalf("unset name: health %q, options %q, want the host %q", h, o, u.Hostname())
	}

	res := admin.put("/api/admin/onboarding", map[string]any{"community_name": "Lantern Fox", "welcome_text": "", "rules_text": ""})
	wantStatus(t, "rename", res, http.StatusOK)
	if h, o := publicNames(t, a); h != "Lantern Fox" || o != "Lantern Fox" {
		t.Fatalf("after the rename: health %q, options %q", h, o)
	}
	var told bool
	for _, e := range a.events.Snapshot() {
		if p, ok := e.Payload.(map[string]string); ok && e.Type == events.CommunityUpdated && p["community_name"] == "Lantern Fox" && e.Recipients == nil {
			told = true
		}
	}
	if !told {
		t.Fatal("no community_updated broadcast after the rename")
	}

	wantStatus(t, "clear", admin.put("/api/admin/onboarding", map[string]any{"community_name": "", "welcome_text": "", "rules_text": ""}), http.StatusOK)
	if h, _ := publicNames(t, a); h != u.Hostname() {
		t.Fatalf("cleared name: health %q, want the host", h)
	}
}
