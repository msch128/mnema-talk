//go:build integration

package later

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

type fixture struct {
	p       *db.Pool
	user    uuid.UUID
	channel uuid.UUID
	events  *events.Recorder
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	var id uuid.UUID
	if err := p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ('writer', 'Writer', 'test-only-unused') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	c, err := chat.CreateChannel(ctx, p, nil, "general", chat.ChannelTypeText, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{p: p, user: id, channel: c.ID, events: &events.Recorder{}}
}

func (f fixture) service(p *db.Pool) *Service {
	return NewService(Service{DB: p, Perms: perm.NewService(p), Events: f.events})
}

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

func TestAMessageEditedWhileItIsBeingSentIsNotSentInItsOldVersion(t *testing.T) {
	f := newFixture(t)
	s := f.service(f.p)
	ctx := context.Background()
	e, err := s.AddScheduled(ctx, NewScheduled{User: f.user, Channel: f.channel, Content: "old", SendAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	stale := dueScheduled{id: e.ID, user: f.user, channel: f.channel, content: "old", updatedAt: e.UpdatedAt.Add(-time.Second)}
	s.sendOne(ctx, stale)
	s.fail(ctx, stale, ReasonRefused) // also a refusal of the old version changes nothing

	var n, state int
	if err := f.p.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM messages), (SELECT COUNT(*) FROM scheduled_messages WHERE state = 'pending')`).Scan(&n, &state); err != nil {
		t.Fatal(err)
	}
	if n != 0 || state != 1 {
		t.Fatalf("messages %d, pending %d: the superseded version was sent or failed", n, state)
	}
}

func TestAnEndedContextStopsTheSendLoop(t *testing.T) {
	f := newFixture(t)
	s := f.service(f.p)
	if _, err := f.p.Exec(context.Background(), `INSERT INTO scheduled_messages (user_id, channel_id, content, send_at) VALUES ($1, $2, 'x', NOW() - INTERVAL '1 minute')`, f.user, f.channel); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.sendScheduled(ctx)
	var n int
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM scheduled_messages`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows %d, %v", n, err)
	}
}
