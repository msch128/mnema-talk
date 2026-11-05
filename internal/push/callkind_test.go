package push

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

func TestCallKindTellsACallCardFromAMissedCallAndAnyOtherMessage(t *testing.T) {
	talk := uuid.New()
	for name, tc := range map[string]struct {
		msg  core.Message
		want string
	}{
		"a Talk card is a call":      {core.Message{Embeds: []core.Embed{{TalkID: &talk}}}, "call"},
		"a missed record":            {core.Message{Embeds: []core.Embed{{Call: "missed"}}}, "missed"},
		"the first matching embed":   {core.Message{Embeds: []core.Embed{{}, {Call: "missed"}, {TalkID: &talk}}}, "missed"},
		"an embed that is no call":   {core.Message{Embeds: []core.Embed{{}}}, ""},
		"a message without embeds":   {core.Message{}, ""},
		"another call state is none": {core.Message{Embeds: []core.Embed{{Call: "ended"}}}, ""},
	} {
		if got := callKind(tc.msg); got != tc.want {
			t.Errorf("%s: callKind = %q, want %q", name, got, tc.want)
		}
	}
}
