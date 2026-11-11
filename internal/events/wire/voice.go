package wire

import (
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
)

// The events of Talks: who is in one, what they share and what the server
// forwards. Events that carry a member's profile or the layers of a share
// live in internal/ws.

// VoiceLeave is the voice_state_update of a member leaving a Talk; action is
// always "leave". Joining is ws.VoiceJoin.
type VoiceLeave struct {
	Action    string    `json:"action" enums:"leave"`
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	UserID    uuid.UUID `json:"user_id" format:"uuid"`
}

func (VoiceLeave) EventName() string { return events.VoiceStateUpdate }

// VoiceKicked tells one connection that it is not, or no longer, in a Talk
// (voice_kicked); reason is left out when a moderator removed the member.
type VoiceKicked struct {
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Reason    string    `json:"reason,omitempty"`
}

func (VoiceKicked) EventName() string { return events.VoiceKicked }

// VoiceSpeaking says a member started or stopped speaking (voice_speaking).
type VoiceSpeaking struct {
	Active    bool      `json:"active"`
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	UserID    uuid.UUID `json:"user_id" format:"uuid"`
}

func (VoiceSpeaking) EventName() string { return events.VoiceSpeaking }

// VoiceMuteState says whether a member is muted or deafened (voice_mute_state).
type VoiceMuteState struct {
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Deafened  bool      `json:"deafened"`
	Muted     bool      `json:"muted"`
	UserID    uuid.UUID `json:"user_id" format:"uuid"`
}

func (VoiceMuteState) EventName() string { return events.VoiceMuteState }

// UserStats carries a member's public voice time after a stay (user_stats).
type UserStats struct {
	UserID       uuid.UUID `json:"user_id" format:"uuid"`
	VoiceSeconds int64     `json:"voice_seconds"`
}

func (UserStats) EventName() string { return events.UserStats }

// MediaState says whether a member shares their screen or camera
// (webrtc_media_state).
type MediaState struct {
	Camera    bool      `json:"camera"`
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Screen    bool      `json:"screen"`
	UserID    uuid.UUID `json:"user_id" format:"uuid"`
}

func (MediaState) EventName() string { return events.WebrtcMediaState }

// ScreenViewers lists who watches a member's screen share (screen_viewers);
// viewers is [] when nobody does, never null.
type ScreenViewers struct {
	ChannelID uuid.UUID   `json:"channel_id" format:"uuid"`
	UserID    uuid.UUID   `json:"user_id" format:"uuid"`
	Viewers   []uuid.UUID `json:"viewers" format:"uuid"`
}

func (ScreenViewers) EventName() string { return events.ScreenViewers }

// ScreenQuality tells a viewer which layer of a screen share it receives
// (-1 before the first picture) and whether the server chose it (screen_quality).
type ScreenQuality struct {
	Auto      bool      `json:"auto"`
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Layer     int       `json:"layer"`
	UserID    uuid.UUID `json:"user_id" format:"uuid"`
}

func (ScreenQuality) EventName() string { return events.ScreenQuality }

// ScreenLayersWanted tells a sharer which of its layers somebody receives
// (screen_layers_wanted); source is "camera" for the layers of the camera and
// left out for the screen. layers is never null.
type ScreenLayersWanted struct {
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Layers    []int     `json:"layers"`
	Source    string    `json:"source,omitempty" enums:"camera"`
}

func (ScreenLayersWanted) EventName() string { return events.ScreenLayersWanted }

// CamerasCapped tells a viewer whose cameras the server does not forward to
// it (cameras_capped); user_ids is never null.
type CamerasCapped struct {
	ChannelID uuid.UUID   `json:"channel_id" format:"uuid"`
	UserIDs   []uuid.UUID `json:"user_ids" format:"uuid"`
}

func (CamerasCapped) EventName() string { return events.CamerasCapped }

// MediaHandshake relays a member's media-key handshake to the others in the
// Talk (media_handshake); the server never reads wire.
type MediaHandshake struct {
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	UserID    uuid.UUID `json:"user_id" format:"uuid"`
	Wire      string    `json:"wire"`
}

func (MediaHandshake) EventName() string { return events.MediaHandshake }

// TalkMoved tells a connection that joining a Create-a-Talk lobby opened the
// Talk channel_id (talk_moved).
type TalkMoved struct {
	ChannelID     uuid.UUID `json:"channel_id" format:"uuid"`
	FromChannelID uuid.UUID `json:"from_channel_id" format:"uuid"`
}

func (TalkMoved) EventName() string { return events.TalkMoved }

// TalkLeader says who leads a temporary Talk now (talk_leader); leader_id is
// null when nobody does.
type TalkLeader struct {
	ChannelID uuid.UUID  `json:"channel_id" format:"uuid"`
	LeaderID  *uuid.UUID `json:"leader_id" format:"uuid"`
}

func (TalkLeader) EventName() string { return events.TalkLeader }

// TalkReaction is a short-lived emoji a member sent to their Talk (talk_reaction).
type TalkReaction struct {
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Emoji     string    `json:"emoji"`
	UserID    uuid.UUID `json:"user_id" format:"uuid"`
}

func (TalkReaction) EventName() string { return events.TalkReaction }

// StageHand says a member raised or lowered their hand in a Stage
// (stage_hand); at is when it was raised and left out when lowered.
type StageHand struct {
	At        *time.Time `json:"at,omitempty" format:"date-time"`
	ChannelID uuid.UUID  `json:"channel_id" format:"uuid"`
	Raised    bool       `json:"raised"`
	UserID    uuid.UUID  `json:"user_id" format:"uuid"`
}

func (StageHand) EventName() string { return events.StageHand }

// StageTalkPower says whether a member may speak in a Stage now (stage_talk_power).
type StageTalkPower struct {
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Granted   bool      `json:"granted"`
	UserID    uuid.UUID `json:"user_id" format:"uuid"`
}

func (StageTalkPower) EventName() string { return events.StageTalkPower }
