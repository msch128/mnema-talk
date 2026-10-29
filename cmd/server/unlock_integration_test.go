//go:build integration

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestUnlockCommandQueuesAndReports(t *testing.T) {
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	startupEnv(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO users (username, display_name, password_hash, role) VALUES ('Admin', 'Admin', 'x', 'admin')`); err != nil {
		t.Fatal(err)
	}
	old := unlockWait
	unlockWait = 700 * time.Millisecond
	t.Cleanup(func() { unlockWait = old })

	// Unknown user.
	if err := unlockCommand(ctx, []string{"nobody"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "no user named") {
		t.Fatalf("unknown user: %v", err)
	}
	// Nobody applies the request: it stays queued.
	var out bytes.Buffer
	if err := unlockCommand(ctx, []string{"admin"}, &out); err != nil || !strings.Contains(out.String(), "queued") {
		t.Fatalf("queued: %v %q", err, out.String())
	}
	// A server applies (deletes) the request while the command waits.
	if _, err := pool.Exec(ctx, `DELETE FROM login_unlock_requests`); err != nil {
		t.Fatal(err)
	}
	unlockWait = 5 * time.Second
	go func() {
		time.Sleep(400 * time.Millisecond)
		_, _ = pool.Exec(ctx, `DELETE FROM login_unlock_requests`)
	}()
	out.Reset()
	if err := unlockCommand(ctx, []string{"--all"}, &out); err != nil || !strings.Contains(out.String(), "all accounts") {
		t.Fatalf("all: %v %q", err, out.String())
	}
	go func() {
		time.Sleep(400 * time.Millisecond)
		_, _ = pool.Exec(ctx, `DELETE FROM login_unlock_requests`)
	}()
	out.Reset()
	if err := unlockCommand(ctx, []string{"Admin"}, &out); err != nil || !strings.Contains(out.String(), "cleared for Admin") {
		t.Fatalf("one: %v %q", err, out.String())
	}
}
