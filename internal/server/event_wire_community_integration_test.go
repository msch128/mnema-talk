//go:build integration

package server

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestWireFormatOfChannelAndRoleEvents(t *testing.T) {
	w := newWireWorld(t, "event_wire_channels_roles")
	catRes := w.admin.post("/api/admin/categories", map[string]any{"name": "Area"})
	var cat struct{ ID uuid.UUID }
	catRes.decode(t, &cat)
	w.step("category created", catRes)
	chRes := w.admin.post("/api/admin/channels", map[string]any{"name": "news", "type": "text"})
	var ch struct{ ID uuid.UUID }
	chRes.decode(t, &ch)
	w.step("channel created", chRes)
	w.step("channel changed", w.admin.patch("/api/admin/channels/"+ch.ID.String(), map[string]any{"name": "newsroom"}))
	w.step("channel deleted", w.admin.delete("/api/admin/channels/"+ch.ID.String()))
	w.step("category deleted", w.admin.delete("/api/admin/categories/"+cat.ID.String()))

	roleRes := w.admin.post("/api/admin/roles", map[string]any{"name": "Crew", "permissions": perm.ViewChannel, "self_assignable": true})
	var role struct{ ID uuid.UUID }
	roleRes.decode(t, &role)
	w.step("role created", roleRes)
	w.step("role changed", w.admin.patch("/api/admin/roles/"+role.ID.String(), map[string]any{"name": "Team"}))
	w.step("channel overwrites", putOverwrites(w.admin, "channels", w.ch, overwrite("role", role.ID, perm.ViewChannel, 0)))
	w.step("member roles set", setRoles(w.admin, w.alice.user.ID, role.ID))
	w.step("member takes a role", w.bob.put("/api/me/roles/"+role.ID.String(), nil))
	w.step("member drops a role", w.bob.delete("/api/me/roles/"+role.ID.String()))
	w.step("role deleted", w.admin.delete("/api/admin/roles/"+role.ID.String()))
}

func TestWireFormatOfCommunityEvents(t *testing.T) {
	w := newWireWorld(t, "event_wire_community")
	w.step("custom emoji added", uploadEmoji(w.admin, "party", "party.png", "image/png", []byte(bannerPNG(t, 32, 32))))
	w.step("community renamed", w.admin.put("/api/admin/onboarding", map[string]any{"community_name": "Harbour", "welcome_text": "", "rules_text": ""}))
	w.step("member blocked", w.alice.put("/api/me/blocks/"+w.bob.user.ID.String(), nil))
	w.step("member unblocked", w.alice.delete("/api/me/blocks/"+w.bob.user.ID.String()))
	bad := w.say(w.bob, w.ch, map[string]any{"content": "buy now"})
	w.step("message reported", w.alice.post("/api/messages/"+bad.String()+"/report", map[string]any{"category": "spam"}))

	w.orderMembers()
	dmRes := w.alice.startDM(w.bob)
	var dm struct{ ID uuid.UUID }
	dmRes.decode(t, &dm)
	w.step("conversation started", dmRes)
	w.step("conversation hidden", w.alice.put("/api/dms/"+dm.ID.String()+"/hidden", map[string]any{"hidden": true}))
	w.step("conversation settings", w.alice.put("/api/users/me/dm-settings", map[string]any{"mute_all": true, "list_collapsed": true}))
	w.stepOf("Talk started in a conversation", 3, w.alice.post("/api/dms/"+dm.ID.String()+"/talk", nil))
	w.calendarSteps()
}

func (w *wireWorld) calendarSteps() {
	w.t.Helper()
	res := w.admin.post("/api/events", map[string]any{"title": "Game night", "description": "bring snacks", "start_time": time.Now().Add(48 * time.Hour), "location": "Hall"})
	var ev struct{ ID uuid.UUID }
	res.decode(w.t, &ev)
	w.step("event created", res)
	base := "/api/events/" + ev.ID.String()
	w.step("event changed", w.admin.patch(base, map[string]any{"title": "Board game night"}))
	w.step("event RSVP", w.alice.put(base+"/rsvp", map[string]string{"response": "going"}))
	commentRes := w.bob.post(base+"/comments", map[string]string{"content": "I bring snacks"})
	var comment struct{ ID uuid.UUID }
	commentRes.decode(w.t, &comment)
	w.stepOf("event comment", 2, commentRes)
	w.step("event comment edited", w.bob.patch(base+"/comments/"+comment.ID.String(), map[string]string{"content": "I bring drinks"}))
	w.step("event comment deleted", w.bob.delete(base+"/comments/"+comment.ID.String()))
	w.step("event cancelled", w.admin.post(base+"/cancel", nil))
}

// orderMembers makes alice the member with the smaller ID. A conversation
// lists its members by ID, and IDs are random.
func (w *wireWorld) orderMembers() {
	if bytes.Compare(w.alice.user.ID[:], w.bob.user.ID[:]) > 0 {
		w.alice, w.bob = w.bob, w.alice
	}
	w.a.runSQL(`UPDATE users SET username = 'first', display_name = 'first' WHERE id = $1`, w.alice.user.ID)
	w.a.runSQL(`UPDATE users SET username = 'second', display_name = 'second' WHERE id = $1`, w.bob.user.ID)
}
