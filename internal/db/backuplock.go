package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// BackupLockID is the advisory lock an online backup (scripts/backup.sh with
// BACKUP_MODE=online) holds exclusively from before it freezes the object
// storage until its database snapshot is written. The jobs that delete
// stored objects take it shared, so they never run during a backup and a
// backup waits for the one that is running. scripts/backup-online.sh carries
// the same number.
const BackupLockID int64 = 120320915497314 // 0x6d6e656d6162, "mnemab"

// backupPoll is how often a waiting job looks whether the backup has ended.
var backupPoll = 30 * time.Second

// TryOutsideBackup runs job unless an online backup is running. It reports
// whether job ran; err is job's error or the failure to ask the database.
func (p *Pool) TryOutsideBackup(ctx context.Context, job func() error) (ran bool, err error) {
	conn, err := p.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("backup lock: %w", err)
	}
	defer conn.Release()
	var free bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock_shared($1)`, BackupLockID).Scan(&free); err != nil {
		return false, fmt.Errorf("backup lock: %w", err)
	}
	if !free {
		return false, nil
	}
	defer func() {
		// The lock belongs to the session: a connection that cannot give it
		// back must not return to the pool with it.
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock_shared($1)`, BackupLockID); err != nil {
			slog.Warn("backup lock not released; closing its connection", "err", err)
			_ = conn.Conn().Close(context.WithoutCancel(ctx))
		}
	}()
	return true, job()
}

// OutsideBackup runs job once no online backup is running, waiting for one
// that is. It returns ctx's error when ctx ends first.
func (p *Pool) OutsideBackup(ctx context.Context, job func() error) error {
	waiting := false
	for {
		ran, err := p.TryOutsideBackup(ctx, job)
		if ran || err != nil {
			return err
		}
		if !waiting {
			waiting = true
			slog.Info("maintenance job waits for the running backup")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backupPoll):
		}
	}
}
