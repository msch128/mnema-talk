package perm_test

import (
	"testing"

	"github.com/google/uuid"
	. "github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

const botWork = ViewChannel | SendMessages

// A bot given access to every channel holds its permissions server-wide and
// sees every channel members see by default; a channel only roles open to
// members stays hidden from it until its own role is allowed there.
func TestBotWithAccessToAllChannels(t *testing.T) {
	bot := uuid.New()
	f := permtest.NewFixture().BotAllChannels(bot)
	own := f.ManagedRole(1, botWork)
	f.Assign(bot, own)
	open := f.Channel(uuid.Nil)
	private := f.Channel(uuid.Nil)
	f.RoleOverwrite(true, private, f.EveryoneID(), 0, ViewChannel)
	staff := f.Role(5, ViewChannel)
	f.RoleOverwrite(true, private, staff, ViewChannel, 0)

	privateCat := f.Category()
	f.RoleOverwrite(false, privateCat, f.EveryoneID(), 0, ViewChannel)
	inPrivateCat := f.Channel(privateCat)
	reopened := f.Channel(privateCat)
	f.RoleOverwrite(true, reopened, f.EveryoneID(), ViewChannel, 0)
	s := f.Snapshot()

	if got := s.Channel(sub(bot), open); got != botWork {
		t.Fatalf("open channel: %b", got)
	}
	if got := s.Channel(sub(bot), private); got != 0 {
		t.Fatalf("a private channel reached the bot: %b", got)
	}
	if got := s.Channel(sub(bot), inPrivateCat); got != 0 {
		t.Fatalf("a channel of a private category reached the bot: %b", got)
	}
	if got := s.Channel(sub(bot), reopened); got != botWork {
		t.Fatalf("a channel reopened to everyone in a private category: %b", got)
	}
	if s.CategoryVisible(sub(bot), privateCat) {
		t.Fatal("a private category is visible to the bot")
	}
	if !s.CategoryVisible(sub(bot), uuid.Nil) {
		t.Fatal("the bot cannot see an open category")
	}

	// Listing the channel gives the bot's own role an allow overwrite there.
	f.RoleOverwrite(true, private, own, ViewChannel|SendMessages, 0)
	if got := f.Snapshot().Channel(sub(bot), private); got != botWork {
		t.Fatalf("listed private channel: %b", got)
	}
}

func TestBotWithAccessToAllChannelsFollowsEveryonesViewBit(t *testing.T) {
	bot := uuid.New()
	f := permtest.NewFixture().Everyone(SendMessages).BotAllChannels(bot)
	own := f.ManagedRole(1, botWork)
	f.Assign(bot, own)
	ch := f.Channel(uuid.Nil)
	if got := f.Snapshot().Channel(sub(bot), ch); got != 0 {
		t.Fatalf("members cannot view anything by default, the bot sees %b", got)
	}
	f.RoleOverwrite(true, ch, f.EveryoneID(), ViewChannel, 0)
	if got := f.Snapshot().Channel(sub(bot), ch); got != botWork {
		t.Fatalf("a channel opened to everyone: %b", got)
	}
}

// Without View channels of its own the choice gives the bot nothing, even
// where @everyone allows viewing: nothing of @everyone is added to a bot.
func TestBotWithAccessToAllChannelsGetsNothingFromEveryone(t *testing.T) {
	bot := uuid.New()
	f := permtest.NewFixture().BotAllChannels(bot)
	own := f.ManagedRole(1, SendMessages)
	f.Assign(bot, own)
	ch := f.Channel(uuid.Nil)
	f.RoleOverwrite(true, ch, f.EveryoneID(), ViewChannel|AddReactions, 0)
	if got := f.Snapshot().Channel(sub(bot), ch); got != 0 {
		t.Fatalf("a bot without View channels sees %b", got)
	}
}

func TestPreviewOfABotRoleFollowsTheBotsRule(t *testing.T) {
	bot := uuid.New()
	f := permtest.NewFixture().BotAllChannels(bot)
	own := f.BotOwnRole(1, botWork, bot)
	open := f.Channel(uuid.Nil)
	private := f.Channel(uuid.Nil)
	f.RoleOverwrite(true, private, f.EveryoneID(), 0, ViewChannel)
	view, who, ok := f.Snapshot().AsRoleOnly(own)
	if !ok {
		t.Fatal("no preview")
	}
	if view.Channel(who, open) != botWork || view.Channel(who, private) != 0 {
		t.Fatalf("preview: open %b, private %b", view.Channel(who, open), view.Channel(who, private))
	}
}
