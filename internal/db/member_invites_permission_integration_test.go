//go:build integration

package db_test

import (
	"context"
	_ "embed"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
)

//go:embed migrations/0108_member_invites_permission.sql
var memberInvitesPermissionSQL string

// A fresh install gives @everyone the right to create invites; an existing
// install keeps what members could do and gives it only to the roles that
// already manage invites, never to a managed (bot) role or an unrelated one.
func TestMemberInvitesPermissionOnFreshAndExistingInstalls(t *testing.T) {
	p := starterDB(t)
	ctx := context.Background()
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if got := everyonePerms(t, ctx, p); got != perm.DefaultEveryone {
		t.Fatalf("a fresh install seeds @everyone with %b, want perm.DefaultEveryone %b", got, perm.DefaultEveryone)
	}

	tx := beginRolledBack(t, p)
	if _, err := tx.Exec(ctx, `UPDATE roles SET permissions = permissions & ~(1::bigint << 30) WHERE is_everyone`); err != nil {
		t.Fatal(err)
	}
	inviter := insertPlainRole(t, ctx, tx, "Inviters", 30)
	other := insertPlainRole(t, ctx, tx, "Others", 31)
	managed := insertPlainRole(t, ctx, tx, "BotRole", 32)
	for id, bits := range map[any]perm.Set{inviter: perm.CreateInvites, other: perm.TimeoutMembers, managed: perm.CreateInvites} {
		if _, err := tx.Exec(ctx, `UPDATE roles SET permissions = $2 WHERE id = $1`, id, int64(bits)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE roles SET managed = TRUE, bot_user_id = gen_random_uuid() WHERE id = $1`, managed); err != nil {
		t.Fatal(err)
	}

	// No account yet: the fresh-install rule gives @everyone the permission back.
	if _, err := tx.Exec(ctx, memberInvitesPermissionSQL); err != nil {
		t.Fatal(err)
	}
	if got := everyonePerms(t, ctx, tx); !got.Has(perm.CreateMemberInvites) {
		t.Fatalf("a database without accounts: @everyone = %b, want the permission", got)
	}

	// An existing install (an account exists): @everyone stays as it was.
	if _, err := tx.Exec(ctx, `UPDATE roles SET permissions = permissions & ~(1::bigint << 30) WHERE is_everyone`); err != nil {
		t.Fatal(err)
	}
	holdRoles(t, ctx, tx, "someone")
	if _, err := tx.Exec(ctx, memberInvitesPermissionSQL); err != nil {
		t.Fatal(err)
	}
	if got := everyonePerms(t, ctx, tx); got.Has(perm.CreateMemberInvites) {
		t.Fatalf("an existing install: @everyone = %b gained the permission", got)
	}
	if got := perm.Set(readRole(t, ctx, tx, inviter).perms); got != perm.CreateInvites|perm.CreateMemberInvites {
		t.Fatalf("a role that manages invites = %b, want it to keep creating them", got)
	}
	if got := perm.Set(readRole(t, ctx, tx, other).perms); got != perm.TimeoutMembers {
		t.Fatalf("an unrelated role changed: %b", got)
	}
	if got := perm.Set(readRole(t, ctx, tx, managed).perms); got != perm.CreateInvites {
		t.Fatalf("a managed role changed: %b", got)
	}
}
