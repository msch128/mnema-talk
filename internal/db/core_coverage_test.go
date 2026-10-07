package db

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func TestConnectRejectsMalformedConfigAndCanceledStartup(t *testing.T) {
	if pool, err := Connect(context.Background(), "postgres://%zz"); pool != nil || err == nil || !strings.Contains(err.Error(), "parse database config") {
		t.Fatalf("malformed connection accepted: pool=%v err=%v", pool, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool, err := Connect(ctx, "postgres://testonly:testonly@127.0.0.1:1/fixture?sslmode=disable&connect_timeout=1")
	if pool != nil || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "ping database") {
		t.Fatalf("canceled startup: pool=%v err=%v", pool, err)
	}
}

type unreadableMigrationDirectory struct{}

func (unreadableMigrationDirectory) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}

func TestMigrationInventoryPropagatesFilesystemPermissionFailure(t *testing.T) {
	files, err := listMigrations(unreadableMigrationDirectory{})
	if files != nil || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("unreadable migration directory: files=%v err=%v", files, err)
	}
}
