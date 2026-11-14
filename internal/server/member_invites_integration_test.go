//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/perm"
)

const meInvites = "/api/me/invites"

func createMemberInvite(t *testing.T, c *client, body map[string]any) auth.Invite {
	t.Helper()
	res := c.post(meInvites, body)
	wantStatus(t, "create a member invite", res, http.StatusCreated)
	var inv auth.Invite
	res.decode(t, &inv)
	return inv
}

func setEveryone(t *testing.T, owner *client, p perm.Set) {
	t.Helper()
	wantStatus(t, "set @everyone", owner.patch("/api/admin/roles/"+everyoneID(t, owner).String(), map[string]any{"permissions": int64(p)}), http.StatusOK)
}

func setMemberInviteLimits(t *testing.T, owner *client, hours, uses, active, perHour int) {
	t.Helper()
	res := owner.put("/api/admin/invites/limits", map[string]any{"max_expires_in_hours": hours, "max_uses": uses, "max_active": active, "max_per_hour": perHour})
	wantStatus(t, "set the limits", res, http.StatusOK)
}

func ownInvites(t *testing.T, c *client) auth.MemberInviteList {
	t.Helper()
	res := c.get(meInvites)
	wantStatus(t, "list own invites", res, http.StatusOK)
	var list auth.MemberInviteList
	res.decode(t, &list)
	return list
}

// Who may create invites: @everyone on a fresh install, withdrawn per role,
// granted per role; Manage invites alone is the admin-area right and does not
// open the member route.
func TestMemberInvitesPermissionMatrix(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	member := a.register(owner, "maya")

	inv := createMemberInvite(t, member, map[string]any{})
	if inv.CreatedByID != member.user.ID || inv.MaxUses == nil || *inv.MaxUses != 10 || inv.ExpiresAt == nil {
		t.Fatalf("defaults: %+v", inv)
	}
	if d := time.Until(*inv.ExpiresAt); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("the default expiry is %v, want about a day", d)
	}

	setEveryone(t, owner, perm.DefaultEveryone&^perm.CreateMemberInvites)
	res := member.post(meInvites, map[string]any{})
	wantStatus(t, "withdrawn from @everyone", res, http.StatusForbidden)
	if res.code(t) != "FORBIDDEN" {
		t.Fatalf("code %q", res.code(t))
	}
	// What the member made stays theirs to see and revoke.
	if list := ownInvites(t, member); len(list.Invites) != 1 {
		t.Fatalf("own invites after the withdrawal: %+v", list.Invites)
	}

	granted := createRole(t, owner, "Hosts", perm.CreateMemberInvites)
	assignRoles(t, owner, member, granted)
	createMemberInvite(t, member, map[string]any{})

	// Manage invites without Create invites: the admin routes, not the member one.
	manager := a.register(owner, "mia")
	assignRoles(t, owner, manager, createRole(t, owner, "Invite managers", perm.CreateInvites))
	wantStatus(t, "manage invites alone", manager.post(meInvites, map[string]any{}), http.StatusForbidden)
	wantStatus(t, "the admin route", manager.post("/api/admin/invites", map[string]any{}), http.StatusCreated)

	// The Owner holds everything.
	createMemberInvite(t, owner, map[string]any{})
}

func TestNobodyCanChooseTheInvitePermissionForThemselves(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	res := owner.post("/api/admin/roles", map[string]any{"name": "open hosts", "permissions": int64(perm.CreateMemberInvites), "self_assignable": true})
	wantStatus(t, "a self-assignable role that invites", res, http.StatusBadRequest)
}

func TestMemberInviteLimitsAreEnforcedOnTheServer(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	member := a.register(owner, "maya")
	setMemberInviteLimits(t, owner, 2, 3, 5, 4)

	for name, body := range map[string]map[string]any{
		"too long":      {"expires_in_hours": 3},
		"too many uses": {"max_uses": 4},
		"zero hours":    {"expires_in_hours": 0},
		"a custom code": {"code": "my-own-guessable-code"},
	} {
		res := member.post(meInvites, body)
		if res.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, res.status, res.body)
		}
	}
	// What the request leaves open is cut down to the bounds, never unlimited.
	inv := createMemberInvite(t, member, map[string]any{})
	if *inv.MaxUses != 3 || time.Until(*inv.ExpiresAt) > 2*time.Hour+time.Minute {
		t.Fatalf("defaults under the bounds: %+v", inv)
	}
	if list := ownInvites(t, member); list.Limits.MaxExpiresInHours != 2 || list.Limits.MaxUses != 3 {
		t.Fatalf("limits shown to the member: %+v", list.Limits)
	}
	wantStatus(t, "a member cannot set the limits", member.put("/api/admin/invites/limits", map[string]any{}), http.StatusForbidden)
	wantStatus(t, "out of range", owner.put("/api/admin/invites/limits", map[string]any{"max_expires_in_hours": 0, "max_uses": 1, "max_active": 1, "max_per_hour": 1}), http.StatusBadRequest)
}

func TestMemberInvitesStopAtTheActiveCapAndTheHourlyLimit(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	member := a.register(owner, "maya")

	setMemberInviteLimits(t, owner, 24, 5, 2, 10)
	first := createMemberInvite(t, member, map[string]any{})
	createMemberInvite(t, member, map[string]any{})
	res := member.post(meInvites, map[string]any{})
	wantStatus(t, "active cap", res, http.StatusConflict)
	wantStatus(t, "revoke one", member.delete(meInvites+"/"+first.ID.String()), http.StatusNoContent)
	createMemberInvite(t, member, map[string]any{})

	// Revoking does not reset the hourly count.
	setMemberInviteLimits(t, owner, 24, 5, 10, 3)
	other := a.register(owner, "otto")
	for i := 0; i < 3; i++ {
		inv := createMemberInvite(t, other, map[string]any{})
		wantStatus(t, "revoke", other.delete(meInvites+"/"+inv.ID.String()), http.StatusNoContent)
	}
	res = other.post(meInvites, map[string]any{})
	wantStatus(t, "hourly limit", res, http.StatusTooManyRequests)
	if res.header.Get("Retry-After") == "" {
		t.Fatal("the hourly limit sends no Retry-After")
	}

	// Whoever holds Manage invites is not held to the caps; the bounds on one invite stay.
	setMemberInviteLimits(t, owner, 24, 5, 1, 1)
	for i := 0; i < 3; i++ {
		createMemberInvite(t, owner, map[string]any{})
	}
}

func TestMembersSeeAndRevokeOnlyTheirOwnInvites(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	maya, otto := a.register(owner, "maya"), a.register(owner, "otto")
	mine, theirs := createMemberInvite(t, maya, map[string]any{}), createMemberInvite(t, otto, map[string]any{})

	if list := ownInvites(t, maya); len(list.Invites) != 1 || list.Invites[0].ID != mine.ID {
		t.Fatalf("maya sees %+v", list.Invites)
	}
	wantStatus(t, "revoke another's invite", maya.delete(meInvites+"/"+theirs.ID.String()), http.StatusNotFound)
	wantStatus(t, "an unknown invite", maya.delete(meInvites+"/"+uuid.NewString()), http.StatusNotFound)
	wantStatus(t, "a member has no other invite route", maya.get("/api/admin/invites"), http.StatusForbidden)
	wantStatus(t, "nor an edit", maya.patch("/api/admin/invites/"+mine.ID.String(), map[string]any{"max_uses": 2}), http.StatusForbidden)
	if list := ownInvites(t, otto); len(list.Invites) != 1 {
		t.Fatal("another's revoke attempt removed an invite")
	}

	// The admin list names who created each invite and can revoke any.
	var all []auth.Invite
	owner.get("/api/admin/invites").decode(t, &all)
	names := map[uuid.UUID]string{}
	for _, inv := range all {
		names[inv.ID] = inv.CreatedByName
	}
	if names[mine.ID] != "maya" || names[theirs.ID] != "otto" {
		t.Fatalf("admin list: %+v", all)
	}
	wantStatus(t, "an admin revokes any", owner.delete("/api/admin/invites/"+theirs.ID.String()), http.StatusNoContent)
	wantStatus(t, "revoke own", maya.delete(meInvites+"/"+mine.ID.String()), http.StatusNoContent)
}

func TestMemberInvitesAreAudited(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	maya := a.register(owner, "maya")
	inv := createMemberInvite(t, maya, map[string]any{})
	wantStatus(t, "revoke", maya.delete(meInvites+"/"+inv.ID.String()), http.StatusNoContent)
	setMemberInviteLimits(t, owner, 12, 5, 5, 5)

	page := auditEntries(t, owner, "limit=200")
	seen := map[string]string{}
	for _, e := range page.Entries {
		seen[e.Action] = e.ActorName
		if body := string(mustJSON(e)); strings.Contains(body, inv.Code) {
			t.Fatalf("an audit entry holds the invite code: %s", body)
		}
	}
	for _, action := range []string{"invite.member_create", "invite.member_revoke"} {
		if seen[action] != "maya" {
			t.Errorf("%s written by %q, want maya (have %v)", action, seen[action], seen)
		}
	}
	if _, ok := seen["invite.limits"]; !ok {
		t.Errorf("no invite.limits entry in %v", seen)
	}
}
