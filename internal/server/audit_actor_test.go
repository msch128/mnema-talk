package server

import (
	"context"
	"testing"
)

func TestAuditActorNeedsASignedInUser(t *testing.T) {
	if _, _, ok := auditActor(context.Background()); ok {
		t.Fatal("an actor without a session")
	}
}
