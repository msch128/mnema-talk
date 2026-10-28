package later

import (
	"testing"
	"time"
)

func TestDeliveredRemindersArePrunedOncePerInterval(t *testing.T) {
	c := &pruneClock{}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if !c.due(now) {
		t.Fatal("the first sweep must prune")
	}
	if c.due(now.Add(sweepEvery)) {
		t.Error("the next sweep must not prune again")
	}
	if c.due(now.Add(pruneEvery - time.Second)) {
		t.Error("pruning before the interval passed")
	}
	if !c.due(now.Add(pruneEvery)) {
		t.Error("pruning is due once the interval passed")
	}
}
