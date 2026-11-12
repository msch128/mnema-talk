//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Without Mention @all and @here, "@all" notifies nobody (doc/roles-and-permissions.md,
// MentionEveryone). Every door that stores member text must resolve mentions
// under the author's policy; the zero policy would let @all through.
func TestAtAllWithoutThePermissionNotifiesNobodyAtEveryDoor(t *testing.T) {
	f := newDMFixture(t, false)
	everyone := everyoneID(t, f.admin)
	wantStatus(t, "take mention everyone", f.admin.patch("/api/admin/roles/"+everyone.String(),
		map[string]any{"permissions": perm.DefaultEveryone &^ perm.MentionEveryone}), http.StatusOK)
	general := f.a.createChannel(f.admin, "general", "text")
	ch := "/api/channels/" + general.String()
	clean := f.a.send(f.admin, general, "a message")
	mine := f.a.send(f.alice, general, "plain words")
	forum := f.a.createChannel(f.admin, "board", "forum")
	doors := map[string]func() response{
		"message": func() response { return f.alice.post(ch+"/messages", map[string]any{"content": "@all look"}) },
		"thread reply": func() response {
			return f.alice.post(ch+"/messages", map[string]any{"content": "@all look", "parent_id": clean})
		},
		"edit": func() response {
			return f.alice.put(ch+"/messages/"+mine.String(), map[string]any{"content": "@all look"})
		},
		"forward comment": func() response {
			return f.alice.post("/api/messages/"+clean.String()+"/forward", map[string]any{"channel_id": general, "comment": "@all look"})
		},
		"poll": func() response {
			return f.alice.post(ch+"/polls", map[string]any{"question": "@all lunch?", "options": []string{"a", "b"}})
		},
		"upload caption": func() response {
			return f.alice.upload(ch+"/upload", "file", "a.png", "image/png", pngBytes, map[string]string{"content": "@all look"})
		},
		"forum post": func() response {
			return f.alice.post("/api/channels/"+forum.String()+"/posts", map[string]any{"title": "t", "content": "@all look"})
		},
	}
	for name, door := range doors {
		res := door()
		if res.status >= 300 {
			t.Errorf("%s: %d %s", name, res.status, res.body)
			continue
		}
		var out struct {
			Mentions []uuid.UUID `json:"mentions"`
			Message  *struct {
				Mentions []uuid.UUID `json:"mentions"`
			} `json:"message"`
		}
		res.decode(t, &out)
		if out.Message != nil {
			out.Mentions = append(out.Mentions, out.Message.Mentions...)
		}
		if len(out.Mentions) != 0 {
			t.Errorf("%s: @all without the permission notified %v", name, out.Mentions)
		}
	}
	var inbox struct{ Messages []struct{ ID uuid.UUID } }
	f.bob.get("/api/mentions").decode(t, &inbox)
	if len(inbox.Messages) != 0 {
		t.Errorf("bob's mentions inbox holds %d entries from @all without the permission", len(inbox.Messages))
	}
	// Control: the Owner's @all does notify, so the checks above can fail.
	var owner struct{ Mentions []uuid.UUID }
	f.admin.post(ch+"/messages", map[string]any{"content": "@all real"}).decode(t, &owner)
	if len(owner.Mentions) == 0 {
		t.Fatal("control: the Owner's @all notified nobody, the field read above says nothing")
	}
}
