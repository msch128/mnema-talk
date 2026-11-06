package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
)

func TestOperatorBackupStatusReadsTheConfiguredFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	h := &systemHandler{cfg: &config.Config{BackupStatusFile: path}}
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	if st := h.operatorBackupStatus(now); st.State != "missing" || st.LastBackupAge != "missing" || st.RestoreTest != "none" {
		t.Fatalf("before the first backup: %+v", st)
	}
	content := `{"format":1,"last_backup_at":"2026-11-06T03:00:00Z","size_bytes":5000,"restore_test":{"result":"passed","at":"2026-11-06T03:02:00Z"}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	st := h.operatorBackupStatus(now)
	if st.State != "ok" || st.Warn || st.Stale || st.StaleAfterHours != 36 || *st.SizeBytes != 5000 {
		t.Fatalf("%+v", st)
	}
	if *st.LastBackupAgeSeconds != 7*3600 || *st.RestoreTestAgeSeconds != 7*3600-120 || st.RestoreTest != "passed" {
		t.Fatalf("ages: %+v", st)
	}
	if st.LastBackupAge != "7h0m0s" || st.LastRestoreTestAge != "6h58m0s" || !st.LastBackupAt.Equal(time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)) {
		t.Fatalf("legacy fields: %+v", st)
	}
	if late := h.operatorBackupStatus(now.Add(48 * time.Hour)); !late.Stale || !late.Warn {
		t.Fatalf("two days later: %+v", late)
	}
}

func TestOperatorBackupStatusSkippedRestoreAndNoConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	content := `{"format":1,"last_backup_at":"2026-11-06T03:00:00Z","size_bytes":1,"restore_test":{"result":"skipped"}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &systemHandler{cfg: &config.Config{BackupStatusFile: path}}
	st := h.operatorBackupStatus(time.Date(2026, 11, 6, 4, 0, 0, 0, time.UTC))
	if st.RestoreTest != "skipped" || st.RestoreTestAgeSeconds != nil || st.LastRestoreTestAge != "skipped" || st.Warn {
		t.Fatalf("%+v", st)
	}
	if none := (&systemHandler{}).operatorBackupStatus(time.Now()); none.State != "not_configured" || none.Hint == "" {
		t.Fatalf("%+v", none)
	}
}
