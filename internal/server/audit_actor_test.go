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

func TestAuditViewerNeedsASignedInUser(t *testing.T) {
	v, err := auditViewer(nil)(context.Background())
	if err != nil || v.Privileged || len(v.Channels) != 0 {
		t.Fatalf("%+v %v", v, err)
	}
}
