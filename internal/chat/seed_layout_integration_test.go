//go:build integration

package chat

import (
	"context"
	"testing"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func seedGermanLayout(t *testing.T, p *db.Pool) {
	t.Helper()
	ctx := context.Background()
	testutil.Reset(t, p)
	for _, q := range []string{
		`DELETE FROM channels`, `DELETE FROM categories`,
		`INSERT INTO users (username, display_name, password_hash, role) VALUES ('owner', 'Owner', 'x', 'admin')`,
		`INSERT INTO categories (name, sort_order) VALUES ('Text-Kanäle', 0), ('Voice-Hangouts', 1)`,
		`INSERT INTO channels (category_id, name, type, topic, sort_order)
		 SELECT c.id, v.name, v.type, v.topic, 0 FROM categories c JOIN (VALUES
		   ('Text-Kanäle', 'general', 'text', 'Allgemeine Diskussionen & Chat'),
		   ('Text-Kanäle', 'medien', 'text', 'Bilder, Screenshots & Clips'),
		   ('Voice-Hangouts', 'Lounge', 'voice', 'Offener Sprach-Hangout'),
		   ('Voice-Hangouts', 'Gaming 4K', 'voice', 'Source-Quality Screen & Gaming Hangout')) AS v(cat, name, type, topic) ON v.cat = c.name`,
	} {
		if _, err := p.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func names(t *testing.T, p *db.Pool, q string) []string {
	t.Helper()
	rows, err := p.Query(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestAFreshInstallGetsEnglishDefaultNamesOnlyWhenAskedAndUntouched(t *testing.T) {
	p := testutil.DB(t)
	ctx := context.Background()

	seedGermanLayout(t, p)
	if did, err := LocalizeSeedLayout(ctx, p, "de"); err != nil || did {
		t.Fatalf("German keeps the names: %v %v", did, err)
	}
	if got := names(t, p, `SELECT name FROM categories ORDER BY sort_order`); got[0] != "Text-Kanäle" {
		t.Fatalf("categories: %v", got)
	}

	did, err := LocalizeSeedLayout(ctx, p, "en")
	if err != nil || !did {
		t.Fatalf("English renames: %v %v", did, err)
	}
	if got := names(t, p, `SELECT name FROM categories ORDER BY sort_order`); got[0] != "Text channels" || got[1] != "Voice hangouts" {
		t.Errorf("categories: %v", got)
	}
	if got := names(t, p, `SELECT name || ': ' || topic FROM channels WHERE name = 'media'`); len(got) != 1 || got[0] != "media: Pictures, screenshots and clips" {
		t.Errorf("channel: %v", got)
	}
	if did, _ := LocalizeSeedLayout(ctx, p, "en"); did {
		t.Error("renamed twice")
	}
}

func TestExistingDataIsNeverRenamed(t *testing.T) {
	p := testutil.DB(t)
	ctx := context.Background()

	seedGermanLayout(t, p)
	if _, err := p.Exec(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ('member', 'Member', 'x')`); err != nil {
		t.Fatal(err)
	}
	if did, err := LocalizeSeedLayout(ctx, p, "en"); err != nil || did {
		t.Fatalf("a second account: %v %v", did, err)
	}

	seedGermanLayout(t, p)
	if _, err := p.Exec(ctx, `INSERT INTO messages (channel_id, user_id, content) SELECT c.id, u.id, 'hi' FROM channels c, users u LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	if did, err := LocalizeSeedLayout(ctx, p, "en"); err != nil || did {
		t.Fatalf("a message exists: %v %v", did, err)
	}

	seedGermanLayout(t, p)
	if _, err := p.Exec(ctx, `UPDATE channels SET name = 'allgemein' WHERE name = 'general'`); err != nil {
		t.Fatal(err)
	}
	if did, err := LocalizeSeedLayout(ctx, p, "en"); err != nil || did {
		t.Fatalf("an edited layout: %v %v", did, err)
	}
	if got := names(t, p, `SELECT name FROM categories ORDER BY sort_order`); got[0] != "Text-Kanäle" {
		t.Errorf("categories: %v", got)
	}
}
