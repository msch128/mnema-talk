//go:build integration

package db_test

import (
	"context"
	_ "embed"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
)

//go:embed migrations/0097_starter_roles_reports_kick.sql
var reportsKickSQL string

// An upgrade gives the built-in Moderator and Admin the report queue and the
// kick, leaves every other role as it was, and a second run changes nothing.
func TestBuiltInRolesGetReportsAndKickOnUpgrade(t *testing.T) {
	p := starterDB(t)
	ctx := context.Background()
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tx := beginRolledBack(t, p)
	// Put the two roles back to what 0034 to 0089 left on an existing install.
	old := moderatorBits() | perm.ManageEvents | perm.BypassNewMemberLimits | perm.EditWiki | perm.ManageWiki
	if _, err := tx.Exec(ctx, `UPDATE roles SET permissions = $1 WHERE name = 'Moderator'`, int64(old)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE roles SET permissions = $1 WHERE name = 'Admin'`, int64(old|adminBits())); err != nil {
		t.Fatal(err)
	}
	mod, admin := readNamed(t, ctx, tx, "Moderator"), readNamed(t, ctx, tx, "Admin")
	custom := insertPlainRole(t, ctx, tx, "Helpers", 20)
	if _, err := tx.Exec(ctx, `UPDATE roles SET permissions = $2 WHERE id = $1`, custom, int64(perm.TimeoutMembers)); err != nil {
		t.Fatal(err)
	}
	lookalike := insertPlainRole(t, ctx, tx, "Moderator", 21) // a second role of that name without the moderator capability
	if _, err := tx.Exec(ctx, `UPDATE roles SET permissions = $2 WHERE id = $1`, lookalike, int64(perm.ManageMessages)); err != nil {
		t.Fatal(err)
	}
	bot := insertPlainRole(t, ctx, tx, "Admin", 22)
	if _, err := tx.Exec(ctx, `UPDATE roles SET permissions = $2, managed = TRUE, bot_user_id = gen_random_uuid() WHERE id = $1`, bot, int64(perm.TimeoutMembers)); err != nil {
		t.Fatal(err)
	}
	// Imported roles that share a built-in name and hold Timeout members (what a
	// Discord import maps moderate-members to): another colour, or the seed's
	// colour but created later. Neither is the seeded role.
	imported := insertPlainRole(t, ctx, tx, "Moderator", 23)
	if _, err := tx.Exec(ctx, `UPDATE roles SET permissions = $2, color = 255, hoist = TRUE WHERE id = $1`, imported, int64(perm.TimeoutMembers)); err != nil {
		t.Fatal(err)
	}
	copycat := insertPlainRole(t, ctx, tx, "Admin", 24)
	if _, err := tx.Exec(ctx, `UPDATE roles SET permissions = $2, color = $3, hoist = TRUE WHERE id = $1`, copycat, int64(perm.TimeoutMembers), admin.color); err != nil {
		t.Fatal(err)
	}
	// The Owner recoloured the seeded Admin and made it mentionable: still the seeded role.
	if _, err := tx.Exec(ctx, `UPDATE roles SET color = 255, hoist = FALSE, mentionable = TRUE WHERE id = $1`, admin.id); err != nil {
		t.Fatal(err)
	}
	beforeOthers := []roleRow{readRole(t, ctx, tx, custom), readRole(t, ctx, tx, lookalike), readRole(t, ctx, tx, bot), readRole(t, ctx, tx, imported), readRole(t, ctx, tx, copycat)}
	everyone := everyonePerms(t, ctx, tx)

	if _, err := tx.Exec(ctx, reportsKickSQL); err != nil {
		t.Fatal(err)
	}
	both := perm.ManageReports | perm.KickMembers
	if got := perm.Set(readRole(t, ctx, tx, mod.id).perms); got != perm.Set(mod.perms)|both {
		t.Fatalf("Moderator = %b, want %b", got, perm.Set(mod.perms)|both)
	}
	if got := perm.Set(readRole(t, ctx, tx, admin.id).perms); got != perm.Set(admin.perms)|both {
		t.Fatalf("Admin = %b, want %b", got, perm.Set(admin.perms)|both)
	}
	for _, before := range beforeOthers {
		if got := readRole(t, ctx, tx, before.id); got != before {
			t.Fatalf("role %q changed:\n got  %+v\n want %+v", before.name, got, before)
		}
	}
	if got := everyonePerms(t, ctx, tx); got != everyone {
		t.Fatalf("@everyone = %b, want %b", got, everyone)
	}

	again := readRole(t, ctx, tx, mod.id)
	if _, err := tx.Exec(ctx, reportsKickSQL); err != nil {
		t.Fatal(err)
	}
	if got := readRole(t, ctx, tx, mod.id); got != again {
		t.Fatalf("a second run changed the Moderator:\n got  %+v\n want %+v", got, again)
	}
}
