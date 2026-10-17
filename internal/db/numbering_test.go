package db

import (
	"io/fs"
	"strings"
	"testing"
)

// Two pull requests merged on the same day both took 0023. Migrations are
// keyed by filename, so both run, in order; renaming one after release would
// make it run twice. This keeps further numbers unique.
var sharedNumbers = map[string]bool{"0023": true}

func TestMigrationNumbersAreUnique(t *testing.T) {
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		number, _, ok := strings.Cut(base, "_")
		if !ok || len(number) != 4 {
			t.Errorf("%s: want NNNN_name.sql", base)
			continue
		}
		if other, dup := seen[number]; dup && !sharedNumbers[number] {
			t.Errorf("%s and %s share the number %s", other, base, number)
		}
		seen[number] = base
	}
}
