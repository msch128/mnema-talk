//go:build integration

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestReset2faCommandTurnsTheSecondFactorOffAndIsAudited(t *testing.T) {
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	startupEnv(t, pool)
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role, token_version)
		VALUES ('Herzog', 'Herzog', 'x', 'admin', 3) RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_totp (user_id, secret_enc, confirmed_at) VALUES ($1, '\x00', NOW())`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, '\x01')`, id); err != nil {
		t.Fatal(err)
	}

	if err := reset2faCommand(ctx, []string{"nobody"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "no user named") {
		t.Fatalf("unknown user: %v", err)
	}
	var out bytes.Buffer
	if err := reset2faCommand(ctx, []string{"herzog"}, &out); err != nil || !strings.Contains(out.String(), "Two-factor sign-in is off for Herzog") {
		t.Fatalf("reset: %v %q", err, out.String())
	}
	var left, version, audited int
	if err := pool.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM user_totp) + (SELECT COUNT(*) FROM recovery_codes),
		(SELECT token_version FROM users WHERE id = $1),
		(SELECT COUNT(*) FROM audit_log WHERE action = 'user.two_factor_reset')`, id).Scan(&left, &version, &audited); err != nil {
		t.Fatal(err)
	}
	if left != 0 || version != 4 || audited != 1 {
		t.Fatalf("left=%d version=%d audited=%d", left, version, audited)
	}
}

func TestReset2faCommandUsageAndDispatch(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}, {"--all"}} {
		if err := reset2faCommand(context.Background(), args, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "usage: mnema-talk reset-2fa") {
			t.Errorf("args %v: %v", args, err)
		}
	}
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"mnema-talk", "reset-2fa"}
	if err := run(); err == nil || !strings.Contains(err.Error(), "usage: mnema-talk reset-2fa") {
		t.Fatalf("run = %v", err)
	}
}
