package perm

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOnlyMembersOfAConversationGetAnything(t *testing.T) {
	f := NewFixture()
	alice, bob, carol, owner, admin := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.Owner(owner).Assign(admin, f.Role(5, Administrator))
	dm := f.DM(alice, bob)
	s := f.Snapshot()

	for name, who := range map[string]uuid.UUID{"a stranger": carol, "the Owner": owner, "an Administrator": admin} {
		if p := s.Channel(Subject{ID: who, Owner: who == owner}, dm); p != 0 {
			t.Errorf("%s holds %b in a conversation they are not in", name, p)
		}
	}
	got := s.Channel(Subject{ID: alice}, dm)
	if want := ViewChannel | SendMessages | AttachFiles | AddReactions; got != want {
		t.Errorf("a member holds %b, want %b", got, want)
	}
	if got.HasAny(ManageMessages | MentionEveryone | Administrator | Connect) {
		t.Errorf("a conversation is not moderated and has no mention-everyone: %b", got)
	}
	if !s.IsDM(dm) || s.KnowsChannel(dm) {
		t.Error("a conversation is known as one, and is not a channel")
	}
	if len(s.ViewableChannels(Subject{ID: alice})) != 0 {
		t.Error("conversations are not channels the member can view")
	}
	if dms := s.DMsOf(alice); len(dms) != 1 || dms[0] != dm || len(s.DMMembers(dm)) != 2 {
		t.Errorf("membership: %v", dms)
	}
}

func TestAMemberKeepsReadingWhenWritingIsTakenAway(t *testing.T) {
	f := NewFixture()
	alice, bob := uuid.New(), uuid.New()
	dm := f.DM(alice, bob)
	f.Timeout(alice, time.Now().Add(time.Hour), "calm down")
	// Without Send direct messages (an onboarding role, say) a member reads only.
	f.Everyone(DefaultEveryone &^ SendDirectMessages)
	for name, who := range map[string]uuid.UUID{"timeout": alice, "no permission": bob} {
		if p := f.Snapshot().Channel(Subject{ID: who}, dm); p != ViewChannel {
			t.Errorf("%s: %b, want read only", name, p)
		}
	}
}

func TestTheCommunitySwitchMakesConversationsReadOnly(t *testing.T) {
	f := NewFixture()
	alice, bob := uuid.New(), uuid.New()
	dm := f.DM(alice, bob)
	f.Snapshot().dmEnabled = false
	if p := f.Snapshot().Channel(Subject{ID: alice}, dm); p != ViewChannel {
		t.Errorf("switch off: %b, want read only", p)
	}
	if f.Snapshot().CanStartDM(Subject{ID: alice}) {
		t.Error("nobody starts conversations with the switch off")
	}
}

func TestPendingRulesAndBotsCannotStartConversations(t *testing.T) {
	f := NewFixture()
	member, bot := uuid.New(), uuid.New()
	f.Rules(1).Bot(bot)
	if f.Snapshot().CanStartDM(Subject{ID: member}) {
		t.Error("a member who has not accepted the rules does not start conversations")
	}
	if f.Snapshot().CanStartDM(Subject{ID: bot}) {
		t.Error("a bot holds nothing it was not given")
	}
}
