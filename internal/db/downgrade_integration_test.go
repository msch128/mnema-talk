//go:build integration

package db

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

// An older binary refuses to start on a database a newer one migrated: it
// would ignore tables (roles, private channels) that restrict access. Going
// forward, and restarting the same version, stay fine, so the update poller
// and the upgrade drill (which only move forward) are not affected.
func TestOlderVersionRefusesAMigratedDatabase(t *testing.T) {
	p := scratchPool(t)
	ctx := context.Background()
	old := fstest.MapFS{
		"0001_a.sql": {Data: []byte(`CREATE TABLE a (x INT);`)},
		"0002_b.sql": {Data: []byte(`CREATE TABLE b (x INT);`)},
	}
	next := fstest.MapFS{
		"0001_a.sql": old["0001_a.sql"],
		"0002_b.sql": old["0002_b.sql"],
		"0003_c.sql": {Data: []byte(`CREATE TABLE c (x INT);`)},
	}
	for name, fsys := range map[string]fstest.MapFS{"old": old, "forward": next, "restart": next} {
		if err := p.MigrateFS(ctx, fsys); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	err := p.MigrateFS(ctx, old)
	if err == nil || !strings.Contains(err.Error(), "0003_c.sql") || !strings.Contains(err.Error(), "newer version") {
		t.Fatalf("an older version must refuse the migrated database: %v", err)
	}
}
