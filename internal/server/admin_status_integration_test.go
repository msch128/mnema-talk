//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestAdminStatusPermissions(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	member := a.register(owner, "member")

	// 1. Anonymous -> 401
	if res := a.anon().get("/api/admin/status"); res.status != http.StatusUnauthorized {
		t.Fatalf("anonymous: expected 401, got %d (%s)", res.status, res.body)
	}

	// 2. Normal member without Administrator -> 403
	if res := member.get("/api/admin/status"); res.status != http.StatusForbidden {
		t.Fatalf("member: expected 403, got %d (%s)", res.status, res.body)
	}

	// 3. Member with a non-admin role (e.g. CreateInvites) -> 403
	inviterRole := createRole(t, owner, "inviters", perm.CreateInvites)
	inviter := a.register(owner, "inviter")
	assignRoles(t, owner, inviter, inviterRole)
	if res := inviter.get("/api/admin/status"); res.status != http.StatusForbidden {
		t.Fatalf("inviter: expected 403, got %d (%s)", res.status, res.body)
	}

	// 4. Member with Administrator role -> 200
	adminRole := createRole(t, owner, "admins", perm.Administrator)
	adminMember := a.register(owner, "adminmember")
	assignRoles(t, owner, adminMember, adminRole)
	if res := adminMember.get("/api/admin/status"); res.status != http.StatusOK {
		t.Fatalf("admin member: expected 200, got %d (%s)", res.status, res.body)
	}

	// 5. Owner -> 200
	if res := owner.get("/api/admin/status"); res.status != http.StatusOK {
		t.Fatalf("owner: expected 200, got %d (%s)", res.status, res.body)
	}
}

func TestAdminStatusPayloadAndHardening(t *testing.T) {
	a := newApp(t, true)
	owner := a.seedAdmin()

	conn, _, err := owner.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := conn.expect("server_info", 2*time.Second); !ok {
		t.Fatal("no server_info")
	}

	res := owner.get("/api/admin/status")
	if res.status != http.StatusOK {
		t.Fatalf("status: %d %s", res.status, res.body)
	}
	if cc := res.header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected Cache-Control: no-store, got %q", cc)
	}

	var st OperatorStatus
	res.decode(t, &st)

	if !strings.HasPrefix(st.Version.GoVersion, "go") {
		t.Fatalf("expected GoVersion to start with 'go', got %q", st.Version.GoVersion)
	}

	files, err := db.MigrationFiles()
	if err != nil {
		t.Fatalf("migration files: %v", err)
	}
	if !st.Database.Reachable {
		t.Fatalf("expected reachable database: %+v", st.Database)
	}
	if st.Database.AppliedMigrations != len(files) || st.Database.PendingMigrations != 0 {
		t.Fatalf("database migrations mismatch: applied=%d pending=%d want=%d",
			st.Database.AppliedMigrations, st.Database.PendingMigrations, len(files))
	}
	if st.Database.LatestMigration != files[len(files)-1] {
		t.Fatalf("database latest migration: got %q, want %q", st.Database.LatestMigration, files[len(files)-1])
	}
	if st.Database.SizeBytes == nil || *st.Database.SizeBytes <= 0 {
		t.Fatalf("expected positive database size, got %+v", st.Database.SizeBytes)
	}

	if st.Backup.LastBackupAge != "unknown" || st.Backup.LastRestoreTestAge != "unknown" {
		t.Fatalf("expected unknown backup ages, got %+v", st.Backup)
	}
	if st.Backup.Hint != "doc/operations.md#backups-and-restore" {
		t.Fatalf("unexpected backup hint: %q", st.Backup.Hint)
	}

	if st.Activity.ActiveSessions < 1 {
		t.Fatalf("expected at least 1 active session, got %d", st.Activity.ActiveSessions)
	}

	body := string(res.body)
	for _, secret := range []string{"postgres://", "TEST_DATABASE_URL", "password"} {
		if strings.Contains(body, secret) {
			t.Fatalf("operator status leaked potential secret %q in %s", secret, body)
		}
	}
}
