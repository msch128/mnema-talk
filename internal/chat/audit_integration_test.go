//go:build integration

package chat

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
)

func TestAuditSnapshotOfAMissingMessageIsEmpty(t *testing.T) {
	f := newChatFixture(t)
	h := &Handler{DB: f.p, Audit: &audit.Log{DB: f.p}}
	if h.snapshotMessage(context.Background(), uuid.New(), true) != nil {
		t.Fatal("snapshot of a message that does not exist")
	}
	if (&Handler{DB: f.p}).snapshotMessage(context.Background(), f.m, true) != nil {
		t.Fatal("snapshot without an audit log")
	}
	// An edit or delete without a snapshot is simply not described further.
	h.auditEdit(context.Background(), f.m, nil, "x")
	h.auditDelete(context.Background(), f.u, f.m, nil)
}
