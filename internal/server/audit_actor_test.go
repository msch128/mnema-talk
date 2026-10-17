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

func TestAuditPrivilegedNeedsASignedInUser(t *testing.T) {
	if ok, err := auditPrivileged(nil)(context.Background()); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
}
