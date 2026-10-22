package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestReset2faCommandNeedsConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := reset2faCommand(context.Background(), []string{"max"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("err = %v", err)
	}
}

func TestReset2faCommandReportsAnUnreachableDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://nobody:none@127.0.0.1:1/none?connect_timeout=1")
	if err := reset2faCommand(context.Background(), []string{"max"}, &bytes.Buffer{}); err == nil {
		t.Fatal("an unreachable database must be an error")
	}
}
