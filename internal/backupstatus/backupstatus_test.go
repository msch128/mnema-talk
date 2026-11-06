package backupstatus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

// written is what scripts/backup.sh writes, at the given time and result.
func written(at, result string) string {
	restore := `{"result":"` + result + `","at":"` + at + `"}`
	if result == RestoreSkipped {
		restore = `{"result":"skipped"}`
	}
	return `{"format":1,"last_backup_at":"` + at + `","size_bytes":123456,"restore_test":` + restore + "}\n"
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "status.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadAFreshBackupWithAPassedRestoreTest(t *testing.T) {
	r := Read(writeFile(t, written("2026-11-06T02:30:02Z", RestorePassed)), now)
	if r.State != StateOK || r.Stale || r.Warn() || r.SizeBytes != 123456 || r.Restore != RestorePassed {
		t.Fatalf("%+v", r)
	}
	if want := 7*time.Hour + 29*time.Minute + 58*time.Second; r.BackupAge != want || r.RestoreAge != want {
		t.Fatalf("age %v, restore age %v, want %v", r.BackupAge, r.RestoreAge, want)
	}
}

func TestAnOldBackupIsStaleAndAFailedRestoreTestWarns(t *testing.T) {
	old := Read(writeFile(t, written("2026-11-04T21:59:00Z", RestorePassed)), now)
	if !old.Stale || !old.Warn() {
		t.Fatalf("36 h 1 min old: %+v", old)
	}
	edge := Read(writeFile(t, written("2026-11-04T22:00:00Z", RestorePassed)), now)
	if edge.Stale {
		t.Fatalf("exactly 36 h is not stale yet: %+v", edge)
	}
	failed := Read(writeFile(t, written("2026-11-06T02:30:00Z", RestoreFailed)), now)
	if failed.Stale || !failed.Warn() || failed.Restore != RestoreFailed {
		t.Fatalf("failed restore test: %+v", failed)
	}
	skipped := Read(writeFile(t, written("2026-11-06T02:30:00Z", RestoreSkipped)), now)
	if skipped.Warn() || skipped.Restore != RestoreSkipped || skipped.RestoreAge != 0 {
		t.Fatalf("skipped restore test: %+v", skipped)
	}
}

func TestNotConfiguredAndMissingAreTheirOwnStates(t *testing.T) {
	if r := Read("", now); r.State != StateNotConfigured || r.Warn() {
		t.Fatalf("%+v", r)
	}
	if r := Read(filepath.Join(t.TempDir(), "status.json"), now); r.State != StateMissing || r.Warn() {
		t.Fatalf("%+v", r)
	}
}

func TestInvalidFilesAreReportedAsInvalid(t *testing.T) {
	for name, content := range map[string]string{
		"not json":            "backup ok",
		"empty":               "",
		"other format":        strings.Replace(written("2026-11-06T02:30:00Z", RestorePassed), `"format":1`, `"format":2`, 1),
		"no backup time":      `{"format":1,"size_bytes":1,"restore_test":{"result":"skipped"}}`,
		"negative size":       strings.Replace(written("2026-11-06T02:30:00Z", RestorePassed), "123456", "-1", 1),
		"far future":          written("2026-11-07T02:30:00Z", RestorePassed),
		"unknown result":      written("2026-11-06T02:30:00Z", "maybe"),
		"passed without time": `{"format":1,"last_backup_at":"2026-11-06T02:30:00Z","size_bytes":1,"restore_test":{"result":"passed"}}`,
		"too large":           written("2026-11-06T02:30:00Z", RestorePassed) + strings.Repeat(" ", maxFileBytes),
	} {
		t.Run(name, func(t *testing.T) {
			if r := Read(writeFile(t, content), now); r.State != StateInvalid || r.Warn() {
				t.Fatalf("%+v", r)
			}
		})
	}
	if r := Read(t.TempDir(), now); r.State != StateInvalid {
		t.Fatalf("a directory: %+v", r)
	}
}

func TestASmallClockSkewCountsAsJustNow(t *testing.T) {
	r := Read(writeFile(t, written("2026-11-06T10:05:00Z", RestorePassed)), now)
	if r.State != StateOK || r.BackupAge != 0 || r.RestoreAge != 0 {
		t.Fatalf("%+v", r)
	}
}
