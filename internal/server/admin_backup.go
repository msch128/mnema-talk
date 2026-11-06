package server

import (
	"time"

	"github.com/msch128/mnema-talk/internal/backupstatus"
)

// backupHint is the operations guide section on backups.
const backupHint = "doc/operations.md#backups-and-restore"

// BackupStatus reports the last backup and its restore test. The app runs
// isolated in containers, so backups run on the host; scripts/backup.sh
// writes a status file the server reads (BACKUP_STATUS_FILE).
type BackupStatus struct {
	// State, Warn, Stale, StaleAfterHours and RestoreTest are optional in the
	// contract: a server before the backup status sends only the deprecated ages.
	// State is ok (a backup was reported), not_configured (BACKUP_STATUS_FILE
	// is empty), missing (no backup has written the file yet) or invalid.
	State string `json:"state" enums:"ok,not_configured,missing,invalid" binding:"optional"`
	// Warn is true when the backup is stale or its restore test failed.
	Warn bool `json:"warn" binding:"optional"`
	// Stale is true when the last backup is older than StaleAfterHours.
	Stale           bool `json:"stale" binding:"optional"`
	StaleAfterHours int  `json:"stale_after_hours" example:"36" binding:"optional"`
	// LastBackupAt, LastBackupAgeSeconds and SizeBytes are set with state ok.
	LastBackupAt         *time.Time `json:"last_backup_at,omitempty" binding:"optional"`
	LastBackupAgeSeconds *int64     `json:"last_backup_age_seconds,omitempty" binding:"optional"`
	SizeBytes            *int64     `json:"size_bytes,omitempty" binding:"optional"`
	// RestoreTest is passed, failed or skipped (RESTORE_TEST=0) with state
	// ok, and none otherwise; RestoreTestAgeSeconds is set for passed and failed.
	RestoreTest           string `json:"restore_test" enums:"passed,failed,skipped,none" binding:"optional"`
	RestoreTestAgeSeconds *int64 `json:"restore_test_age_seconds,omitempty" binding:"optional"`
	// LastBackupAge and LastRestoreTestAge are the ages as Go durations
	// ("7h29m"), or the state when there is none. Deprecated: use the fields above.
	LastBackupAge      string `json:"last_backup_age" example:"7h29m"`
	LastRestoreTestAge string `json:"last_restore_test_age" example:"7h29m"`
	Hint               string `json:"hint" example:"doc/operations.md#backups-and-restore"`
}

func (h *systemHandler) operatorBackupStatus(now time.Time) BackupStatus {
	path := ""
	if h.cfg != nil {
		path = h.cfg.BackupStatusFile
	}
	return backupStatusOf(backupstatus.Read(path, now))
}

func backupStatusOf(r backupstatus.Report) BackupStatus {
	st := BackupStatus{
		State: r.State, Warn: r.Warn(), Stale: r.Stale, StaleAfterHours: int(backupstatus.StaleAfter.Hours()),
		RestoreTest: "none", LastBackupAge: r.State, LastRestoreTestAge: r.State, Hint: backupHint,
	}
	if r.State != backupstatus.StateOK {
		return st
	}
	at, age, size := r.BackupAt.UTC(), int64(r.BackupAge.Seconds()), r.SizeBytes
	st.LastBackupAt, st.LastBackupAgeSeconds, st.SizeBytes = &at, &age, &size
	st.LastBackupAge = r.BackupAge.Truncate(time.Minute).String()
	st.RestoreTest, st.LastRestoreTestAge = r.Restore, r.Restore
	if r.Restore != backupstatus.RestoreSkipped {
		restoreAge := int64(r.RestoreAge.Seconds())
		st.RestoreTestAgeSeconds = &restoreAge
		st.LastRestoreTestAge = r.RestoreAge.Truncate(time.Minute).String()
	}
	return st
}
