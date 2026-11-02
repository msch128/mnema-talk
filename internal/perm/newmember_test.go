package perm_test

import (
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

func TestTalksOfConversationsAreExemptFromTheWaits(t *testing.T) {
	f := permtest.NewFixture()
	a, b := uuid.New(), uuid.New()
	dm := f.DM(a, b)
	talk := f.DMTalk(dm)
	f.NewMemberRules(time.Hour, time.Hour, time.Hour)
	s := f.Snapshot()
	if !s.Channel(Subject{ID: a}, talk).Has(Speak) {
		t.Fatal("members of a conversation speak in its Talk at once")
	}
}
