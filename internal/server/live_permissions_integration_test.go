//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

const (
	soon = 2 * time.Second
	none = 400 * time.Millisecond
)

func mustDial(t *testing.T, c *client) *wsConn {
	t.Helper()
	w, _, err := c.dialWS(c.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	// The connect snapshot arrives first; it is part of what is tested elsewhere.
	if _, ok := w.expect("voice_rooms", soon); !ok {
		t.Fatal("no connect snapshot")
	}
	return w
}

// typesUntil lists the event types received up to and including final.
func (w *wsConn) typesUntil(final string, within time.Duration) []string {
	var seen []string
	timeout := time.After(within)
	for {
		select {
		case ev, ok := <-w.events:
			if !ok {
				return seen
			}
			seen = append(seen, ev.Type)
			if ev.Type == final {
				return seen
			}
		case <-timeout:
			return seen
		}
	}
}

func TestChannelEventsReachOnlyTheirViewers(t *testing.T) {
	f := newAccessFixture(t, true)
	secret := f.privateChannel(t, "staff-only", "text")
	open := f.a.createChannel(f.owner, "open", "text")
	outWS, inWS, ownerWS := mustDial(t, f.outsider), mustDial(t, f.insider), mustDial(t, f.owner)

	msg := postMessage(t, f.owner, secret, map[string]any{"content": "classified"})
	for _, w := range []*wsConn{inWS, ownerWS} {
		if p, ok := w.expect("message_create", soon); !ok || !strings.Contains(string(p), "classified") {
			t.Fatalf("viewer missed the message: %s", p)
		}
	}
	if p, ok := outWS.expect("message_create", none); ok {
		t.Fatalf("outsider received a private message: %s", p)
	}

	wantStatus(t, "edit", f.owner.put("/api/channels/"+secret.String()+"/messages/"+msg.String(), map[string]string{"content": "classified 2"}), http.StatusOK)
	wantStatus(t, "react", f.insider.post("/api/messages/"+msg.String()+"/reactions", map[string]string{"emoji": "👍"}), http.StatusOK)
	p, ok := inWS.expect("message_reaction", soon)
	var reaction struct {
		ChannelID uuid.UUID `json:"channel_id"`
	}
	_ = json.Unmarshal(p, &reaction)
	if !ok || reaction.ChannelID != secret {
		t.Fatalf("reaction event: %s", p)
	}
	wantStatus(t, "delete", f.owner.delete("/api/channels/"+secret.String()+"/messages/"+msg.String()), http.StatusNoContent)
	for _, typ := range []string{"message_update", "message_reaction", "message_delete"} {
		if p, ok := outWS.expect(typ, 100*time.Millisecond); ok {
			t.Fatalf("outsider received %s of a private channel: %s", typ, p)
		}
	}

	// Typing: shown to viewers of the channel only, and only from those who may write.
	inWS.send("typing", map[string]any{"channel_id": secret})
	if _, ok := ownerWS.expect("typing", soon); !ok {
		t.Fatal("the owner should see the typing insider")
	}
	if _, ok := outWS.expect("typing", none); ok {
		t.Fatal("typing leaked")
	}
	outWS.send("typing", map[string]any{"channel_id": secret})
	if _, ok := inWS.expect("typing", none); ok {
		t.Fatal("an outsider cannot show as typing in a channel they cannot see")
	}
	// Open channels stay open.
	postMessage(t, f.owner, open, map[string]any{"content": "hello all"})
	if _, ok := outWS.expect("message_create", soon); !ok {
		t.Fatal("open channel message missing")
	}
	wantStatus(t, "lock sending", putOverwrites(f.owner, "channels", open, overwrite("role", f.everyone, 0, perm.SendMessages)), http.StatusNoContent)
	if _, ok := outWS.expect("permissions_changed", soon); !ok {
		t.Fatal("permissions_changed missing")
	}
	outWS.send("typing", map[string]any{"channel_id": open})
	if _, ok := ownerWS.expect("typing", none); ok {
		t.Fatal("someone who cannot send shows as typing")
	}
}

func TestPermissionChangesApplyLive(t *testing.T) {
	f := newAccessFixture(t, true)
	secret := f.privateChannel(t, "staff-only", "text")
	outWS := mustDial(t, f.outsider)

	// Granting the role: the outsider is told, then sees the channel and its events.
	assignRoles(t, f.owner, f.outsider, f.staff)
	if _, ok := outWS.expect("permissions_changed", soon); !ok {
		t.Fatal("the member was not told about the new role")
	}
	if !visibleChannelIDs(t, f.outsider)[secret] || !channelPerms(myPerms(t, f.outsider), secret).Has(perm.ViewChannel) {
		t.Fatal("the channel should have appeared")
	}
	postMessage(t, f.owner, secret, map[string]any{"content": "welcome"})
	if _, ok := outWS.expect("message_create", soon); !ok {
		t.Fatal("no events after joining the channel")
	}

	// Taking it away again: gone at once, events stop.
	assignRoles(t, f.owner, f.outsider)
	if _, ok := outWS.expect("permissions_changed", soon); !ok {
		t.Fatal("the member was not told about losing the role")
	}
	if visibleChannelIDs(t, f.outsider)[secret] {
		t.Fatal("the channel should be gone")
	}
	wantStatus(t, "history", f.outsider.get("/api/channels/"+secret.String()+"/messages"), http.StatusNotFound)
	postMessage(t, f.owner, secret, map[string]any{"content": "secret again"})
	if p, ok := outWS.expect("message_create", none); ok {
		t.Fatalf("events kept flowing after the permission was removed: %s", p)
	}

	// Editing a role live also reaches its holders (and nobody else).
	assignRoles(t, f.owner, f.outsider, f.staff)
	outWS.expect("permissions_changed", soon)
	other := f.a.register(f.owner, "bystander")
	otherWS := mustDial(t, other)
	wantStatus(t, "edit role", f.owner.patch("/api/admin/roles/"+f.staff.String(), map[string]any{"name": "Crew", "color": 255}), http.StatusOK)
	if _, ok := outWS.expect("permissions_changed", soon); !ok {
		t.Fatal("holders are told when their role changes")
	}
	seen := otherWS.typesUntil("roles_changed", soon)
	if len(seen) == 0 || seen[len(seen)-1] != "roles_changed" {
		t.Fatalf("everybody learns that roles changed: %v", seen)
	}
	for _, typ := range seen {
		if typ == "permissions_changed" {
			t.Fatal("only the role's holders are told to refresh their permissions")
		}
	}
}

func TestTalkAccessIsEnforcedAndRevokedLive(t *testing.T) {
	f := newAccessFixture(t, true)
	lounge := f.a.createChannel(f.owner, "Lounge", "voice")
	staffTalk := f.privateChannel(t, "Staff Talk", "voice")
	ownerWS := mustDial(t, f.owner)
	inWS := mustDial(t, f.insider)
	outWS := mustDial(t, f.outsider)

	// A private Talk: outsiders cannot join, do not see who is in it.
	inWS.send("voice_join", map[string]any{"channel_id": staffTalk})
	if _, ok := ownerWS.expect("voice_state_update", soon); !ok {
		t.Fatal("the owner sees the insider join")
	}
	if p, ok := outWS.expect("voice_state_update", none); ok {
		t.Fatalf("a private Talk's presence leaked: %s", p)
	}
	outWS.send("voice_join", map[string]any{"channel_id": staffTalk})
	p, ok := outWS.expect("voice_kicked", soon)
	if !ok || !strings.Contains(string(p), "missing_permission") {
		t.Fatalf("joining a private Talk: %s", p)
	}
	if _, ok := ownerWS.expect("voice_state_update", none); ok {
		t.Fatal("the refused join must not show up")
	}
	// A later connection does not see the private room in its snapshot either.
	late := f.a.register(f.owner, "late")
	lateConn, _, err := late.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := lateConn.expect("voice_snapshot", soon); !ok || strings.Contains(string(p), f.insider.user.ID.String()) {
		t.Fatalf("snapshot leaks the private Talk: %s", p)
	}
	ownerFresh, _, _ := f.owner.dialWS(f.a.origin())
	if p, ok := ownerFresh.expect("voice_snapshot", soon); !ok || !strings.Contains(string(p), f.insider.user.ID.String()) {
		t.Fatalf("the owner sees every room: %s", p)
	}

	// Connect is its own permission: @everyone loses it for the Lounge.
	outWS.send("voice_join", map[string]any{"channel_id": lounge})
	if _, ok := ownerWS.expect("voice_state_update", soon); !ok {
		t.Fatal("the lounge join is announced")
	}
	wantStatus(t, "deny connect", putOverwrites(f.owner, "channels", lounge, overwrite("role", f.everyone, 0, perm.Connect)), http.StatusNoContent)
	if p, ok := outWS.expect("voice_kicked", soon); !ok || !strings.Contains(string(p), "missing_permission") {
		t.Fatalf("a member who may no longer connect must be removed: %s", p)
	}
	if p, ok := ownerWS.expect("voice_state_update", soon); !ok || !strings.Contains(string(p), `"leave"`) {
		t.Fatalf("the leave must be announced: %s", p)
	}
	outWS.send("voice_join", map[string]any{"channel_id": lounge})
	if _, ok := outWS.expect("voice_kicked", soon); !ok {
		t.Fatal("rejoining without Connect must fail")
	}

	// Removing the role that grants access to a private Talk removes the member from it.
	assignRoles(t, f.owner, f.insider)
	if p, ok := inWS.expect("voice_kicked", soon); !ok || !strings.Contains(string(p), staffTalk.String()) {
		t.Fatalf("losing the role must end the call: %s", p)
	}
}

func TestVoiceKickReasonIsOptional(t *testing.T) {
	f := newAccessFixture(t, true)
	lounge := f.a.createChannel(f.owner, "Lounge", "voice")
	ownerWS := mustDial(t, f.owner)
	outWS := mustDial(t, f.outsider)
	outWS.send("voice_join", map[string]any{"channel_id": lounge})
	if _, ok := ownerWS.expect("voice_state_update", soon); !ok {
		t.Fatal("join not announced")
	}
	mod := f.a.register(f.owner, "talkmod")
	assignRoles(t, f.owner, mod, createRole(t, f.owner, "Talk mod", perm.DisconnectMembers))
	wantStatus(t, "kick", mod.post("/api/admin/users/"+f.outsider.user.ID.String()+"/kick", nil), http.StatusNoContent)
	p, ok := outWS.expect("voice_kicked", soon)
	if !ok || strings.Contains(string(p), "reason") {
		t.Fatalf("a moderator's kick carries no reason: %s", p)
	}
}
