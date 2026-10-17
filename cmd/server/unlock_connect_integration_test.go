//go:build integration

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestUnlockCommandReportsUnreachableDatabase(t *testing.T) {
	startupEnv(t, testutil.DB(t))
	t.Setenv("DATABASE_URL", "postgres://fixture:fixture@127.0.0.1:1/fixture?sslmode=disable&connect_timeout=1")
	if err := unlockCommand(context.Background(), []string{"--all"}, &bytes.Buffer{}); err == nil {
		t.Fatal("unreachable database was accepted")
	}
}
