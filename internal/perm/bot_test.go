package perm_test

import (
	"testing"

	"github.com/google/uuid"
	. "github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func sub(id uuid.UUID) Subject { return Subject{ID: id} }

// A bot gets nothing from @everyone, neither base permissions nor
// @everyone overwrites; what it is given through roles and its own
// overwrites works like for a member.
func TestBotsDoNotInheritEveryone(t *testing.T) {
	bot, member := uuid.New(), uuid.New()
	f := permtest.NewFixture().Bot(bot)
	ch := f.Channel(uuid.Nil)
	open := f.Channel(uuid.Nil)
	f.RoleOverwrite(true, open, f.EveryoneID(), ViewChannel|SendMessages, 0)
	s := f.Snapshot()

	if got := s.Global(sub(bot)); got != 0 {
		t.Fatalf("a bot without roles holds %b server-wide", got)
	}
	if got := s.Channel(sub(bot), ch); got != 0 {
		t.Fatalf("channel %b", got)
	}
	if got := s.Channel(sub(bot), open); got != 0 {
		t.Fatalf("an @everyone overwrite reached the bot: %b", got)
	}
	if !s.Channel(sub(member), open).Has(ViewChannel) || !s.IsBot(bot) || s.IsBot(member) {
		t.Fatal("members keep @everyone")
	}

	role := f.Role(1, SendMessages|AddReactions)
	f.Assign(bot, role)
	f.RoleOverwrite(true, ch, role, ViewChannel, 0)
	if got := s.Channel(sub(bot), ch); !got.Has(ViewChannel|SendMessages|AddReactions) || got.Has(ManageMessages) {
		t.Fatalf("role + overwrite: %b", got)
	}
	if s.Channel(sub(bot), open).Has(ViewChannel) {
		t.Fatal("a role without overwrite must not open a channel")
	}
	f.MemberOverwrite(true, ch, bot, 0, SendMessages)
	if s.Channel(sub(bot), ch).Has(SendMessages) {
		t.Fatal("a member overwrite denies the bot")
	}
}

func TestOwnerGrantOnly(t *testing.T) {
	owner, admin := uuid.New(), uuid.New()
	f := permtest.NewFixture().Owner(owner)
	r := f.Role(1, Administrator)
	f.Assign(admin, r)
	s := f.Snapshot()
	if err := CheckGrant(s, sub(admin), ManageBots); err == nil {
		t.Fatal("an Administrator must not grant manage bots")
	}
	if err := CheckGrant(s, sub(owner), ManageBots); err != nil {
		t.Fatal(err)
	}
	if !AdminArea.Has(ManageBots) {
		t.Fatal("manage bots opens the admin area")
	}
}

func TestCanActAs(t *testing.T) {
	owner, caller, bot := uuid.New(), uuid.New(), uuid.New()
	f := permtest.NewFixture().Owner(owner).Bot(bot)
	ch := f.Channel(uuid.Nil)
	own := f.Role(1, SendMessages)
	botRole := f.Role(2, SendMessages)
	f.Assign(caller, own).Assign(bot, botRole)
	f.RoleOverwrite(true, ch, botRole, ViewChannel, 0)
	f.RoleOverwrite(true, ch, own, ViewChannel, 0)
	s := f.Snapshot()

	if err := s.CanActAs(sub(caller), sub(bot)); err != nil {
		t.Fatalf("equal rights: %v", err)
	}
	if !s.RoleHeldByBot(botRole) || s.RoleHeldByBot(own) {
		t.Fatal("RoleHeldByBot")
	}
	// More in one channel, more server-wide, and administrator power are refused.
	f.RoleOverwrite(true, ch, botRole, ViewChannel|ManageMessages, 0)
	if s.CanActAs(sub(caller), sub(bot)) == nil {
		t.Fatal("a channel permission the caller lacks")
	}
	if err := s.CanActAs(sub(owner), sub(bot)); err != nil {
		t.Fatal(err)
	}
	f.RoleOverwrite(true, ch, botRole, ViewChannel, 0)
	other := f.Role(3, SendMessages|ManageMessages)
	f.Assign(bot, other)
	if s.CanActAs(sub(caller), sub(bot)) == nil {
		t.Fatal("a server-wide permission the caller lacks")
	}
	adm := f.Role(4, Administrator)
	f.Assign(bot, adm)
	if s.CanActAs(sub(caller), sub(bot)) == nil {
		t.Fatal("administrator bot")
	}
}

func TestElevatedChannelPermissions(t *testing.T) {
	bot := uuid.New()
	f := permtest.NewFixture().Bot(bot)
	ch := f.Channel(uuid.Nil)
	cat := f.Category()
	r := f.Role(1, SendMessages)
	f.Assign(bot, r)
	s := f.Snapshot()
	if s.ElevatedFor(sub(bot)) || s.RoleGrantsElevatedInChannels(r) {
		t.Fatal("nothing elevated yet")
	}
	f.RoleOverwrite(true, ch, r, ViewChannel|DisconnectMembers, 0)
	if !s.ElevatedFor(sub(bot)) || !s.RoleGrantsElevatedInChannels(r) {
		t.Fatal("Disconnect members in a channel is elevated")
	}
	other := f.Role(2, SendMessages)
	f.RoleOverwrite(false, cat, other, DisconnectMembers, 0)
	if !s.RoleGrantsElevatedInChannels(other) {
		t.Fatal("category overwrites count")
	}
	if s.ElevatedFor(sub(uuid.New())) {
		t.Fatal("an unrelated account holds nothing")
	}
}

func TestCheckBotGrowth(t *testing.T) {
	bot, actor := uuid.New(), uuid.New()
	ch := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	// build makes a world: the bot's server-wide bits and channel bits, and
	// what the actor may do in the channel.
	build := func(botGlobal, botInChannel, actorInChannel Set) *Snapshot {
		f := permtest.NewFixture().Bot(bot)
		f.ChannelWithID(ch, uuid.Nil)
		br := f.Role(1, SendMessages|botGlobal)
		f.Assign(bot, br)
		f.RoleOverwrite(true, ch, br, ViewChannel|botInChannel, 0)
		ar := f.Role(2, SendMessages)
		f.Assign(actor, ar)
		f.RoleOverwrite(true, ch, ar, ViewChannel|actorInChannel, 0)
		return f.Snapshot()
	}
	plain := build(0, 0, 0)
	if err := CheckBotGrowth(plain, plain, sub(actor)); err != nil {
		t.Fatal(err)
	}
	// Elevated bits, server-wide or in a channel.
	if CheckBotGrowth(plain, build(ManageChannels, 0, 0), sub(actor)) == nil || CheckBotGrowth(plain, build(0, DisconnectMembers, DisconnectMembers), sub(actor)) == nil {
		t.Fatal("becoming elevated must be refused")
	}
	// Permissions the actor lacks, server-wide and in a channel.
	if CheckBotGrowth(plain, build(ManageMessages, 0, 0), sub(actor)) == nil {
		t.Fatal("a server-wide permission the actor lacks")
	}
	if CheckBotGrowth(plain, build(0, AddReactions|ManageMessages, 0), sub(actor)) == nil {
		t.Fatal("a channel permission the actor lacks")
	}
	// What the actor holds may be given, and what a bot already had stays.
	if err := CheckBotGrowth(plain, build(0, ManageMessages, ManageMessages), sub(actor)); err != nil {
		t.Fatalf("within the actor's rights: %v", err)
	}
	elevated := build(ManageChannels, 0, 0)
	if err := CheckBotGrowth(elevated, elevated, sub(actor)); err != nil {
		t.Fatalf("unchanged: %v", err)
	}
	// Taking permissions away, or a snapshot without the bot, is no growth.
	if err := CheckBotGrowth(elevated, plain, sub(actor)); err != nil {
		t.Fatal(err)
	}
	if err := CheckBotGrowth(plain, EmptySnapshot(), sub(actor)); err != nil {
		t.Fatal(err)
	}
}
