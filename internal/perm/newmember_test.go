package perm_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func TestNewMemberWaitsHoldWritingReactingAndSpeakingApart(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t.Cleanup(SetClock(func() time.Time { return now }))

	f := permtest.NewFixture()
	ch := f.Channel(uuid.Nil)
	newbie, vouched, admin, owner, bot, old := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.Owner(owner).Bot(bot)
	f.Assign(admin, f.Role(5, Administrator))
	f.Assign(vouched, f.Role(3, BypassNewMemberLimits))
	for _, u := range []uuid.UUID{newbie, vouched, admin, owner, bot} {
		f.Joined(u, now.Add(-5*time.Minute))
	}
	f.Joined(old, now.Add(-48*time.Hour))
	f.NewMemberRules(10*time.Minute, 30*time.Minute, time.Hour)
	s := f.Snapshot()

	got := s.Channel(Subject{ID: newbie}, ch)
	if !got.Has(ViewChannel) || got.HasAny(SendMessages|AttachFiles|AddReactions|Speak) {
		t.Fatalf("new member holds %b", got)
	}
	if !got.Has(Connect) {
		t.Fatal("a new member may still join a Talk and listen")
	}
	if w := s.NewMemberWaits(newbie); w.Write != 5*time.Minute || w.React != 25*time.Minute || w.Speak != 55*time.Minute {
		t.Fatalf("waits %+v", w)
	}

	for name, u := range map[string]uuid.UUID{"bypass role": vouched, "administrator": admin, "owner": owner, "bot": bot, "old member": old} {
		if w := s.NewMemberWaits(u); w != (NewMemberWaits{}) {
			t.Errorf("%s waits %+v", name, w)
		}
	}
	if !s.Channel(Subject{ID: vouched}, ch).Has(SendMessages | AddReactions | Speak) {
		t.Error("the bypass role did not keep its rights")
	}

	// Each gate opens by itself, without a reload.
	now = now.Add(6 * time.Minute)
	got = s.Channel(Subject{ID: newbie}, ch)
	if !got.Has(SendMessages|AttachFiles) || got.HasAny(AddReactions|Speak) {
		t.Fatalf("after writing opened: %b", got)
	}
	now = now.Add(2 * time.Hour)
	if !s.Channel(Subject{ID: newbie}, ch).Has(SendMessages | AddReactions | Speak) {
		t.Fatal("everything should be open")
	}
}

func TestNewMemberRefusalNamesTheWaitAndAnUnknownMemberCountsAsNew(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t.Cleanup(SetClock(func() time.Time { return now }))
	f := permtest.NewFixture()
	ch := f.Channel(uuid.Nil)
	unknown := uuid.New()
	f.NewMemberRules(10*time.Minute, 10*time.Minute, 10*time.Minute)
	s := f.Snapshot()
	sub := Subject{ID: unknown}

	for _, tc := range []struct {
		need Set
		want string
	}{
		{ViewChannel | SendMessages, "new members can write in "},
		{ViewChannel | AddReactions, "new members can react in "},
		{ViewChannel | Connect | Speak, "new members can speak in "},
	} {
		err := s.Deny(sub, s.Channel(sub, ch), tc.need)
		if err == nil || len(err.Error()) < len(tc.want) || err.Error()[:len(tc.want)] != tc.want {
			t.Errorf("need %b: %v, want %q...", tc.need, err, tc.want)
		}
	}
	if err := s.Deny(sub, s.Channel(sub, ch), ViewChannel|Connect); err != nil {
		t.Errorf("joining a Talk is not held back: %v", err)
	}
	// A refusal with another reason keeps its words.
	if err := s.Deny(sub, s.Channel(sub, ch), ViewChannel|ManageMessages); err == nil || err.Error() == "" {
		t.Errorf("missing permission: %v", err)
	}
}

// Conversations: a new member may answer (writing stays; the chat package
// refuses to write first), reactions follow the react wait, starting a
// conversation follows the write wait.
func TestConversationsFollowTheWaitsOfANewMember(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t.Cleanup(SetClock(func() time.Time { return now }))
	f := permtest.NewFixture()
	newbie, old := uuid.New(), uuid.New()
	dm := f.DM(newbie, old)
	f.Joined(newbie, now).Joined(old, now.Add(-48*time.Hour))
	f.NewMemberRules(time.Hour, 2*time.Hour, 3*time.Hour)
	s := f.Snapshot()

	got := s.Channel(Subject{ID: newbie}, dm)
	if !got.Has(ViewChannel|SendMessages|AttachFiles) || got.Has(AddReactions) {
		t.Fatalf("new member holds %b in a conversation", got)
	}
	if !s.CanStartDM(Subject{ID: newbie}) || s.CanStartNewDM(Subject{ID: newbie}) {
		t.Fatal("a new member may reply but not start conversations")
	}
	if !s.CanStartNewDM(Subject{ID: old}) || !s.Channel(Subject{ID: old}, dm).Has(AddReactions) {
		t.Fatal("older members are not held back")
	}
	if err := s.NewMemberRefusal(newbie, AddReactions); err == nil || err.Error() == "" {
		t.Fatalf("react refusal %v", err)
	}

	now = now.Add(3 * time.Hour)
	if !s.CanStartNewDM(Subject{ID: newbie}) || !s.Channel(Subject{ID: newbie}, dm).Has(AddReactions) {
		t.Fatal("the waits are over")
	}
}

func TestTheCallOfAConversationFollowsTheSpeakWait(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t.Cleanup(SetClock(func() time.Time { return now }))
	f := permtest.NewFixture()
	newbie, old := uuid.New(), uuid.New()
	dm := f.DM(newbie, old)
	call := f.DMTalk(dm)
	f.Joined(newbie, now).Joined(old, now.Add(-48*time.Hour))
	f.NewMemberRules(0, 0, time.Hour)
	s := f.Snapshot()
	sub := Subject{ID: newbie}

	if got := s.Channel(sub, call); got.HasAny(Connect | Speak | Video) {
		t.Fatalf("new member holds %b in the call", got)
	}
	err := s.DenyIn(sub, call, s.Channel(sub, call), ViewChannel|Connect)
	if err == nil || err.Error() != "new members can speak in 3600 s" {
		t.Fatalf("join refusal: %v", err)
	}
	if !s.Channel(Subject{ID: old}, call).Has(Connect | Speak) {
		t.Fatal("the other member joins freely")
	}
	now = now.Add(2 * time.Hour)
	if !s.Channel(sub, call).Has(Connect | Speak) {
		t.Fatal("after the wait the call is open")
	}
}

func TestSpeakingWaitAlsoHoldsCameraAndScreenShare(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t.Cleanup(SetClock(func() time.Time { return now }))
	f := permtest.NewFixture()
	ch := f.Channel(uuid.Nil)
	newbie := uuid.New()
	f.Joined(newbie, now).NewMemberRules(0, 0, time.Hour)
	got := f.Snapshot().Channel(Subject{ID: newbie}, ch)
	if got.HasAny(Speak|Video) || !got.Has(Connect|SendMessages) {
		t.Fatalf("holds %b", got)
	}
}

type recordedChanges struct {
	mu  sync.Mutex
	ids []uuid.UUID
}

func (r *recordedChanges) PermissionsChanged(ids []uuid.UUID) {
	r.mu.Lock()
	r.ids = append(r.ids, ids...)
	r.mu.Unlock()
}

func (r *recordedChanges) seen() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uuid.UUID(nil), r.ids...)
}

// A wait that ends is announced like any change of rights, so a Talk the
// member is in applies the new media policy without a rejoin.
func TestTheEndOfAWaitIsAnnouncedToTheMember(t *testing.T) {
	f := permtest.NewFixture()
	newbie, waiting := uuid.New(), uuid.New()
	// newbie's speak wait ends in 300 ms; waiting's much later.
	f.Joined(newbie, time.Now().Add(-time.Hour+300*time.Millisecond)).Joined(waiting, time.Now())
	f.NewMemberRules(0, 0, time.Hour)
	snap := f.Snapshot()
	svc := NewServiceFrom(func(context.Context, time.Time) (*Snapshot, error) { return snap, nil })
	rec := &recordedChanges{}
	svc.SetNotifier(rec)
	if _, err := svc.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(rec.seen()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	got := rec.seen()
	if len(got) != 1 || got[0] != newbie {
		t.Fatalf("announced %v, want only %v", got, newbie)
	}
}

func TestNoTimerWithoutWaits(t *testing.T) {
	f := permtest.NewFixture()
	svc := NewServiceFrom(func(context.Context, time.Time) (*Snapshot, error) { return f.Snapshot(), nil })
	rec := &recordedChanges{}
	svc.SetNotifier(rec)
	if _, err := svc.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1300 * time.Millisecond)
	if len(rec.seen()) != 0 {
		t.Fatalf("announced %v", rec.seen())
	}
}
