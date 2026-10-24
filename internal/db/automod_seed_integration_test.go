//go:build integration

package db_test

import (
	"context"
	"testing"
)

// A fresh or upgraded server starts with the same conservative automod rules
// (doc/upgrade.md): mention spam above 10, links of accounts under 10 minutes,
// a raid at more than 10 joins in 10 seconds; the word list is empty and off.
func TestAutomodDefaultsAreConservative(t *testing.T) {
	p := starterDB(t)
	ctx := context.Background()
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	type row struct {
		enabled bool
		action  string
		config  string
	}
	want := map[string]row{
		"mentions":    {true, "block", `{"max_mentions": 10}`},
		"new_account": {true, "block", `{"restrict": "links", "max_age_seconds": 600}`},
		"raid":        {true, "flag", `{"joins": 10, "window_seconds": 10, "restrict_seconds": 600}`},
		"words":       {false, "block", `{"words": []}`},
		"links":       {false, "block", `{"mode": "block_list", "domains": []}`},
	}
	rows, err := p.Query(ctx, `SELECT kind, enabled, action, config::text, timeout_seconds,
		cardinality(exempt_roles) + cardinality(exempt_channels) FROM automod_rules`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var kind, action, config string
		var enabled bool
		var timeout, exempt int
		if err := rows.Scan(&kind, &enabled, &action, &config, &timeout, &exempt); err != nil {
			t.Fatal(err)
		}
		w, ok := want[kind]
		if !ok || enabled != w.enabled || action != w.action || timeout != 0 || exempt != 0 {
			t.Errorf("%s: enabled=%v action=%s timeout=%d exempt=%d", kind, enabled, action, timeout, exempt)
		}
		var same bool
		if err := p.QueryRow(ctx, `SELECT $1::jsonb = $2::jsonb`, config, w.config).Scan(&same); err != nil || !same {
			t.Errorf("%s: config %s, want %s (%v)", kind, config, w.config, err)
		}
		seen++
	}
	if seen != len(want) {
		t.Fatalf("%d rules seeded, want %d", seen, len(want))
	}
	var raidUntil *string
	if err := p.QueryRow(ctx, `SELECT raid_until::text FROM automod_state`).Scan(&raidUntil); err != nil || raidUntil != nil {
		t.Fatalf("no raid is running on a new server: %v %v", raidUntil, err)
	}
}
