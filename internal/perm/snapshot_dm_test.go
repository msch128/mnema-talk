package perm_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func TestOnlyMembersOfAConversationGetAnything(t *testing.T) {
	f := permtest.NewFixture()
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
	f := permtest.NewFixture()
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
	f := permtest.NewFixture()
	alice, bob := uuid.New(), uuid.New()
	dm := f.DM(alice, bob)
	f.DMEnabled(false)
	if p := f.Snapshot().Channel(Subject{ID: alice}, dm); p != ViewChannel {
		t.Errorf("switch off: %b, want read only", p)
	}
	if f.Snapshot().CanStartDM(Subject{ID: alice}) {
		t.Error("nobody starts conversations with the switch off")
	}
}

func TestPendingRulesAndBotsCannotStartConversations(t *testing.T) {
	f := permtest.NewFixture()
	member, bot := uuid.New(), uuid.New()
	f.Rules(1).Bot(bot)
	if f.Snapshot().CanStartDM(Subject{ID: member}) {
		t.Error("a member who has not accepted the rules does not start conversations")
	}
	if f.Snapshot().CanStartDM(Subject{ID: bot}) {
		t.Error("a bot holds nothing it was not given")
	}
}

func TestATalkOfAConversationIsForItsMembersOnly(t *testing.T) {
	f := permtest.NewFixture()
	alice, bob, carol, owner, admin := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.Owner(owner).Assign(admin, f.Role(5, Administrator))
	dm := f.DM(alice, bob)
	talk := f.DMTalk(dm)
	s := f.Snapshot()

	for name, who := range map[string]uuid.UUID{"a stranger": carol, "the Owner": owner, "an Administrator": admin} {
		if p := s.Channel(Subject{ID: who, Owner: who == owner}, talk); p != 0 {
			t.Errorf("%s holds %b in a Talk of a conversation they are not in", name, p)
		}
	}
	for name, who := range map[string]uuid.UUID{"alice": alice, "bob": bob} {
		if p := s.Channel(Subject{ID: who}, talk); !p.Has(ViewChannel | Connect) {
			t.Errorf("%s holds %b in the Talk of their conversation", name, p)
		}
	}
	// The Owner's "everything" shortcut must not reach it: listings that filter
	// by ViewableChannels / HiddenChannels (search, inbox, saved, notify) keep it out.
	plain := f.Channel(uuid.Nil)
	s = f.Snapshot()
	sub := Subject{ID: owner, Owner: true}
	viewable, hidden := s.ViewableChannels(sub), s.HiddenChannels(sub)
	if viewable == nil || slices.Contains(viewable, talk) || !slices.Contains(viewable, plain) {
		t.Errorf("the Owner can view %v, which must hold the plain channel and not the Talk", viewable)
	}
	if !slices.Contains(hidden, talk) || slices.Contains(hidden, plain) {
		t.Errorf("hidden from the Owner: %v", hidden)
	}
	if got := s.ViewableChannels(Subject{ID: alice}); !slices.Contains(got, talk) {
		t.Errorf("a member can view their Talk: %v", got)
	}
}

func TestAMemberWhoJoinsTheConversationGetsItsTalk(t *testing.T) {
	f := permtest.NewFixture()
	alice, bob, carol := uuid.New(), uuid.New(), uuid.New()
	dm := f.DM(alice, bob)
	talk := f.DMTalk(dm)
	if f.Snapshot().Channel(Subject{ID: carol}, talk) != 0 {
		t.Fatal("carol is not in it yet")
	}
	f.AddDMMember(dm, carol)
	if !f.Snapshot().Channel(Subject{ID: carol}, talk).Has(Connect) {
		t.Fatal("a new member of a group may join its Talk")
	}
}

// H-L6-04: a conversation's call is resolved by membership, not by the
// community's channel rules. A community that hides channels from @everyone
// and denies everything on the call by overwrite still leaves the call to the
// conversation's members.
func TestMembershipAloneOpensTheCallOfAConversation(t *testing.T) {
	f := permtest.NewFixture()
	alice, bob := uuid.New(), uuid.New()
	f.Everyone(DefaultEveryone &^ ViewChannel)
	dm := f.DM(alice, bob)
	call := f.DMTalk(dm)
	f.RoleOverwrite(true, call, f.EveryoneID(), 0, ChannelScoped)
	f.MemberOverwrite(true, call, bob, 0, ChannelScoped)
	s := f.Snapshot()
	want := ViewChannel | Connect | Speak | Video | SendMessages | AttachFiles | AddReactions
	for name, who := range map[string]uuid.UUID{"alice": alice, "bob, denied by a member overwrite": bob} {
		if got := s.Channel(Subject{ID: who}, call); !got.Has(want) || got.HasAny(ManageMessages|MentionEveryone) {
			t.Errorf("%s holds %b in the call, want %b and no moderation of its chat", name, got, want)
		}
		if !slices.Contains(s.ViewableChannels(Subject{ID: who}), call) || !s.MayConnectAnywhere(Subject{ID: who}) {
			t.Errorf("%s does not get the call in the lists or a relay credential", name)
		}
	}
	// The server-wide rules still hold: a timeout takes the call's voice and chat, reading stays.
	f.Timeout(alice, time.Now().Add(time.Hour), "calm down")
	if got := f.Snapshot().Channel(Subject{ID: alice}, call); got.HasAny(Connect|Speak|SendMessages) || !got.Has(ViewChannel) {
		t.Errorf("a timed out member holds %b in the call", got)
	}
}

// Nobody outside the conversation gets anything from the community's rights,
// however wide: View channels, the moderation bits, overwrites that allow
// everything, Administrator, the Owner.
func TestNoCommunityRightOpensTheCallOfAConversation(t *testing.T) {
	f := permtest.NewFixture()
	alice, bob, viewer, mod, admin, owner := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	modRole := f.Role(4, ManageChannels|DisconnectMembers|ManageStage|ManageMessages|ManageRoles|ManageMembers|KickMembers)
	f.Owner(owner).Assign(mod, modRole).Assign(admin, f.Role(5, Administrator))
	dm := f.DM(alice, bob)
	call := f.DMTalk(dm)
	f.RoleOverwrite(true, call, f.EveryoneID(), ChannelScoped, 0)
	f.RoleOverwrite(true, call, modRole, ChannelScoped, 0)
	f.MemberOverwrite(true, call, viewer, ChannelScoped, 0)
	s := f.Snapshot()
	outsiders := map[string]Subject{"a member with View channels": {ID: viewer}, "a moderator": {ID: mod},
		"an Administrator": {ID: admin}, "the Owner": {ID: owner, Owner: true}}
	for name, sub := range outsiders {
		if p := s.Channel(sub, call); p != 0 {
			t.Errorf("%s holds %b in a call they are not in", name, p)
		}
		if slices.Contains(s.ViewableChannels(sub), call) || s.ChannelPermissions(sub)[call] != 0 {
			t.Errorf("%s gets the call in a list", name)
		}
	}
}

// A member of the conversation who moderates the community keeps the per-Talk
// removal there (Disconnect members; doc/moderation.md: such a removal is not
// audited), but nobody except the Owner moderates the call's chat.
func TestAModeratorInTheConversationMayRemoveFromItsCall(t *testing.T) {
	f := permtest.NewFixture()
	alice, bob := uuid.New(), uuid.New()
	f.Assign(alice, f.Role(4, DisconnectMembers|ManageMessages|MentionEveryone))
	call := f.DMTalk(f.DM(alice, bob))
	got := f.Snapshot().Channel(Subject{ID: alice}, call)
	if !got.Has(DisconnectMembers) || got.HasAny(ManageMessages|MentionEveryone) {
		t.Errorf("a moderating member holds %b in the call", got)
	}
}
