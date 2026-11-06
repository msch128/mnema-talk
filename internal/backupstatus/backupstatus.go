// Package backupstatus reads the status file scripts/backup.sh writes after
// each successful backup: when it ran, how big it is and whether the restore
// test passed. Backups run on the host, so this file is all the server learns
// about them; it is mounted read-only (BACKUP_STATUS_FILE).
package backupstatus

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"time"
)

// StaleAfter is the backup age from which the Status tab warns: a nightly
// backup that missed one night.
const StaleAfter = 36 * time.Hour

// Format is the version of the file scripts/backup.sh writes.
const Format = 1

// maxFileBytes bounds the read; the real file is about 200 bytes.
const maxFileBytes = 64 << 10

// futureSkew is how far in the future a timestamp may be (host and container
// clocks) before the file counts as invalid.
const futureSkew = 10 * time.Minute

// States of a report.
const (
	// StateOK: a backup was reported.
	StateOK = "ok"
	// StateNotConfigured: BACKUP_STATUS_FILE is empty.
	StateNotConfigured = "not_configured"
	// StateMissing: configured, but no backup has written the file yet.
	StateMissing = "missing"
	// StateInvalid: the file cannot be read or is not a status file.
	StateInvalid = "invalid"
)

// Restore test results.
const (
	RestorePassed  = "passed"
	RestoreFailed  = "failed"
	RestoreSkipped = "skipped"
)

// file is the JSON scripts/backup.sh writes.
type file struct {
	Format       int       `json:"format"`
	LastBackupAt time.Time `json:"last_backup_at"`
	SizeBytes    int64     `json:"size_bytes"`
	RestoreTest  struct {
		Result string     `json:"result"`
		At     *time.Time `json:"at"`
	} `json:"restore_test"`
}

// Report is what the Status tab shows.
type Report struct {
	State string
	// Stale is true when the last backup is older than StaleAfter.
	Stale bool
	// BackupAt, BackupAge and SizeBytes describe the last backup (StateOK).
	BackupAt  time.Time
	BackupAge time.Duration
	SizeBytes int64
	// Restore is the restore test result; RestoreAge its age (zero when skipped).
	Restore    string
	RestoreAge time.Duration
}

// Warn is true when the operator should look: the backup is stale or the
// restore test failed.
func (r Report) Warn() bool {
	return r.State == StateOK && (r.Stale || r.Restore == RestoreFailed)
}

// Read reports the status file at path as of now.
func Read(path string, now time.Time) Report {
	if path == "" {
		return Report{State: StateNotConfigured}
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Report{State: StateMissing}
	}
	if err != nil {
		return Report{State: StateInvalid}
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil || len(data) > maxFileBytes {
		return Report{State: StateInvalid}
	}
	r, err := Parse(data, now)
	if err != nil {
		return Report{State: StateInvalid}
	}
	return r
}

var errInvalid = errors.New("backupstatus: not a valid status file")

// Parse reads the file's content as of now.
func Parse(data []byte, now time.Time) (Report, error) {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return Report{}, err
	}
	if f.Format != Format || f.LastBackupAt.IsZero() || f.SizeBytes < 0 || f.LastBackupAt.After(now.Add(futureSkew)) {
		return Report{}, errInvalid
	}
	r := Report{State: StateOK, BackupAt: f.LastBackupAt, BackupAge: max(0, now.Sub(f.LastBackupAt)), SizeBytes: f.SizeBytes}
	r.Stale = r.BackupAge > StaleAfter
	switch f.RestoreTest.Result {
	case RestorePassed, RestoreFailed:
		if f.RestoreTest.At == nil || f.RestoreTest.At.After(now.Add(futureSkew)) {
			return Report{}, errInvalid
		}
		r.Restore, r.RestoreAge = f.RestoreTest.Result, max(0, now.Sub(*f.RestoreTest.At))
	case RestoreSkipped:
		r.Restore = RestoreSkipped
	default:
		return Report{}, errInvalid
	}
	return r, nil
}
