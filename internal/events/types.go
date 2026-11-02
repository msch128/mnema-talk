package events

// The WebSocket event registry. Every event name on the wire is a constant
// here; handlers and the hub never spell one as a string literal. The names are
// public API (doc/api-stability.md): testdata/events.golden.json pins them, and
// web/src/types/events.ts must agree (npm run contracts:check). A constant is
// named after its value, "message_create" is MessageCreate; a client-to-server
// event carries the prefix Client.

// Server-to-client events.
const (
	MessageCreate       = "message_create"
	MessageUpdate       = "message_update"
	MessageDelete       = "message_delete"
	MessageReaction     = "message_reaction"
	PollUpdate          = "poll_update"
	PinsChanged         = "pins_changed"
	ModerationNotice    = "moderation_notice"
	ChannelsChanged     = "channels_changed"
	ReadState           = "read_state"
	UserUpdate          = "user_update"
	MemberJoined        = "member_joined"
	MemberTimeout       = "member_timeout"
	RolesChanged        = "roles_changed"
	MemberRolesChanged  = "member_roles_changed"
	PermissionsChanged  = "permissions_changed"
	SystemUpdate        = "system_update"
	InteractionResponse = "interaction_response"
	WebrtcMediaState    = "webrtc_media_state"
	PresenceUpdate      = "presence_update"
	UserStats           = "user_stats"
	VoiceStateUpdate    = "voice_state_update"
	ScreenViewers       = "screen_viewers"
	ScreenLayers        = "screen_layers"
	ScreenQuality       = "screen_quality"
	ScreenLayersWanted  = "screen_layers_wanted"
	CamerasCapped       = "cameras_capped"
	MediaHandshake      = "media_handshake"
	VoiceKicked         = "voice_kicked"
	ServerInfo          = "server_info"
	PresenceSnapshot    = "presence_snapshot"
	VoiceSnapshot       = "voice_snapshot"
	VoiceRooms          = "voice_rooms"
	Pong                = "pong"
	WebrtcOffer         = "webrtc_offer"
	WebrtcCandidate     = "webrtc_candidate"
	Typing              = "typing"
	VoiceSpeaking       = "voice_speaking"
	VoiceMuteState      = "voice_mute_state"
	EmojiChanged        = "emoji_changed"
	ThreadFollowUpdated = "thread_follow_updated"
	BlockUpdated        = "block_updated"
	SavedUpdated        = "saved_updated"
	ReminderUpdate      = "reminder_update"
	ScheduledUpdate     = "scheduled_message_update"
	DMCreated           = "dm_created"
	DMMemberAdded       = "dm_member_added"
	DMMemberRemoved     = "dm_member_removed"
	DMPrefsUpdated      = "dm_prefs_updated"
	// DMCallRing says a call started in a conversation ("Start a Talk"): its
	// members' sessions show an incoming call. DMCallEnd says it stopped
	// ringing (answered, cancelled, timed out) or is over.
	DMCallRing = "dm_call_ring"
	DMCallEnd  = "dm_call_end"
	// TalkLeader says who leads a temporary Talk now (leader_id is null when
	// nobody does). TalkMoved tells one connection that its join of a Create-a-
	// Talk lobby opened the Talk channel_id and seated it there.
	TalkLeader = "talk_leader"
	TalkMoved  = "talk_moved"
	// StageHand says a member raised or lowered their hand in a Stage;
	// StageTalkPower says whether a member may speak there now (explicitly
	// granted, or holding Manage stage). TalkReaction is a short-lived emoji
	// a member sent to the Talk they are in. All three go to that Talk's
	// members only and are never stored.
	StageHand      = "stage_hand"
	StageTalkPower = "stage_talk_power"
	TalkReaction   = "talk_reaction"
	// Kicked tells a member a moderator signed them out everywhere; their
	// connection closes right after.
	Kicked = "kicked"
	// ForumTagsChanged announces that tags were created, edited or removed
	// in a forum channel.
	ForumTagsChanged = "forum_tags_changed"
	// EventUpdate tells clients that a community event was created, edited,
	// cancelled, completed or that its RSVPs changed.
	EventUpdate = "event_update"
	// EventDelete tells one member that a community event left their view (it
	// moved into, or its Talk became, something they may not see): drop it.
	EventDelete = "event_delete"
	// EventReminder notifies members 15 minutes before an event starts.
	EventReminder = "event_reminder"
	// CommunityUpdated announces the community's name after the Owner changed
	// it, so open tabs retitle themselves without a reload.
	CommunityUpdated = "community_updated"
)

// Client-to-server events.
const (
	ClientPing                   = "ping"
	ClientPresenceIdle           = "presence_idle"
	ClientVoiceJoin              = "voice_join"
	ClientVoiceLeave             = "voice_leave"
	ClientTyping                 = "typing"
	ClientVoiceSpeaking          = "voice_speaking"
	ClientVoiceMuteState         = "voice_mute_state"
	ClientWebrtcAnswer           = "webrtc_answer"
	ClientWebrtcCandidate        = "webrtc_candidate"
	ClientWebrtcDiag             = "webrtc_diag"
	ClientWebrtcRequestKeyframe  = "webrtc_request_keyframe"
	ClientWebrtcSubscribe        = "webrtc_subscribe"
	ClientWebrtcLayers           = "webrtc_layers"
	ClientWebrtcScreenshareStart = "webrtc_screenshare_start"
	ClientWebrtcScreenshareStop  = "webrtc_screenshare_stop"
	ClientWebrtcCameraStop       = "webrtc_camera_stop"
	ClientMediaHandshake         = "media_handshake"
	ClientStageHand              = "stage_hand"
	ClientStageTalkPower         = "stage_talk_power"
	ClientTalkReaction           = "talk_reaction"
)
