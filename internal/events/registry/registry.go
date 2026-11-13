// Package registry lists every realtime event type of the server and derives
// the schema of their JSON from the Go types (api/events.json). The web app
// generates its event types from that file, so the two cannot drift.
//
// A new event type must be added to All; TestEveryEventTypeIsRegistered fails
// otherwise.
package registry

import (
	"reflect"
	"slices"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat/blocks"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/dm"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/chat/saved"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/poll"
	"github.com/msch128/mnema-talk/internal/ws"
)

// Entry is one event type. Payload is set when the event is a domain type
// under another name (core.MessageCreate is a core.Message): the schema then
// describes, or refers to, that type.
type Entry struct {
	Event   events.Event
	Payload any
}

// shape returns the Go type whose JSON the entry sends.
func (e Entry) shape() reflect.Type {
	if e.Payload != nil {
		return reflect.TypeOf(e.Payload)
	}
	t := reflect.TypeOf(e.Event)
	if t.Kind() == reflect.Pointer {
		return t.Elem()
	}
	return t
}

// All is every event type the server publishes.
func All() []Entry {
	return slices.Concat(members(), messageEvents(), channels(), talks(), moderation(), others())
}

// Members and presence.
func members() []Entry {
	return []Entry{
		{Event: auth.UserUpdate{}, Payload: auth.User{}},
		{Event: wire.UserDisabled{}},
		{Event: auth.MemberJoined{}, Payload: auth.User{}},
		{Event: ws.PresenceUpdate{}},
		{Event: ws.PresenceSnapshot{}},
	}
}

// Messages.
func messageEvents() []Entry {
	return []Entry{
		{Event: (*core.MessageCreate)(nil), Payload: core.Message{}},
		{Event: (*core.MessageUpdate)(nil), Payload: core.Message{}},
		{Event: wire.MessageDelete{}},
		{Event: messages.ReactionResult{}},
		{Event: messages.PinsChanged{}},
		{Event: wire.PinRemoved{}},
		{Event: poll.Update{}},
		{Event: wire.Typing{}},
		{Event: wire.ThreadFollowUpdated{}},
		{Event: wire.ReadStateMarked{}},
		{Event: wire.ReadStateUnread{}},
		{Event: wire.ReadStateCounts{}},
		{Event: wire.ReadStateChannelRefresh{}},
		{Event: wire.ReadStateRefresh{}},
		{Event: saved.SavedUpdated{}},
		{Event: bot.InteractionResponse{}},
		{Event: bot.InteractionModal{}},
		{Event: bot.InteractionDeferred{}},
	}
}

// Channels.
func channels() []Entry {
	return []Entry{
		{Event: wire.ChannelsChanged{}},
		{Event: wire.ForumTagsChanged{}},
	}
}

// Talks.
func talks() []Entry {
	return []Entry{
		{Event: ws.VoiceJoin{}},
		{Event: wire.VoiceLeave{}},
		{Event: ws.VoiceSnapshot{}},
		{Event: ws.VoiceRooms{}},
		{Event: wire.VoiceKicked{}},
		{Event: wire.VoiceSpeaking{}},
		{Event: wire.VoiceMuteState{}},
		{Event: wire.UserStats{}},
		{Event: wire.MediaState{}},
		{Event: wire.ScreenViewers{}},
		{Event: ws.ScreenLayers{}},
		{Event: wire.ScreenQuality{}},
		{Event: wire.ScreenLayersWanted{}},
		{Event: wire.CamerasCapped{}},
		{Event: wire.MediaHandshake{}},
		{Event: ws.WebrtcOffer{}},
		{Event: (*ws.WebrtcCandidate)(nil)},
		{Event: wire.TalkMoved{}},
		{Event: wire.TalkLeader{}},
		{Event: wire.TalkReaction{}},
		{Event: wire.StageHand{}},
		{Event: wire.StageTalkPower{}},
		{Event: dm.DMCallRing{}},
		{Event: dm.DMCallEnd{}},
	}
}

// Moderation and administration.
func moderation() []Entry {
	return []Entry{
		{Event: wire.ModerationNotice{}},
		{Event: wire.AutomodTimeoutNotice{}},
		{Event: wire.MessageDeletedNotice{}},
		{Event: wire.MessagesDeletedNotice{}},
		{Event: wire.MemberTimeout{}},
		{Event: wire.Kicked{}},
		{Event: wire.RolesChanged{}},
		{Event: wire.PermissionsChanged{}},
		{Event: wire.MemberRolesChanged{}},
		{Event: wire.ReportsOpen{}},
		{Event: wire.SystemUpdate{}},
	}
}

// Everything else.
func others() []Entry {
	return []Entry{
		{Event: ws.ServerInfo{}},
		{Event: wire.Pong{}},
		{Event: wire.EmojiChanged{}},
		{Event: wire.CommunityUpdated{}},
		{Event: blocks.BlockUpdated{}},
		{Event: dm.DMCreated{}, Payload: dm.DirectMessage{}},
		{Event: dm.DMMemberAdded{}, Payload: dm.DMMemberChange{}},
		{Event: dm.DMMemberRemoved{}, Payload: dm.DMMemberChange{}},
		{Event: dm.DMPrefsUpdated{}},
		{Event: later.ReminderEvent{}},
		{Event: later.ScheduledEvent{}},
		{Event: (*communityevents.EventUpdate)(nil), Payload: communityevents.CommunityEvent{}},
		{Event: communityevents.EventDeletePayload{}},
		{Event: communityevents.EventReminderPayload{}},
		{Event: communityevents.SignupNoticePayload{}},
		{Event: communityevents.CommentPayload{}},
		{Event: communityevents.CommentNoticePayload{}},
	}
}
