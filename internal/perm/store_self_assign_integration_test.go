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

	if _, changed, err := w.svc.SetSelfRole(ctx, w.member.ID, open.ID, true); err != nil || !changed {
		t.Fatalf("take: changed=%v err=%v", changed, err)
	}
	if got := w.heldRoles(t, w.member.ID); len(got) != 1 || got[0] != open.ID {
		t.Fatalf("roles after take: %v", got)
	}
	// Taking it again is no change.
	if _, changed, err := w.svc.SetSelfRole(ctx, w.member.ID, open.ID, true); err != nil || changed {
		t.Fatalf("take twice: changed=%v err=%v", changed, err)
	}
	if _, changed, err := w.svc.SetSelfRole(ctx, w.member.ID, open.ID, false); err != nil || !changed {
		t.Fatalf("drop: changed=%v err=%v", changed, err)
	}
	if got := w.heldRoles(t, w.member.ID); len(got) != 0 {
		t.Fatalf("roles after drop: %v", got)
	}
	if _, changed, err := w.svc.SetSelfRole(ctx, w.member.ID, open.ID, false); err != nil || changed {
		t.Fatalf("drop twice: changed=%v err=%v", changed, err)
	}
	if len(notified.calls) != 2 || len(notified.calls[0]) != 1 || notified.calls[0][0] != w.member.ID {
		t.Fatalf("permissions were refreshed for the member twice, got %v", notified.calls)
	}

	_, _, err = w.svc.SetSelfRole(ctx, w.member.ID, closed.ID, true)
	wantAPI(t, "a role that is not open", err, 403)
	_, _, err = w.svc.SetSelfRole(ctx, w.member.ID, w.everyone, true)
	wantAPI(t, "@everyone", err, 403)
	_, _, err = w.svc.SetSelfRole(ctx, w.member.ID, uuid.New(), true)
	wantAPI(t, "an unknown role", err, 404)
	_, _, err = w.svc.SetSelfRole(ctx, w.owner.ID, open.ID, true)
	wantAPI(t, "the Owner", err, 403)
	_, _, err = w.svc.SetSelfRole(ctx, uuid.New(), open.ID, true)
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
	_, _, err := w.svc.SetSelfRole(ctx, w.member.ID, open.ID, true)
	wantAPI(t, "a role that gained a ban permission", err, 403)
	list, err := w.svc.SelfAssignableRoles(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("it is no longer listed: %v %+v", err, list)
	}
}

type countingNotifier struct{ calls [][]uuid.UUID }

func (c *countingNotifier) PermissionsChanged(ids []uuid.UUID) { c.calls = append(c.calls, ids) }
