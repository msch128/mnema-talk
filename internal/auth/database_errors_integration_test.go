//go:build integration

package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
)

func TestAuthDatabaseCancellationPropagates(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, query string
		call        func(*db.Pool) error
	}{
		{"seed admin lookup", "SELECT EXISTS (SELECT 1 FROM users WHERE role = 'admin')", func(p *db.Pool) error { return EnsureAdminUser(ctx, p, "cool_guy", testPassword, true) }},
		{"login", "FROM users WHERE LOWER(username)", func(p *db.Pool) error { _, _, e := Login(ctx, p, member.Username, testPassword); return e }},
		{"register invite precheck", "SELECT EXISTS (SELECT 1 FROM invites", func(p *db.Pool) error {
			_, e := Register(ctx, p, "newmember", "", testPassword, "member-invite-k7x2")
			return e
		}},
		{"register locked invite lookup", "FOR UPDATE OF i", func(p *db.Pool) error {
			_, e := Register(ctx, p, "newmember", "", testPassword, "member-invite-k7x2")
			return e
		}},
		{"register username lookup", "SELECT EXISTS (SELECT 1 FROM users WHERE LOWER(username)", func(p *db.Pool) error {
			_, e := Register(ctx, p, "newmember", "", testPassword, "member-invite-k7x2")
			return e
		}},
		{"register insert", "INSERT INTO users (username, display_name, password_hash, role, invite_id)", func(p *db.Pool) error {
			_, e := Register(ctx, p, "newmember", "", testPassword, "member-invite-k7x2")
			return e
		}},
		{"register invite increment", "UPDATE invites SET uses_count", func(p *db.Pool) error {
			_, e := Register(ctx, p, "newmember", "", testPassword, "member-invite-k7x2")
			return e
		}},
		{"change password load", "SELECT password_hash, role FROM users", func(p *db.Pool) error {
			_, e := ChangePassword(ctx, p, member.ID, testPassword, "next-password-value")
			return e
		}},
		{"password confirmation load", "SELECT password_hash FROM users", func(p *db.Pool) error { return CheckUserPassword(ctx, p, member.ID, testPassword) }},
		{"list invitations", "ORDER BY i.created_at", func(p *db.Pool) error { _, e := ListInvites(ctx, p); return e }},
		{"create invitation", "INSERT INTO invites", func(p *db.Pool) error {
			_, e := CreateInvite(ctx, p, admin.ID, "cancelled-invite-k7x2", nil, nil)
			return e
		}},
		{"delete invitation", "DELETE FROM invites", func(p *db.Pool) error { return DeleteInvite(ctx, p, uuid.New()) }},
		{"list admin users", "ORDER BY role = 'admin' DESC", func(p *db.Pool) error { _, e := ListUsersForAdmin(ctx, p); return e }},
		{"reset password write", "UPDATE users SET password_hash", func(p *db.Pool) error { _, e := ResetPassword(ctx, p, member.ID); return e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook := &authQueryHook{match: tc.query, cancel: true}
			fault := authHookPool(t, p, hook)
			if err := tc.call(fault); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context cancellation", err)
			}
		})
	}
	// A failed invite increment must roll back the newly inserted user too.
	var exists bool
	if err := p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username='newmember')`).Scan(&exists); err != nil || exists {
		t.Fatalf("registration rollback: exists=%v err=%v", exists, err)
	}
}

func TestAuthListRejectsIncompatibleDatabaseColumnTypes(t *testing.T) {
	p, _, _ := authFixture(t)
	ctx := context.Background()
	// A mismatched schema can return a UUID-looking column with invalid data.
	// Shadow only this connection's search path so the fixture never alters
	// application tables or relies on invalid records bypassing constraints.
	_, err := p.Exec(ctx, `CREATE SCHEMA auth_scan_fixture;
		CREATE VIEW auth_scan_fixture.invites AS SELECT
		'invalid-uuid'::text AS id, 'testcode'::text AS code, NULL::int AS max_uses,
		0::int AS uses_count, NULL::timestamptz AS expires_at, NOW() AS created_at,
		NULL::uuid AS channel_id, 'invalid-uuid'::text AS created_by;
		CREATE VIEW auth_scan_fixture.users AS SELECT
		'invalid-uuid'::text AS id, 'testuser'::text AS username, 'testuser'::text AS display_name,
		''::text AS bio, 'user'::text AS role, NULL::text AS avatar_s3_key, ''::text AS status_text, NULL::timestamptz AS status_expires_at,
		'online'::text AS presence, ''::text AS locale, NOW() AS created_at,
		NULL::timestamptz AS disabled_at, NULL::timestamptz AS banned_at, NULL::timestamptz AS banned_until, NULL::timestamptz AS timeout_until, NULL::timestamptz AS last_seen_at, FALSE AS is_bot`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, `DROP SCHEMA auth_scan_fixture CASCADE`); err != nil {
			t.Error(err)
		}
	})
	config := p.Config().Copy()
	config.ConnConfig.RuntimeParams["search_path"] = "auth_scan_fixture,public"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fault := &db.Pool{Pool: pool}
	if rows, err := ListInvites(ctx, fault); rows != nil || err == nil || !strings.Contains(err.Error(), "invalid UUID") {
		t.Fatalf("incompatible invite scan = %v, %v", rows, err)
	}
	if rows, err := ListUsersForAdmin(ctx, fault); rows != nil || err == nil || !strings.Contains(err.Error(), "invalid UUID") {
		t.Fatalf("incompatible user scan = %v, %v", rows, err)
	}
}
