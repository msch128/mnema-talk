//go:build integration

package db

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// holdBackupLock takes the lock the way an online backup does, on a session
// of its own, and returns the function that ends that session's hold.
func holdBackupLock(t *testing.T, p *Pool) (release func()) {
	t.Helper()
	conn, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_lock($1)`, BackupLockID); err != nil {
		t.Fatal(err)
	}
	done := false
	release = func() {
		if done {
			return
		}
		done = true
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, BackupLockID)
		conn.Release()
	}
	t.Cleanup(release)
	return release
}

func TestAJobDoesNotRunWhileABackupHoldsTheLock(t *testing.T) {
	p := scratchPool(t)
	holdBackupLock(t, p)
	ran, err := p.TryOutsideBackup(context.Background(), func() error {
		t.Error("the job ran during a backup")
		return nil
	})
	if ran || err != nil {
		t.Fatalf("ran=%v err=%v; want the job skipped without an error", ran, err)
	}
}

func TestAWaitingJobRunsOnceTheBackupHasEnded(t *testing.T) {
	p := scratchPool(t)
	old := backupPoll
	backupPoll = 20 * time.Millisecond
	t.Cleanup(func() { backupPoll = old })
	release := holdBackupLock(t, p)
	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- p.OutsideBackup(context.Background(), func() error { close(started); return nil })
	}()
	select {
	case <-started:
		t.Fatal("the job ran during a backup")
	case <-time.After(150 * time.Millisecond):
	}
	release()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job never ran after the backup ended")
	}
}

func TestAWaitingJobGivesUpWhenTheServerStops(t *testing.T) {
	p := scratchPool(t)
	holdBackupLock(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := p.OutsideBackup(ctx, func() error { t.Error("the job ran during a backup"); return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context's", err)
	}
}

func TestABackupWaitsForARunningJobAndGetsTheLockAfterwards(t *testing.T) {
	p := scratchPool(t)
	ctx := context.Background()
	backupSees := func() (free bool) {
		t.Helper()
		if err := p.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, BackupLockID).Scan(&free); err != nil {
			t.Fatal(err)
		}
		return free
	}
	failure := errors.New("job failed")
	ran, err := p.TryOutsideBackup(ctx, func() error {
		if backupSees() {
			t.Error("a backup got the lock while a job was running")
		}
		return failure
	})
	if !ran || !errors.Is(err, failure) {
		t.Fatalf("ran=%v err=%v; want the job's own error", ran, err)
	}
	// Session locks would survive on a pooled connection: every one must be free again.
	var held int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND objid = ($1 & 4294967295)::oid AND classid = ($1 >> 32)::oid`, BackupLockID).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if held != 0 {
		t.Fatalf("%d sessions still hold the backup lock after the job", held)
	}
}

func TestTheBackupScriptUsesTheSameLockAsTheServer(t *testing.T) {
	script, err := os.ReadFile("../../scripts/backup-online.sh")
	if err != nil {
		t.Fatal(err)
	}
	if want := "BACKUP_LOCK_ID=" + strconv.FormatInt(BackupLockID, 10) + "\n"; !strings.Contains(string(script), want) {
		t.Fatalf("scripts/backup-online.sh does not set %q", strings.TrimSpace(want))
	}
}
