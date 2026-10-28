package push

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

var noon = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func settingOf(level chat.NotifyLevel, until *time.Time) chat.NotifySetting {
	return chat.NotifySetting{Level: level, MutedUntil: until}
}

func TestEffectiveNotificationLevelDecidesWhoIsTold(t *testing.T) {
	later, earlier := noon.Add(time.Hour), noon.Add(-time.Hour)
	inherit := settingOf(chat.NotifyInherit, nil)
	cases := []struct {
		name              string
		channel, category chat.NotifySetting
		presence          string
		// plain: an ordinary message; atAll: @all or @here; addressed: a mention by name or role, or a reply.
		plain, atAll, addressed bool
	}{
		{"nothing set means all messages", inherit, inherit, "online", true, true, true},
		{"mentions only", settingOf(chat.NotifyMentions, nil), inherit, "online", false, true, true},
		{"muted lets only what is addressed to the member through", settingOf(chat.NotifyMute, nil), inherit, "online", false, false, true},
		{"muted for an hour", settingOf(chat.NotifyMute, &later), inherit, "online", false, false, true},
		{"the mute ran out", settingOf(chat.NotifyMute, &earlier), inherit, "online", true, true, true},
		{"the category decides when the channel does not", inherit, settingOf(chat.NotifyMentions, nil), "online", false, true, true},
		{"the channel overrides a muted category", settingOf(chat.NotifyAll, nil), settingOf(chat.NotifyMute, nil), "online", true, true, true},
		{"do not disturb silences everything, a mute or not", settingOf(chat.NotifyMute, nil), inherit, "dnd", false, false, false},
		{"focus silences everything", settingOf(chat.NotifyAll, nil), inherit, "focus", false, false, false},
		{"away still gets pushes", settingOf(chat.NotifyAll, nil), inherit, "away", true, true, true},
	}
	for _, c := range cases {
		cand := candidate{presence: c.presence, channelLevel: c.channel, categoryLvl: c.category, now: noon}
		if got := cand.wants(false, false); got != c.plain {
			t.Errorf("%s: plain message wanted = %v, want %v", c.name, got, c.plain)
		}
		if got := cand.wants(true, false); got != c.atAll {
			t.Errorf("%s: @all wanted = %v, want %v", c.name, got, c.atAll)
		}
		if got := cand.wants(true, true); got != c.addressed {
			t.Errorf("%s: addressed message wanted = %v, want %v", c.name, got, c.addressed)
		}
	}
}

func TestOnlyMembersWhoCanViewTheChannelAreTold(t *testing.T) {
	f := permtest.NewFixture()
	category := f.Category()
	open, hidden := f.Channel(category), f.Channel(category)
	f.RoleOverwrite(true, hidden, f.EveryoneID(), 0, perm.ViewChannel)
	insider, outsider := uuid.New(), uuid.New()
	staff := f.Role(1, perm.ViewChannel)
	f.RoleOverwrite(true, hidden, staff, perm.ViewChannel, 0).Assign(insider, staff)
	snap := f.Snapshot()

	if !canView(snap, outsider, open) || !canView(snap, insider, hidden) {
		t.Fatal("a member who may view the channel is not told")
	}
	if canView(snap, outsider, hidden) {
		t.Fatal("a member who may not view the channel would be told")
	}
	if canView(snap, outsider, uuid.New()) {
		t.Fatal("an unknown channel is viewable")
	}
}
