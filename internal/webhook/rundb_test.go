package webhook

import (
	"context"
	"testing"
	"time"
)

func TestRunWithoutDatabaseOnlyWaits(t *testing.T) {
	d := NewDispatcher(nil, nil, "test")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	cancel()
	<-done
	d.Wait(time.Second)
}
