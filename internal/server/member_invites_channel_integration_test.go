//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/onboarding"
	"github.com/msch128/mnema-talk/internal/perm"
)

func previewOf(t *testing.T, a *app, code string) onboarding.InvitePreview {
	t.Helper()
	res := a.anon().get("/api/invites/" + code)
	wantStatus(t, "preview", res, http.StatusOK)
	var p onboarding.InvitePreview
	res.decode(t, &p)
	return p
}

// An invite from a channel's menu lands the joiner in that channel, if a new
// member can see it; a channel the member may not see looks like none, a
// channel overwrite can take or give the right, and nothing but a community
// channel qualifies.
func TestMemberInviteIntoAChannel(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	maya := a.register(owner, "maya")
	open := a.createChannel(owner, "open", "text")
	talk := a.createChannel(owner, "lounge", "voice")

	inv := createMemberInvite(t, maya, map[string]any{"channel_id": open})
	if inv.ChannelID == nil || *inv.ChannelID != open {
		t.Fatalf("invite: %+v", inv)
	}
	if p := previewOf(t, a, inv.Code); !p.Valid || p.ChannelID == nil || *p.ChannelID != open {
		t.Fatalf("preview of an invite into a public channel: %+v", p)
	}
	talkInv := createMemberInvite(t, maya, map[string]any{"channel_id": talk})
	if p := previewOf(t, a, talkInv.Code); p.ChannelID == nil || *p.ChannelID != talk {
		t.Fatalf("a Talk is a place to land too: %+v", p)
	}
	plain := createMemberInvite(t, maya, map[string]any{})
	if p := previewOf(t, a, plain.Code); !p.Valid || p.ChannelID != nil {
		t.Fatalf("an invite without a channel names none: %+v", p)
	}

	// The joiner signs up with it and is a member like any other.
	reg := a.anon().post("/api/auth/register", map[string]string{"username": "joiner", "password": "member-password-123", "invite_code": inv.Code})
	wantStatus(t, "register", reg, http.StatusCreated)

	wantStatus(t, "an unknown channel", maya.post(meInvites, map[string]any{"channel_id": uuid.New()}), http.StatusNotFound)
	peer := a.register(owner, "peer")
	dm := maya.post("/api/dms", map[string]any{"user_ids": []uuid.UUID{peer.user.ID}})
	wantStatus(t, "a conversation", dm, http.StatusCreated)
	var conv struct{ ID uuid.UUID }
	dm.decode(t, &conv)
	wantStatus(t, "a conversation is no place to invite into", maya.post(meInvites, map[string]any{"channel_id": conv.ID}), http.StatusNotFound)

	// A channel only a role can see: hidden from maya, so 404; visible to her
	// through a role, the invite is made but the preview does not point a new
	// member at it.
	secret := a.createChannel(owner, "staff", "text")
	hide := overwrite("role", everyoneID(t, owner), 0, perm.ViewChannel)
	wantStatus(t, "hide", putOverwrites(owner, "channels", secret, hide), http.StatusNoContent)
	wantStatus(t, "a channel maya cannot see", maya.post(meInvites, map[string]any{"channel_id": secret}), http.StatusNotFound)
	staff := createRole(t, owner, "Staff", perm.ViewChannel)
	assignRoles(t, owner, maya, staff)
	wantStatus(t, "show to staff", putOverwrites(owner, "channels", secret, hide, overwrite("role", staff, perm.ViewChannel, 0)), http.StatusNoContent)
	hidden := createMemberInvite(t, maya, map[string]any{"channel_id": secret})
	if p := previewOf(t, a, hidden.Code); !p.Valid || p.ChannelID != nil {
		t.Fatalf("a channel new members cannot see is not named: %+v", p)
	}
}

func TestChannelOverwritesDecideWhoInvitesFromAChannel(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	maya := a.register(owner, "maya")
	quiet := a.createChannel(owner, "quiet", "text")
	loud := a.createChannel(owner, "loud", "text")

	// Denied in one channel: no invite from there, one without a channel still works.
	wantStatus(t, "deny", putOverwrites(owner, "channels", quiet, overwrite("role", everyoneID(t, owner), 0, perm.CreateMemberInvites)), http.StatusNoContent)
	wantStatus(t, "denied in the channel", maya.post(meInvites, map[string]any{"channel_id": quiet}), http.StatusForbidden)
	createMemberInvite(t, maya, map[string]any{"channel_id": loud})
	createMemberInvite(t, maya, map[string]any{})

	// Withdrawn everywhere but allowed in one channel.
	setEveryone(t, owner, perm.DefaultEveryone&^perm.CreateMemberInvites)
	wantStatus(t, "withdrawn", maya.post(meInvites, map[string]any{"channel_id": loud}), http.StatusForbidden)
	wantStatus(t, "allow", putOverwrites(owner, "channels", loud, overwrite("role", everyoneID(t, owner), perm.CreateMemberInvites, 0)), http.StatusNoContent)
	createMemberInvite(t, maya, map[string]any{"channel_id": loud})
	wantStatus(t, "and nowhere else", maya.post(meInvites, map[string]any{}), http.StatusForbidden)
}

func TestMemberInvitesFollowTheLockdownAndTimeouts(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	maya := a.register(owner, "maya")
	createMemberInvite(t, maya, map[string]any{})

	wantStatus(t, "lock", owner.put("/api/admin/raid/lockdown", map[string]any{"active": true, "pause_invites": true, "hours": 1}), http.StatusOK)
	res := maya.post(meInvites, map[string]any{})
	wantStatus(t, "invites paused", res, http.StatusConflict)
	wantStatus(t, "unlock", owner.put("/api/admin/raid/lockdown", map[string]any{"active": false}), http.StatusOK)
	createMemberInvite(t, maya, map[string]any{})

	wantStatus(t, "timeout", owner.post("/api/admin/users/"+maya.user.ID.String()+"/timeout", map[string]any{"duration_seconds": 600, "reason": "cool off"}), http.StatusOK)
	wantStatus(t, "a member in a timeout does not recruit", maya.post(meInvites, map[string]any{}), http.StatusForbidden)
	// What she made can still be revoked.
	var list auth.MemberInviteList = ownInvites(t, maya)
	wantStatus(t, "revoke", maya.delete(meInvites+"/"+list.Invites[0].ID.String()), http.StatusNoContent)
}

func TestAnInviteStopsWorkingWhileItsCreatorIsDisabled(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	maya := a.register(owner, "maya")
	inv := createMemberInvite(t, maya, map[string]any{})
	register := func(name string) int {
		return a.anon().post("/api/auth/register", map[string]string{"username": name, "password": "member-password-123", "invite_code": inv.Code}).status
	}

	wantStatus(t, "disable", owner.post("/api/admin/users/"+maya.user.ID.String()+"/disable", nil), http.StatusNoContent)
	if p := previewOf(t, a, inv.Code); p.Valid {
		t.Fatalf("the invite of a disabled member previews as valid: %+v", p)
	}
	if got := register("early"); got != http.StatusBadRequest {
		t.Fatalf("sign-up with the invite of a disabled member: %d, want 400", got)
	}
	wantStatus(t, "enable", owner.post("/api/admin/users/"+maya.user.ID.String()+"/enable", nil), http.StatusNoContent)
	if got := register("late"); got != http.StatusCreated {
		t.Fatalf("sign-up once the member is back: %d", got)
	}
}
