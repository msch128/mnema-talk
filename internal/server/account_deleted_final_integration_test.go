//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// A deleted account is final: no administrator action, no code path and not
// even a stray UPDATE brings it back.
func TestADeletedAccountCannotBeRevived(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	id := alice.user.ID.String()
	if res := a.deleteAccount(alice, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d", res.status)
	}

	base := "/api/admin/users/" + id
	for name, res := range map[string]response{
		"enable":        admin.post(base+"/enable", nil),
		"disable":       admin.post(base+"/disable", nil),
		"set password":  admin.post(base+"/password", map[string]string{"password": "an-admin-chosen-password"}),
		"temp password": admin.post(base+"/password-reset", nil),
		"revoke":        admin.post(base+"/sessions/revoke", nil),
		"2fa reset":     admin.post(base+"/two-factor/reset", nil),
		"timeout":       admin.post(base+"/timeout", map[string]any{"duration_seconds": 600, "reason": "x"}),
		"ban":           admin.post(base+"/ban", map[string]any{"reason": "x"}),
		"unban":         admin.do(http.MethodDelete, base+"/ban", nil, nil),
		"roles":         admin.put(base+"/roles", map[string]any{"role_ids": []string{}}),
	} {
		if res.status != http.StatusNotFound {
			t.Errorf("%s: want 404 like a missing account, got %d %s", name, res.status, res.body)
		}
	}

	// The tombstone is untouched and cannot sign in.
	var disabled, deleted bool
	var hash string
	if err := a.db.QueryRow(context.Background(), `SELECT disabled_at IS NOT NULL, deleted_at IS NOT NULL, password_hash FROM users WHERE id = $1`, id).Scan(&disabled, &deleted, &hash); err != nil {
		t.Fatal(err)
	}
	if !disabled || !deleted || strings.HasPrefix(hash, "$2") {
		t.Fatalf("tombstone changed: disabled=%v deleted=%v hash=%q", disabled, deleted, hash)
	}
	if got := a.password(a.anon(), "alice", memberPassword); got.status != http.StatusUnauthorized {
		t.Fatalf("sign-in: %d", got.status)
	}
}

func TestTheDatabaseRefusesToReviveATombstone(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	id := alice.user.ID
	a.deleteAccount(alice, memberPassword, "")
	ctx := context.Background()
	for name, stmt := range map[string]string{
		"undelete":  `UPDATE users SET deleted_at = NULL WHERE id = $1`,
		"enable":    `UPDATE users SET disabled_at = NULL WHERE id = $1`,
		"password":  `UPDATE users SET password_hash = '$2a$10$abcdefghijklmnopqrstuuabcdefghijklmnopqrstuvwxyz01234' WHERE id = $1`,
		"rename":    `UPDATE users SET username = 'alice' WHERE id = $1`,
		"e-mail":    `UPDATE users SET email = 'alice@example.org' WHERE id = $1`,
		"avatar":    `UPDATE users SET avatar_s3_key = 'x' WHERE id = $1`,
		"owner":     `UPDATE users SET role = 'admin' WHERE id = $1`,
		"ban state": `UPDATE users SET banned_at = NOW() WHERE id = $1`,
	} {
		if _, err := a.db.Exec(ctx, stmt, id); err == nil {
			t.Errorf("%s: the database must refuse this", name)
		}
	}
	// Counters still move: deleting a channel removes the tombstone's messages.
	if _, err := a.db.Exec(ctx, `UPDATE users SET voice_seconds = voice_seconds + 1 WHERE id = $1`, id); err != nil {
		t.Errorf("counters must stay writable: %v", err)
	}
}
