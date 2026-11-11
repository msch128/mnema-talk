//go:build integration

package poll

import (
	"context"
	"testing"

	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestMessagesWithoutAPollHaveNoPollToAnnounceOrShow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id, err := messages.CreateMessage(ctx, f.p, messages.NewMessage{ChannelID: f.channel, UserID: f.user.ID, Content: "just words"})
	if err != nil {
		t.Fatal(err)
	}
	s := f.service(f.p)
	if _, err := s.announce(ctx, f.channel, id); err == nil {
		t.Fatal("announced a poll that does not exist")
	}
	if _, err := s.detail(ctx, id, f.user.ID); err == nil {
		t.Fatal("showed a poll that does not exist")
	}
	if err := Vote(ctx, f.p, id, f.user.ID, nil); err == nil {
		t.Fatal("voted in a poll that does not exist")
	}
	if _, err := loadRef(ctx, f.p, id); err == nil {
		t.Fatal("found a poll that does not exist")
	}
	if changed, err := end(ctx, f.p, id); err != nil || changed {
		t.Fatalf("ending a poll that does not exist: %v, %v", changed, err)
	}
}

func TestAnnounceOrLogSwallowsAFailureAfterTheCommit(t *testing.T) {
	f := newFixture(t)
	m := f.newPoll(t)
	ctx := context.Background()
	rec := &events.Recorder{}
	broken := NewService(Service{DB: drift(t, f.p, brokenPolls), Perms: perm.NewService(f.p), Events: rec})
	broken.announceOrLog(ctx, f.channel, m.ID)
	if n := len(rec.Snapshot()); n != 0 {
		t.Fatalf("a failed announcement broadcast %d events", n)
	}
	good := NewService(Service{DB: f.p, Perms: perm.NewService(f.p), Events: rec})
	good.announceOrLog(ctx, f.channel, m.ID)
	if n := len(rec.Snapshot()); n != 1 {
		t.Fatalf("events after a working announcement = %d, want 1", n)
	}
}
