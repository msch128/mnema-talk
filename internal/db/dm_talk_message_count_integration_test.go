//go:build integration

package db

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"
)

// The message total of a profile is public. What was said in the call of a
// conversation is as private as the conversation, so it must not count, no
// matter whether it was written before or after the migration, and deleting
// the call must not take anything off the total again.
func TestMessagesInAConversationsCallDoNotCountPublicly(t *testing.T) {
	p := scratchPool(t)
	ctx := context.Background()
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	files, err := listMigrations(sub)
	if err != nil {
		t.Fatal(err)
	}
	before := fstest.MapFS{}
	for _, name := range files {
		if name >= "0098" {
			break
		}
		b, err := fs.ReadFile(sub, name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = &fstest.MapFile{Data: b}
	}
	if err := p.MigrateFS(ctx, before); err != nil {
		t.Fatal(err)
	}
	var user, quiet, text, dm, talk string
	q := func(dst *string, sql string, args ...any) {
		t.Helper()
		if err := p.QueryRow(ctx, sql, args...).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	q(&user, `INSERT INTO users (username, display_name, password_hash) VALUES ('talker', 'T', 'x') RETURNING id`)
	q(&quiet, `INSERT INTO users (username, display_name, password_hash) VALUES ('quiet', 'Q', 'x') RETURNING id`)
	q(&text, `INSERT INTO channels (name, type) VALUES ('general', 'text') RETURNING id`)
	q(&dm, `INSERT INTO channels (name, type) VALUES ('dm', 'text') RETURNING id`)
	q(&talk, `INSERT INTO channels (name, type, is_temp, dm_id) VALUES ('call', 'voice', TRUE, $1) RETURNING id`, dm)
	send := func(ch string, inDM bool) {
		t.Helper()
		if _, err := p.Exec(ctx, `INSERT INTO messages (channel_id, user_id, content, in_dm) VALUES ($1, $2, 'hi', $3)`, ch, user, inDM); err != nil {
			t.Fatal(err)
		}
	}
	count := func(id string) int64 {
		t.Helper()
		var n int64
		if err := p.QueryRow(ctx, `SELECT message_count FROM users WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	send(text, false)
	send(text, false)
	send(dm, true)
	send(talk, false) // counted by the old trigger: this is the leak
	send(talk, false)
	send(talk, false)
	if got := count(user); got != 5 {
		t.Fatalf("setup: old total = %d, want 5", got)
	}

	if err := p.MigrateFS(ctx, sub); err != nil {
		t.Fatal(err)
	}
	if got := count(user); got != 2 {
		t.Errorf("total after the migration = %d, want 2 (the call's messages left out)", got)
	}
	if got := count(quiet); got != 0 {
		t.Errorf("a member without messages has a total of %d", got)
	}

	send(talk, false)
	send(text, false)
	if got := count(user); got != 3 {
		t.Errorf("total after one more message in the call and one in a channel = %d, want 3", got)
	}

	if _, err := p.Exec(ctx, `DELETE FROM channels WHERE id = $1`, dm); err != nil {
		t.Fatal(err)
	}
	if got := count(user); got != 3 {
		t.Errorf("deleting the conversation (and its call) changed the total to %d, want 3", got)
	}
	if _, err := p.Exec(ctx, `DELETE FROM messages WHERE channel_id = $1`, text); err != nil {
		t.Fatal(err)
	}
	if got := count(user); got != 0 {
		t.Errorf("total after deleting the channel's messages = %d, want 0", got)
	}
}
