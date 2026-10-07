package db

import (
	"strings"
	"testing"
)

func TestVerifyChecksums(t *testing.T) {
	files := map[string]string{"0001_a.sql": Checksum([]byte("a")), "0002_b.sql": Checksum([]byte("b"))}

	backfill, err := verifyChecksums(map[string]string{"0001_a.sql": "", "0002_b.sql": files["0002_b.sql"]}, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(backfill) != 1 || backfill[0] != "0001_a.sql" {
		t.Fatalf("backfill = %v, want [0001_a.sql]", backfill)
	}

	_, err = verifyChecksums(map[string]string{"0001_a.sql": Checksum([]byte("edited"))}, files)
	if err == nil || !strings.Contains(err.Error(), "0001_a.sql") {
		t.Fatalf("edited migration not detected: %v", err)
	}

	// An applied migration that no longer ships is not an error.
	if _, err := verifyChecksums(map[string]string{"0000_gone.sql": "x"}, files); err != nil {
		t.Fatal(err)
	}

	// One newer than every shipped file means a newer version migrated the
	// database: an older binary must refuse it.
	_, err = verifyChecksums(map[string]string{"0001_a.sql": files["0001_a.sql"], "0003_new.sql": "x"}, files)
	if err == nil || !strings.Contains(err.Error(), "0003_new.sql") || !strings.Contains(err.Error(), "newer version") {
		t.Fatalf("schema from a newer version not detected: %v", err)
	}
}

func TestChecksumIgnoresLineEndings(t *testing.T) {
	if Checksum([]byte("SELECT 1;\r\nSELECT 2;\r\n")) != Checksum([]byte("SELECT 1;\nSELECT 2;\n")) {
		t.Fatal("CRLF checkout must not change the checksum")
	}
	if Checksum([]byte("SELECT 1;")) == Checksum([]byte("SELECT 2;")) {
		t.Fatal("different content, same checksum")
	}
}

func TestEmbeddedMigrationsAreNumbered(t *testing.T) {
	files, err := MigrationFiles()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, f := range files {
		num, _, ok := strings.Cut(f, "_")
		if !ok || len(num) != 4 {
			t.Errorf("%s: want NNNN_<name>.sql", f)
		}
		if prev, dup := seen[num]; dup {
			t.Errorf("%s and %s share the number %s", prev, f, num)
		}
		seen[num] = f
	}
}
