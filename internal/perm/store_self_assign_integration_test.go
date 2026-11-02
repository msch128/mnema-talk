//go:build integration

package perm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (w *world) selfRole(t *testing.T, name string, perms perm.Set, open bool) perm.Role {
	t.Helper()
	desc := "for testing"
	role, err := w.svc.CreateRole(context.Background(), w.owner, perm.RoleInput{Name: &name, Permissions: &perms, SelfAssignable: &open, Description: &desc})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return role
}

func TestTheFlagIsRefusedWhereItWouldHandOutAdministration(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	yes := true
	for name, perms := range map[string]perm.Set{
		"administrator": perm.Administrator,
		"manage roles":  perm.ManageRoles,
		"kick members":  perm.KickMembers,
		"ban members":   perm.BanMembers,
		"channels":      perm.ManageChannels | perm.SendMessages,
		"messages":      perm.ManageMessages,
		"stage":         perm.ManageStage,
		"events":        perm.ManageEvents,
	} {
		n, p := "r "+name, perms
		_, err := w.svc.CreateRole(ctx, w.owner, perm.RoleInput{Name: &n, Permissions: &p, SelfAssignable: &yes})
		wantAPI(t, "create "+name, err, 400)
	}

	plain := w.selfRole(t, "Gamer", perm.SendMessages|perm.Connect, true)
	if !plain.SelfAssignable || plain.Description != "for testing" {
		t.Fatalf("flag and description not stored: %+v", plain)
	}
	// Later permission growth is refused while the flag is on ...
	grown := perm.SendMessages | perm.ManageRoles
	_, err := w.svc.UpdateRole(ctx, w.owner, plain.ID, perm.RoleInput{Permissions: &grown})
	wantAPI(t, "grow a self-assignable role", err, 400)
	// ... and works once the flag is off in the same request or before.
	no := false
	if _, err := w.svc.UpdateRole(ctx, w.owner, plain.ID, perm.RoleInput{Permissions: &grown, SelfAssignable: &no}); err != nil {
		t.Fatalf("unflag with growth: %v", err)
	}
	_, err = w.svc.UpdateRole(ctx, w.owner, plain.ID, perm.RoleInput{SelfAssignable: &yes})
	wantAPI(t, "flag a role that has admin permissions", err, 400)

	// @everyone and managed roles never are.
	_, err = w.svc.UpdateRole(ctx, w.owner, w.everyone, perm.RoleInput{SelfAssignable: &yes})
	wantAPI(t, "flag @everyone", err, 400)
	roles, _ := w.svc.Roles(ctx)
	for _, r := range roles {
		if r.Managed {
			_, err = w.svc.UpdateRole(ctx, w.owner, r.ID, perm.RoleInput{SelfAssignable: &yes})
			wantAPI(t, "flag a managed role", err, 400)
		}
	}
	tooLong := strings.Repeat("x", 201)
	_, err = w.svc.UpdateRole(ctx, w.owner, plain.ID, perm.RoleInput{Description: &tooLong})
	wantAPI(t, "overlong description", err, 400)
}

func TestMembersTakeAndDropOpenRolesOnly(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	open := w.selfRole(t, "Gamer", perm.SendMessages, true)
	closed := w.selfRole(t, "Staff", perm.SendMessages, false)
	notified := &countingNotifier{}
	w.svc.SetNotifier(notified)

	list, err := w.svc.SelfAssignableRoles(ctx)
	if err != nil || len(list) != 1 || list[0].ID != open.ID {
		t.Fatalf("the picker lists exactly the open role: %v %+v", err, list)
	}

	if _, changed, _, err := w.svc.SetSelfRole(ctx, w.member.ID, open.ID, true); err != nil || !changed {
		t.Fatalf("take: changed=%v err=%v", changed, err)
	}
	if got := w.heldRoles(t, w.member.ID); len(got) != 1 || got[0] != open.ID {
		t.Fatalf("roles after take: %v", got)
	}
	// Taking it again is no change.
	if _, changed, _, err := w.svc.SetSelfRole(ctx, w.member.ID, open.ID, true); err != nil || changed {
		t.Fatalf("take twice: changed=%v err=%v", changed, err)
	}
	if _, changed, _, err := w.svc.SetSelfRole(ctx, w.member.ID, open.ID, false); err != nil || !changed {
		t.Fatalf("drop: changed=%v err=%v", changed, err)
	}
	if got := w.heldRoles(t, w.member.ID); len(got) != 0 {
		t.Fatalf("roles after drop: %v", got)
	}
	if _, changed, _, err := w.svc.SetSelfRole(ctx, w.member.ID, open.ID, false); err != nil || changed {
		t.Fatalf("drop twice: changed=%v err=%v", changed, err)
	}
	if len(notified.calls) != 2 || len(notified.calls[0]) != 1 || notified.calls[0][0] != w.member.ID {
		t.Fatalf("permissions were refreshed for the member twice, got %v", notified.calls)
	}

	_, _, _, err = w.svc.SetSelfRole(ctx, w.member.ID, closed.ID, true)
	wantAPI(t, "a role that is not open", err, 403)
	_, _, _, err = w.svc.SetSelfRole(ctx, w.member.ID, w.everyone, true)
	wantAPI(t, "@everyone", err, 403)
	_, _, _, err = w.svc.SetSelfRole(ctx, w.member.ID, uuid.New(), true)
	wantAPI(t, "an unknown role", err, 404)
	_, _, _, err = w.svc.SetSelfRole(ctx, w.owner.ID, open.ID, true)
	wantAPI(t, "the Owner", err, 403)
	_, _, _, err = w.svc.SetSelfRole(ctx, uuid.New(), open.ID, true)
	wantAPI(t, "an unknown account", err, 404)
}

func TestAnOpenRoleThatGainedAdminPermissionsIsClosedAtOnce(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	open := w.selfRole(t, "Gamer", perm.SendMessages, true)
	// Whatever path set the bits (an import, a direct write), taking it is refused.
	if _, err := w.p.Exec(ctx, `UPDATE roles SET permissions = $2 WHERE id = $1`, open.ID, int64(perm.SendMessages|perm.BanMembers)); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := w.svc.SetSelfRole(ctx, w.member.ID, open.ID, true)
	wantAPI(t, "a role that gained a ban permission", err, 403)
	list, err := w.svc.SelfAssignableRoles(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("it is no longer listed: %v %+v", err, list)
	}
}

type countingNotifier struct{ calls [][]uuid.UUID }

func (c *countingNotifier) PermissionsChanged(ids []uuid.UUID) { c.calls = append(c.calls, ids) }

func TestAChannelOverwriteThatAllowsModerationClosesTheRole(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	open := w.selfRole(t, "Gamer", perm.SendMessages, true)
	// Setting it on a flagged role is refused ...
	err := w.svc.SetOverwrites(ctx, w.owner, perm.TargetChannel, w.channel, []perm.Overwrite{{Type: "role", ID: open.ID, Allow: perm.ManageMessages}})
	wantAPI(t, "moderation allowed to an open role in a channel", err, 400)
	err = w.svc.SetOverwrites(ctx, w.owner, perm.TargetCategory, w.category, []perm.Overwrite{{Type: "role", ID: open.ID, Allow: perm.DisconnectMembers}})
	wantAPI(t, "disconnect allowed to an open role in a category", err, 400)
	// A role that already has such an overwrite cannot be opened.
	other := w.selfRole(t, "Closed", perm.SendMessages, false)
	if err := w.svc.SetOverwrites(ctx, w.owner, perm.TargetChannel, w.channel, []perm.Overwrite{{Type: "role", ID: other.ID, Allow: perm.ManageMessages}}); err != nil {
		t.Fatal(err)
	}
	yes := true
	_, err = w.svc.UpdateRole(ctx, w.owner, other.ID, perm.RoleInput{SelfAssignable: &yes})
	wantAPI(t, "opening a role with a moderation overwrite", err, 400)
	// ... a harmless allow and a deny are fine, and a deny can be dropped again by the member.
	if err := w.svc.SetOverwrites(ctx, w.owner, perm.TargetChannel, w.channel, []perm.Overwrite{{Type: "role", ID: open.ID, Allow: perm.SendMessages, Deny: perm.AddReactions}}); err != nil {
		t.Fatalf("a harmless overwrite: %v", err)
	}
	if _, _, _, err := w.svc.SetSelfRole(ctx, w.member.ID, open.ID, true); err != nil {
		t.Fatalf("take: %v", err)
	}
	// An overwrite that got there some other way closes the role.
	if _, err := w.p.Exec(ctx, `UPDATE permission_overwrites SET allow = $2 WHERE channel_id = $1`, w.channel, int64(perm.ManageMessages)); err != nil {
		t.Fatal(err)
	}
	w.svc.Invalidate()
	_, _, _, err = w.svc.SetSelfRole(ctx, w.other.ID, open.ID, true)
	wantAPI(t, "a role with an overwrite that allows moderation", err, 403)
	list, _ := w.svc.SelfAssignableRoles(ctx)
	if len(list) != 0 {
		t.Fatalf("it is no longer listed: %+v", list)
	}
}

func TestAPositionAboveTheModeratorsGivesNoRank(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	// Created first, so it ends up above the moderators' role created next.
	open := w.selfRole(t, "Gamer", perm.SendMessages, true)
	modName, mod := "Moderators", perm.ManageMembers|perm.KickMembers
	modRole, err := w.svc.CreateRole(ctx, w.owner, perm.RoleInput{Name: &modName, Permissions: &mod})
	if err != nil {
		t.Fatal(err)
	}
	if open.Position >= modRole.Position {
		// The new role goes to the bottom: the flagged one moved up above it.
		after, _ := w.svc.Roles(ctx)
		for _, r := range after {
			if r.ID == open.ID {
				open = r
			}
		}
	}
	if open.Position <= modRole.Position {
		t.Fatalf("setup: the open role (%d) must sit above the moderators' (%d)", open.Position, modRole.Position)
	}
	if err := w.svc.SetMemberRoles(ctx, w.owner, w.other.ID, []uuid.UUID{modRole.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := w.svc.SetSelfRole(ctx, w.member.ID, open.ID, true); err != nil {
		t.Fatal(err)
	}
	snap, err := w.svc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.TopPosition(w.member); got != 0 {
		t.Fatalf("the open role gives rank %d, want none", got)
	}
	if err := snap.CanModerate(w.other, w.member); err != nil {
		t.Fatalf("a moderator can still act on the member: %v", err)
	}
}

func TestTheSelfRoleCallReportsWhatTheMemberHolds(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	a := w.selfRole(t, "Alpha", perm.SendMessages, true)
	b := w.selfRole(t, "Beta", perm.SendMessages, true)
	if _, _, held, err := w.svc.SetSelfRole(ctx, w.member.ID, a.ID, true); err != nil || len(held) != 1 {
		t.Fatalf("after one: %v %v", err, held)
	}
	if _, _, held, _ := w.svc.SetSelfRole(ctx, w.member.ID, b.ID, true); len(held) != 2 {
		t.Fatalf("after two: %v", held)
	}
	if _, _, held, _ := w.svc.SetSelfRole(ctx, w.member.ID, a.ID, false); len(held) != 1 || held[0] != b.ID {
		t.Fatalf("after dropping one: %v", held)
	}
}
