package chat

import (
	"testing"
	"time"
)

func TestResolveNotifyOrderAndExpiry(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	set := func(l NotifyLevel, until *time.Time) NotifySetting { return NotifySetting{Level: l, MutedUntil: until} }
	cases := []struct {
		name         string
		channel, cat NotifySetting
		want         NotifyLevel
	}{
		{"nothing set", set("", nil), set("", nil), NotifyAll},
		{"inherit both", set(NotifyInherit, nil), set(NotifyInherit, nil), NotifyAll},
		{"category decides", set(NotifyInherit, nil), set(NotifyMentions, nil), NotifyMentions},
		{"channel beats category", set(NotifyAll, nil), set(NotifyMute, nil), NotifyAll},
		{"channel mute beats category all", set(NotifyMute, nil), set(NotifyAll, nil), NotifyMute},
		{"timed mute active", set(NotifyMute, &future), set(NotifyAll, nil), NotifyMute},
		{"expired channel mute falls through", set(NotifyMute, &past), set(NotifyMentions, nil), NotifyMentions},
		{"expired mute at the boundary", set(NotifyMute, &now), set(NotifyInherit, nil), NotifyAll},
		{"expired category mute", set(NotifyInherit, nil), set(NotifyMute, &past), NotifyAll},
		{"indefinite mute", set(NotifyMute, nil), set(NotifyInherit, nil), NotifyMute},
	}
	for _, c := range cases {
		if got := ResolveNotify(now, c.channel, c.cat); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestNotifyLevelValid(t *testing.T) {
	for _, l := range []NotifyLevel{NotifyAll, NotifyMentions, NotifyMute, NotifyInherit} {
		if !l.valid() {
			t.Errorf("%q should be valid", l)
		}
	}
	if NotifyLevel("loud").valid() || NotifyLevel("").valid() {
		t.Error("unknown levels must be invalid")
	}
}
