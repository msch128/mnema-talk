//go:build integration

package db_test

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil/pgtest"
)

//go:embed migrations/0034_starter_roles.sql
var starterRoleSQL string

// Colours stored by migration 0034: muted amber and a deeper blue.
const (
	moderatorAmber = (196 << 16) | (163 << 8) | 90 // #C4A35A
	adminBlue      = (44 << 16) | (74 << 8) | 124  // #2C4A7C
)

func moderatorBits() perm.Set {
	return perm.ManageMessages | perm.DisconnectMembers | perm.TimeoutMembers | perm.ViewAuditLog
}

func adminBits() perm.Set {
	return moderatorBits() | perm.ManageChannels | perm.ManageRoles | perm.CreateInvites | perm.ManageMembers | perm.BanMembers
}

// starterDB is a fresh schema on the shared test Postgres, migrated by the
// caller. Package db cannot import perm (that import cycle is forbidden), so
// this external test builds the same scratch schema as the other db tests.
func starterDB(t *testing.T) *db.Pool {
	t.Helper()
	dsn, err := pgtest.DSN()
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	ctx := context.Background()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand read: %v", err)
	}
	schema := "starter_" + hex.EncodeToString(b)

	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Logf("cleanup drop schema %s: %v", schema, err)
		}
		admin.Close()
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	p, err := db.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

// After migrate, Moderator and Admin exist empty with exactly the documented
// bits, above every role that was already there. Running the same insert
// again does not add a second copy or change the first.
func TestStarterRolesAreSeededEmpty(t *testing.T) {
	p := starterDB(t)
	ctx := context.Background()
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	first := assertSeededStarterRoles(t, ctx, p)
	if _, err := p.Exec(ctx, starterRoleSQL); err != nil {
		t.Fatal(err)
	}
	if again := assertSeededStarterRoles(t, ctx, p); again != first {
		t.Fatalf("running the insert again changed the roles:\n got  %+v\n want %+v", again, first)
	}
}

func testStarterRolesBothNames(t *testing.T, p *db.Pool, ctx context.Context) {
	tx := beginRolledBack(t, p)
	mod := twistRole(t, ctx, tx, "Moderator", 4, 1, perm.Administrator)
	admin := twistRole(t, ctx, tx, "Admin", 9, 2, perm.ManageBots)
	keep := insertPlainRole(t, ctx, tx, "Keep", 5)
	holdRoles(t, ctx, tx, "kept-both", mod, admin)
	before := map[uuid.UUID]roleRow{
		mod: readRole(t, ctx, tx, mod), admin: readRole(t, ctx, tx, admin), keep: readRole(t, ctx, tx, keep),
	}
	everyone := everyonePerms(t, ctx, tx)
	if _, err := tx.Exec(ctx, starterRoleSQL); err != nil {
		t.Fatal(err)
	}
	for id, row := range before {
		if got := readRole(t, ctx, tx, id); got != row {
			t.Fatalf("existing role changed:\n got  %+v\n want %+v", got, row)
		}
	}
	if got := everyonePerms(t, ctx, tx); got != everyone || got != perm.DefaultEveryone {
		t.Fatalf("@everyone = %b, want %b", got, perm.DefaultEveryone)
	}
	if n := countNamed(t, ctx, tx, "Moderator"); n != 1 {
		t.Fatalf("Moderator count = %d", n)
	}
	if n := countNamed(t, ctx, tx, "Admin"); n != 1 {
		t.Fatalf("Admin count = %d", n)
	}
}

func testStarterRolesAdminMissing(t *testing.T, p *db.Pool, ctx context.Context) {
	tx := beginRolledBack(t, p)
	if _, err := tx.Exec(ctx, `DELETE FROM roles WHERE name = 'Admin'`); err != nil {
		t.Fatal(err)
	}
	mod := twistRole(t, ctx, tx, "Moderator", 4, 3, perm.Administrator)
	keep := insertPlainRole(t, ctx, tx, "Keep", 11)
	holdRoles(t, ctx, tx, "kept-mod", mod)
	beforeMod, beforeKeep := readRole(t, ctx, tx, mod), readRole(t, ctx, tx, keep)
	if _, err := tx.Exec(ctx, starterRoleSQL); err != nil {
		t.Fatal(err)
	}
	if got := readRole(t, ctx, tx, mod); got != beforeMod {
		t.Fatalf("Moderator changed:\n got  %+v\n want %+v", got, beforeMod)
	}
	if got := readRole(t, ctx, tx, keep); got != beforeKeep {
		t.Fatalf("Keep changed:\n got  %+v\n want %+v", got, beforeKeep)
	}
	if got := everyonePerms(t, ctx, tx); got != perm.DefaultEveryone {
		t.Fatalf("@everyone = %b", got)
	}
	admin := readNamed(t, ctx, tx, "Admin")
	assertEmptySeed(t, admin, adminBits(), adminBlue)
	var maxOther int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), 0) FROM roles WHERE id <> $1`, admin.id).Scan(&maxOther); err != nil {
		t.Fatal(err)
	}
	if admin.position != maxOther+1 || admin.position <= beforeMod.position {
		t.Fatalf("Admin at %d, want %d (above Moderator at %d)", admin.position, maxOther+1, beforeMod.position)
	}
}

// A role that already has the name keeps its bits, position and members.
// Only a missing name is inserted, at the top, without moving the others.
func TestStarterRolesLeaveExistingNamesAlone(t *testing.T) {
	p := starterDB(t)
	ctx := context.Background()
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Run("both names", func(t *testing.T) {
		testStarterRolesBothNames(t, p, ctx)
	})
	t.Run("admin name missing", func(t *testing.T) {
		testStarterRolesAdminMissing(t, p, ctx)
	})
}

type roleRow struct {
	id                          uuid.UUID
	name                        string
	color, position             int
	perms                       int64
	hoist, mentionable, managed bool
	holders                     int
}

func assertSeededStarterRoles(t *testing.T, ctx context.Context, q db.Querier) (pair [2]roleRow) {
	t.Helper()
	if got := everyonePerms(t, ctx, q); got != perm.DefaultEveryone {
		t.Fatalf("@everyone = %b, want %b", got, perm.DefaultEveryone)
	}
	assertNoAccountRoles(t, ctx, q)
	pair[0] = readNamed(t, ctx, q, "Moderator")
	pair[1] = readNamed(t, ctx, q, "Admin")
	assertEmptySeed(t, pair[0], moderatorBits(), moderatorAmber)
	assertEmptySeed(t, pair[1], adminBits(), adminBlue)
	var below int
	err := q.QueryRow(ctx, `SELECT COALESCE(MAX(position), 0) FROM roles WHERE id <> $1 AND id <> $2`, pair[0].id, pair[1].id).Scan(&below)
	if err != nil {
		t.Fatal(err)
	}
	if pair[0].position != below+1 || pair[1].position != below+2 {
		t.Fatalf("positions Moderator=%d Admin=%d, want %d and %d", pair[0].position, pair[1].position, below+1, below+2)
	}
	return pair
}

func assertEmptySeed(t *testing.T, r roleRow, bits perm.Set, color int) {
	t.Helper()
	got := perm.Set(r.perms)
	forbidden := perm.Administrator | perm.ManageBots | perm.ManageWebhooks
	if got != bits || got.Has(forbidden) {
		t.Fatalf("%s unexpected permissions: got %b, want %b (forbidden: %b)", r.name, got, bits, forbidden)
	}
	if r.color != color || !r.hoist {
		t.Fatalf("%s unexpected color or hoist: color=%d hoist=%v", r.name, r.color, r.hoist)
	}
	if r.mentionable || r.managed || r.holders != 0 {
		t.Fatalf("%s unexpected role flags: mentionable=%v managed=%v holders=%d", r.name, r.mentionable, r.managed, r.holders)
	}
}

func assertNoAccountRoles(t *testing.T, ctx context.Context, q db.Querier) {
	t.Helper()
	var n int
	err := q.QueryRow(ctx, `SELECT COUNT(*)::int FROM roles WHERE name IN ('User', 'Member', 'Owner')`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("found %d User, Member or Owner roles", n)
	}
}

func readNamed(t *testing.T, ctx context.Context, q db.Querier, name string) roleRow {
	t.Helper()
	if n := countNamed(t, ctx, q, name); n != 1 {
		t.Fatalf("%s count = %d, want 1", name, n)
	}
	var id uuid.UUID
	if err := q.QueryRow(ctx, `SELECT id FROM roles WHERE name = $1`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return readRole(t, ctx, q, id)
}

func readRole(t *testing.T, ctx context.Context, q db.Querier, id uuid.UUID) roleRow {
	t.Helper()
	var r roleRow
	err := q.QueryRow(ctx, `
		SELECT id, name, color, position, permissions, hoist, mentionable, managed,
		       (SELECT COUNT(*)::int FROM user_roles ur WHERE ur.role_id = roles.id)
		FROM roles WHERE id = $1`, id).Scan(
		&r.id, &r.name, &r.color, &r.position, &r.perms, &r.hoist, &r.mentionable, &r.managed, &r.holders)
	if err != nil {
		t.Fatalf("role %s: %v", id, err)
	}
	return r
}

func countNamed(t *testing.T, ctx context.Context, q db.Querier, name string) int {
	t.Helper()
	var n int
	if err := q.QueryRow(ctx, `SELECT COUNT(*)::int FROM roles WHERE name = $1`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func everyonePerms(t *testing.T, ctx context.Context, q db.Querier) perm.Set {
	t.Helper()
	var n int64
	if err := q.QueryRow(ctx, `SELECT permissions FROM roles WHERE is_everyone`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return perm.Set(n)
}

func beginRolledBack(t *testing.T, p *db.Pool) pgx.Tx {
	t.Helper()
	tx, err := p.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

func twistRole(t *testing.T, ctx context.Context, tx pgx.Tx, name string, position, color int, bits perm.Set) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		UPDATE roles
		   SET position = $2, color = $3, permissions = $4, hoist = FALSE, mentionable = TRUE
		 WHERE name = $1
		RETURNING id`, name, position, color, int64(bits)).Scan(&id)
	if err != nil {
		t.Fatalf("twist %s: %v", name, err)
	}
	return id
}

func insertPlainRole(t *testing.T, ctx context.Context, tx pgx.Tx, name string, position int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO roles (name, position) VALUES ($1, $2) RETURNING id`, name, position).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func holdRoles(t *testing.T, ctx context.Context, tx pgx.Tx, username string, ids ...uuid.UUID) {
	t.Helper()
	var user uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ($1, 'h', 'x') RETURNING id`, username).Scan(&user)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, user, id); err != nil {
			t.Fatal(err)
		}
	}
}
