//go:build integration

package server

import (
	"net/http"
	"testing"
)

func TestTheDirectMessageSettingIsPersonalAndValidated(t *testing.T) {
	f := newDMFixture(t, false)
	var got struct {
		DMPolicy string `json:"dm_policy"`
	}
	f.alice.get("/api/users/me/privacy").decode(t, &got)
	if got.DMPolicy != "everyone" {
		t.Fatalf("default %q", got.DMPolicy)
	}
	wantStatus(t, "close", f.alice.put("/api/users/me/privacy", map[string]any{"dm_policy": "nobody"}), http.StatusOK)
	f.alice.get("/api/users/me/privacy").decode(t, &got)
	if got.DMPolicy != "nobody" {
		t.Fatalf("after closing %q", got.DMPolicy)
	}
	f.bob.get("/api/users/me/privacy").decode(t, &got)
	if got.DMPolicy != "everyone" {
		t.Fatalf("another member's setting changed: %q", got.DMPolicy)
	}
	wantStatus(t, "invalid", f.alice.put("/api/users/me/privacy", map[string]any{"dm_policy": "friends"}), http.StatusBadRequest)
	wantStatus(t, "unknown field", f.alice.put("/api/users/me/privacy", map[string]any{"dm_policy": "everyone", "x": 1}), http.StatusBadRequest)
	wantStatus(t, "signed out", f.a.anon().get("/api/users/me/privacy"), http.StatusUnauthorized)

	// Closing the door stops new conversations, not existing ones.
	open := f.bob.mustDM(t, f.carol)
	wantStatus(t, "carol closes", f.carol.put("/api/users/me/privacy", map[string]any{"dm_policy": "nobody"}), http.StatusOK)
	wantStatus(t, "existing goes on", f.bob.dmPost(open.ID, "still here"), http.StatusCreated)
	wantStatus(t, "new is refused", f.dave.startDM(f.carol), http.StatusForbidden)
}

func TestEveryRouteOfAConversationAnswersNotFoundToOutsiders(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	base := "/api/channels/" + dm.ID.String()
	for what, res := range map[string]response{
		"unread":        f.carol.post(base+"/unread", map[string]any{"message_id": dm.ID}),
		"first unread":  f.carol.get(base + "/first-unread"),
		"notifications": f.carol.put(base+"/notifications", map[string]any{"level": "mute"}),
		"commands":      f.carol.get(base + "/commands"),
		"interactions":  f.carol.post(base+"/interactions", map[string]any{"command_id": dm.ID}),
		"upload":        f.carol.upload(base+"/upload", "file", "a.png", "image/png", pngBytes, nil),
		"polls":         f.carol.post(base+"/polls", map[string]any{"question": "?", "options": []string{"a", "b"}}),
		"add member":    f.carol.post("/api/dms/"+dm.ID.String()+"/members", map[string]any{"user_id": f.dave.user.ID}),
		"leave":         f.carol.delete("/api/dms/" + dm.ID.String() + "/members/@me"),
		"leave for bob": f.alice.delete("/api/dms/" + dm.ID.String() + "/members/" + f.bob.user.ID.String()),
	} {
		want := http.StatusNotFound
		if what == "leave for bob" {
			want = http.StatusBadRequest // members can only remove themselves, and a pair cannot be left
		}
		if res.status != want {
			t.Errorf("%s: %d %s, want %d", what, res.status, res.body, want)
		}
	}
	wantStatus(t, "unknown id", f.alice.get("/api/dms/"+dm.ID.String()[:30]), http.StatusBadRequest)
}
