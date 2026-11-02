package push

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
)

func TestCallKindTellsACallCardFromAMissedCallAndAnyOtherMessage(t *testing.T) {
	talk := uuid.New()
	for name, tc := range map[string]struct {
		msg  chat.Message
		want string
	}{
		"a Talk card is a call":      {chat.Message{Embeds: []chat.Embed{{TalkID: &talk}}}, "call"},
		"a missed record":            {chat.Message{Embeds: []chat.Embed{{Call: "missed"}}}, "missed"},
		"the first matching embed":   {chat.Message{Embeds: []chat.Embed{{}, {Call: "missed"}, {TalkID: &talk}}}, "missed"},
		"an embed that is no call":   {chat.Message{Embeds: []chat.Embed{{}}}, ""},
		"a message without embeds":   {chat.Message{}, ""},
		"another call state is none": {chat.Message{Embeds: []chat.Embed{{Call: "ended"}}}, ""},
	} {
		if got := callKind(tc.msg); got != tc.want {
			t.Errorf("%s: callKind = %q, want %q", name, got, tc.want)
		}
	}
}
