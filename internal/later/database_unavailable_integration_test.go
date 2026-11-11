//go:build integration

package later

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
)

// closedPool is a pool whose every statement fails, as a database that went away does.
func closedPool(t *testing.T, p *db.Pool) *db.Pool {
	t.Helper()
	pool, err := db.Connect(context.Background(), p.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	return pool
}

func TestADatabaseThatWentAwayFailsEveryCallWithoutPanicking(t *testing.T) {
	f := newFixture(t)
	s := f.service(closedPool(t, f.p))
	ctx := context.Background()
	sub := perm.Subject{ID: f.user}

	if _, err := s.AddReminder(ctx, sub, uuid.New(), time.Now().Add(time.Hour)); err == nil {
		t.Error("AddReminder succeeded")
	}
	if _, err := s.ListReminders(ctx, sub); err == nil {
		t.Error("ListReminders succeeded")
	}
	if _, err := s.RemoveReminder(ctx, f.user, uuid.New()); err == nil {
		t.Error("RemoveReminder succeeded")
	}
	if _, err := s.AddScheduled(ctx, NewScheduled{User: f.user, Channel: f.channel, Content: "x", SendAt: time.Now().Add(time.Hour)}); err == nil {
		t.Error("AddScheduled succeeded")
	}
	if _, err := s.ListScheduled(ctx, sub); err == nil {
		t.Error("ListScheduled succeeded")
	}
	text := "x"
	if _, err := s.UpdateScheduled(ctx, f.user, uuid.New(), ScheduledPatch{Content: &text}); err == nil {
		t.Error("UpdateScheduled succeeded")
	}
	if err := s.RemoveScheduled(ctx, f.user, uuid.New()); err == nil {
		t.Error("RemoveScheduled succeeded")
	}
	if _, _, err := s.subjectOf(ctx, f.user); err == nil {
		t.Error("subjectOf succeeded")
	}
	if _, err := s.loadRef(ctx, uuid.New()); err == nil {
		t.Error("loadRef succeeded")
	}
	// The sweeps log and carry on.
	s.Sweep(ctx)
	s.fail(ctx, dueScheduled{id: uuid.New(), user: f.user, channel: f.channel}, ReasonRefused)
	s.sendOne(ctx, dueScheduled{id: uuid.New(), user: f.user, channel: f.channel, content: "x"})
}
