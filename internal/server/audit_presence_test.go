package server

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// With the writer gone the queue fills up, and further events are dropped
// instead of blocking the hub.
func TestAuditPresenceNeverBlocksTheHub(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	record := auditPresence(ctx, nil, nil)
	writePresence(ctx, nil, nil, presenceEvent{})
	for i := 0; i < 3000; i++ {
		record(uuid.New(), uuid.New(), i%2 == 0)
	}
}
