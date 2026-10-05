//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type profile struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	StatusText  string    `json:"status_text"`
	Presence    string    `json:"presence"`
}

func TestStatusTextOnlyBySelfOrAdmin(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	moritz := a.register(admin, "moritz")

	res := max.put("/api/users/me/status", map[string]string{"status_text": "  zockt grad  "})
	if res.status != http.StatusOK {
		t.Fatalf("own status: %d %s", res.status, res.body)
	}
	var p profile
	res.decode(t, &p)
	if p.StatusText != "zockt grad" {
		t.Fatalf("status %q, want trimmed text", p.StatusText)
	}
	if res := max.put("/api/users/me/status", map[string]string{"status_text": strings.Repeat("x", 33)}); res.status != http.StatusBadRequest {
		t.Fatalf("33 characters accepted: %d", res.status)
	}

	// Members cannot touch someone else's status; admins can, for anyone.
	other := "/api/admin/users/" + max.user.ID.String() + "/status"
	if res := moritz.put(other, map[string]string{"status_text": "doof"}); res.status != http.StatusForbidden {
		t.Fatalf("member edited another status: %d", res.status)
	}
	res = admin.put(other, map[string]string{"status_text": ""})
	if res.status != http.StatusOK {
		t.Fatalf("admin cleared status: %d %s", res.status, res.body)
	}
	var cleared profile
	res.decode(t, &cleared)
	if cleared.StatusText != "" || cleared.Presence != "" {
		t.Fatalf("after admin clear: %+v (presence must stay private)", cleared)
	}
	var members []profile
	admin.get("/api/members").decode(t, &members)
	for _, m := range members {
		if m.Username == "max" && m.StatusText != "" {
			t.Fatalf("members still show %q", m.StatusText)
		}
	}
}

func TestPresenceChoiceAndNames(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")

	if res := max.put("/api/users/me/presence", map[string]string{"presence": "offline"}); res.status != http.StatusBadRequest {
		t.Fatalf("offline is not choosable: %d", res.status)
	}
	if res := max.put("/api/users/me/presence", map[string]string{"presence": "focus"}); res.status != http.StatusOK {
		t.Fatalf("set focus: %d %s", res.status, res.body)
	}
	var me profile
	max.get("/api/auth/me").decode(t, &me)
	if me.Presence != "focus" {
		t.Fatalf("presence %q, want focus", me.Presence)
	}

	if res := max.put("/api/users/me/profile", map[string]string{"display_name": strings.Repeat("ä", 25)}); res.status != http.StatusBadRequest {
		t.Fatalf("25 character display name accepted: %d", res.status)
	}
	if res := max.put("/api/users/me/profile", map[string]string{"display_name": "Max Mustermann"}); res.status != http.StatusOK {
		t.Fatalf("display name: %d %s", res.status, res.body)
	}

	for _, reserved := range []string{"all", "HERE"} {
		inv := admin.post("/api/admin/invites", map[string]any{})
		var code struct{ Code string }
		inv.decode(t, &code)
		res := a.anon().post("/api/auth/register", map[string]string{
			"username": reserved, "password": "member-password-123", "invite_code": code.Code,
		})
		if res.status != http.StatusBadRequest {
			t.Fatalf("reserved username %q registered: %d", reserved, res.status)
		}
	}
}

func TestMentionsResolveUsersAllAndHere(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	moritz := a.register(admin, "moritz")
	ch := a.createChannel(admin, "allgemein", "text")

	// Only max is connected, so only max is "here".
	ws, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ws.expect("presence_snapshot", 2*time.Second); !ok {
		t.Fatal("no presence snapshot")
	}

	mentions := func(content string) []uuid.UUID {
		t.Helper()
		res := admin.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": content})
		if res.status != http.StatusCreated {
			t.Fatalf("post: %d %s", res.status, res.body)
		}
		var m struct{ Mentions []uuid.UUID }
		res.decode(t, &m)
		return m.Mentions
	}
	if got := mentions("hey @MAX."); len(got) != 1 || got[0] != max.user.ID {
		t.Fatalf("@MAX. → %v", got)
	}
	if got := mentions("an @here"); len(got) != 1 || got[0] != max.user.ID {
		t.Fatalf("@here → %v, want only the connected max", got)
	}
	got := mentions("@all bitte lesen")
	if len(got) != 2 || !slices.Contains(got, max.user.ID) || !slices.Contains(got, moritz.user.ID) {
		t.Fatalf("@all → %v, want max and moritz (never the author)", got)
	}
	if got := mentions("mail@max.de and @nobody"); len(got) != 0 {
		t.Fatalf("no mention expected, got %v", got)
	}

	if s := moritz.readState(t, ch); s.MentionCount != 1 {
		t.Fatalf("moritz mentions=%d, want 1 (@all)", s.MentionCount)
	}
	if s := max.readState(t, ch); s.MentionCount != 3 {
		t.Fatalf("max mentions=%d, want 3", s.MentionCount)
	}

	// Editing re-resolves: the mention goes away with the text.
	id := a.send(admin, ch, "@moritz komm mal")
	res := admin.put("/api/channels/"+ch.String()+"/messages/"+id.String(), map[string]string{"content": "schon gut"})
	if res.status != http.StatusOK {
		t.Fatalf("edit: %d %s", res.status, res.body)
	}
	var edited struct{ Mentions []uuid.UUID }
	res.decode(t, &edited)
	if len(edited.Mentions) != 0 {
		t.Fatalf("edited mentions %v", edited.Mentions)
	}
	if s := moritz.readState(t, ch); s.MentionCount != 1 {
		t.Fatalf("after edit moritz mentions=%d, want 1", s.MentionCount)
	}
}

func TestPresenceOverWebSocket(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()
	max := a.register(admin, "max")

	watcher, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	watcher.expect("presence_snapshot", 2*time.Second)

	if res := max.put("/api/users/me/presence", map[string]string{"presence": "dnd"}); res.status != http.StatusOK {
		t.Fatalf("set dnd: %d", res.status)
	}
	conn, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	waitStatus := func(want string) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case <-deadline:
				t.Fatalf("no presence_update %q", want)
			default:
			}
			raw, ok := watcher.expect("presence_update", 3*time.Second)
			if !ok {
				t.Fatalf("no presence_update %q", want)
			}
			var p struct {
				UserID uuid.UUID `json:"user_id"`
				Status string    `json:"status"`
			}
			_ = json.Unmarshal(raw, &p)
			if p.UserID == max.user.ID && p.Status == want {
				return
			}
		}
	}
	waitStatus("dnd")

	if res := max.put("/api/users/me/presence", map[string]string{"presence": "online"}); res.status != http.StatusOK {
		t.Fatalf("set online: %d", res.status)
	}
	waitStatus("online")
	conn.send("presence_idle", map[string]bool{"idle": true})
	waitStatus("away")
	conn.send("presence_idle", map[string]bool{"idle": false})
	waitStatus("online")
}
