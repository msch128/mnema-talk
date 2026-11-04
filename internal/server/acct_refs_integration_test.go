//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
)

// A deleted account stays as a tombstone row, so a reference to it never
// dangles, but it must not keep pointing at the person: other members' ban
// entries, emoji they uploaded, Talks they led, messages they pinned and Discord imports they ran
// all lose the link.
func TestDeletionClearsReferencesOtherRowsHoldToTheAccount(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")
	ctx := context.Background()
	id := alice.user.ID

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	ch := a.createChannel(admin, "general", "text")
	pinned := a.send(a.login("bob", memberPassword), ch, "bob's pinned words")
	exec(`UPDATE users SET banned_at = NOW(), banned_by = $1 WHERE id = $2`, id, bob.user.ID)
	exec(`UPDATE users SET timeout_until = NOW() + interval '1 hour', timeout_by = $1 WHERE id = $2`, id, bob.user.ID)
	exec(`INSERT INTO custom_emoji (name, content_type, s3_key, size_bytes, content_hash, created_by)
		VALUES ('wave', 'image/png', 'emoji/wave.png', 3, 'h', $1)`, id)
	exec(`INSERT INTO channels (name, type, is_temp, leader_id) VALUES ('alice talk', 'voice', TRUE, $1)`, id)
	exec(`UPDATE messages SET is_pinned = TRUE, pinned_at = NOW(), pinned_by = $1 WHERE id = $2`, id, pinned)
	exec(`INSERT INTO discord_imports (template_code, created_by, created_by_name, undone_at, undone_by)
		VALUES ('abc', $1, 'alice', NOW(), $1)`, id)

	if res := a.deleteAccount(alice, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}

	for _, c := range []struct{ what, sql string }{
		{"users.banned_by", `SELECT COUNT(*) FROM users WHERE banned_by = $1`},
		{"messages.pinned_by", `SELECT COUNT(*) FROM messages WHERE pinned_by = $1`},
		{"users.timeout_by", `SELECT COUNT(*) FROM users WHERE timeout_by = $1`},
		{"custom_emoji.created_by", `SELECT COUNT(*) FROM custom_emoji WHERE created_by = $1`},
		{"channels.leader_id", `SELECT COUNT(*) FROM channels WHERE leader_id = $1`},
		{"discord_imports.created_by", `SELECT COUNT(*) FROM discord_imports WHERE created_by = $1`},
		{"discord_imports.undone_by", `SELECT COUNT(*) FROM discord_imports WHERE undone_by = $1`},
	} {
		if n := a.countRows(c.sql, id); n != 0 {
			t.Errorf("%s still points at the deleted account (%d rows)", c.what, n)
		}
	}
	// What they were attached to stays.
	for what, sql := range map[string]string{
		"the ban":    `SELECT COUNT(*) FROM users WHERE id = $1 AND banned_at IS NOT NULL`,
		"the emoji":  `SELECT COUNT(*) FROM custom_emoji WHERE name = 'wave' AND $1::uuid IS NOT NULL`,
		"the Talk":   `SELECT COUNT(*) FROM channels WHERE name = 'alice talk' AND $1::uuid IS NOT NULL`,
		"the import": `SELECT COUNT(*) FROM discord_imports WHERE template_code = 'abc' AND $1::uuid IS NOT NULL`,
	} {
		if a.countRows(sql, bob.user.ID) != 1 {
			t.Errorf("%s was removed with the account", what)
		}
	}
}
