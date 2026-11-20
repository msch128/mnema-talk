//go:build integration

package server

import (
	"context"
	"os"
	"testing"
)

// Migration 0111 cleans the display names stored before names were cleaned
// on the way in: the format characters go, a name with nothing left falls
// back to the username, and joiners inside an emoji sequence stay.
func TestTheMigrationCleansStoredDisplayNames(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	mallory, blank, zoe := a.register(owner, "mallory"), a.register(owner, "blankname"), a.register(owner, "zoe")
	ctx := context.Background()
	for id, name := range map[any]string{mallory.user.ID: "\u202eevil\u200b", blank.user.ID: "\u2066\u2069", zoe.user.ID: "Zoë \U0001f469\u200d\U0001f4bb"} {
		if _, err := a.db.Exec(ctx, `UPDATE users SET display_name = $1 WHERE id = $2`, name, id); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../db/migrations/0111_display_name_format_characters.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(ctx, string(raw)); err != nil {
		t.Fatalf("migration: %v", err)
	}
	for id, want := range map[any]string{mallory.user.ID: "evil", blank.user.ID: "blankname", zoe.user.ID: "Zoë \U0001f469\u200d\U0001f4bb"} {
		var got string
		if err := a.db.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, id).Scan(&got); err != nil || got != want {
			t.Errorf("display name %q (%v), want %q", got, err, want)
		}
	}
}
