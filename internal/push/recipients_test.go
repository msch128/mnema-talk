package push

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
)

var noon = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func settingOf(level chat.NotifyLevel, until *time.Time) chat.NotifySetting {
	return chat.NotifySetting{Level: level, MutedUntil: until}
}

func TestEffectiveNotificationLevelDecidesWhoIsTold(t *testing.T) {
	later, earlier := noon.Add(time.Hour), noon.Add(-time.Hour)
	cases := []struct {
		name              string
		channel, category chat.NotifySetting
		presence          string
		plain, mention    bool
	}{
		{"nothing set means all messages", settingOf(chat.NotifyInherit, nil), settingOf(chat.NotifyInherit, nil), "online", true, true},
		{"mentions only", settingOf(chat.NotifyMentions, nil), settingOf(chat.NotifyInherit, nil), "online", false, true},
		{"muted", settingOf(chat.NotifyMute, nil), settingOf(chat.NotifyInherit, nil), "online", false, false},
		{"muted for an hour", settingOf(chat.NotifyMute, &later), settingOf(chat.NotifyInherit, nil), "online", false, false},
		{"the mute ran out", settingOf(chat.NotifyMute, &earlier), settingOf(chat.NotifyInherit, nil), "online", true, true},
		{"the category decides when the channel does not", settingOf(chat.NotifyInherit, nil), settingOf(chat.NotifyMentions, nil), "online", false, true},
		{"the channel overrides a muted category", settingOf(chat.NotifyAll, nil), settingOf(chat.NotifyMute, nil), "online", true, true},
		{"do not disturb silences everything", settingOf(chat.NotifyAll, nil), settingOf(chat.NotifyInherit, nil), "dnd", false, false},
		{"focus silences everything", settingOf(chat.NotifyAll, nil), settingOf(chat.NotifyInherit, nil), "focus", false, false},
		{"away still gets pushes", settingOf(chat.NotifyAll, nil), settingOf(chat.NotifyInherit, nil), "away", true, true},
	}
	for _, c := range cases {
		cand := candidate{presence: c.presence, channelLevel: c.channel, categoryLvl: c.category, now: noon}
		if got := cand.wants(false); got != c.plain {
			t.Errorf("%s: plain message wanted = %v, want %v", c.name, got, c.plain)
		}
		if got := cand.wants(true); got != c.mention {
			t.Errorf("%s: mention wanted = %v, want %v", c.name, got, c.mention)
		}
	}
}

func TestOnlyMembersWhoCanViewTheChannelAreTold(t *testing.T) {
	f := perm.NewFixture()
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
