//go:build integration

package db_test

import (
	"context"
	_ "embed"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/perm"
)

//go:embed migrations/0070_events.sql
var eventsMigrationSQL string

// eventsGrantSQL is the role grant at the end of migration 0070, so the test
// runs exactly the statement an upgrade runs without recreating its tables.
func eventsGrantSQL(t *testing.T) string {
	t.Helper()
	i := strings.Index(eventsMigrationSQL, "UPDATE roles")
	if i < 0 {
		t.Fatal("migration 0070 has no role grant")
	}
	stmt, _, ok := strings.Cut(eventsMigrationSQL[i:], ";")
	if !ok {
		t.Fatal("migration 0070 role grant is not terminated")
	}
	return stmt
}

func insertRoleWith(t *testing.T, ctx context.Context, tx pgx.Tx, name string, position int, bits perm.Set) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO roles (name, position, permissions) VALUES ($1, $2, $3) RETURNING id`,
		name, position, int64(bits)).Scan(&id)
	if err != nil {
		t.Fatalf("insert role %s: %v", name, err)
	}
	return id
}

// Manage events follows the ability to moderate members, not a role name:
// a renamed moderating role gets it, a role that is only called "Moderator"
// does not, and @everyone and bot roles never do.
func TestEventsGrantFollowsModerationNotNames(t *testing.T) {
	p := starterDB(t)
	ctx := context.Background()
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tx := beginRolledBack(t, p)
	if _, err := tx.Exec(ctx, `UPDATE roles SET name = name || '-old' WHERE NOT is_everyone`); err != nil {
		t.Fatal(err)
	}
	helpers := insertRoleWith(t, ctx, tx, "Helpers", 50, perm.TimeoutMembers)
	nameOnly := insertRoleWith(t, ctx, tx, "Moderator", 51, perm.ManageMessages)
	// There is only one managed (bot) role; give it the moderating bit.
	var bot uuid.UUID
	if err := tx.QueryRow(ctx, `UPDATE roles SET permissions = $1 WHERE managed RETURNING id`, int64(perm.TimeoutMembers)).Scan(&bot); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, eventsGrantSQL(t)); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		id   uuid.UUID
		want bool
	}{{"Helpers", helpers, true}, {"Moderator by name only", nameOnly, false}, {"managed bot role", bot, false}}
	for _, c := range cases {
		got := perm.Set(readRole(t, ctx, tx, c.id).perms).Has(perm.ManageEvents)
		if got != c.want {
			t.Errorf("%s: Manage events = %v, want %v", c.name, got, c.want)
		}
	}
	if everyonePerms(t, ctx, tx).Has(perm.ManageEvents) {
		t.Error("@everyone got Manage events")
	}
}
