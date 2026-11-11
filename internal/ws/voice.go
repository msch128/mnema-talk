package ws

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/pion/webrtc/v4"
)

// voiceKey is one user's stay in one voice room. Two tabs of a user can sit
// in different rooms; join time, mute state and grace are tracked per room.
type voiceKey struct {
	user uuid.UUID
	ch   uuid.UUID
}

// MuteState is whether a voice member muted their microphone or deafened.
type MuteState struct {
	Muted    bool `json:"muted"`
	Deafened bool `json:"deafened"`
}

// VoiceUser is a member of a voice room, since when they are in it and
// whether they are muted or deafened.
type VoiceUser struct {
	auth.User
	JoinedAt time.Time `json:"joined_at"`
	MuteState
	// TalkPower is whether the member may speak in a Stage; HandRaisedAt is
	// when they raised their hand there. Both stay empty outside a Stage.
	TalkPower    bool       `json:"talk_power,omitempty"`
	HandRaisedAt *time.Time `json:"hand_raised_at,omitempty"`
}

// voiceSnapshot copies the current voice rooms (channel → users).
func (h *Hub) voiceSnapshot() map[uuid.UUID]map[uuid.UUID]VoiceUser {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[uuid.UUID]map[uuid.UUID]VoiceUser, len(h.voice))
	for chID, users := range h.voice {
		cp := make(map[uuid.UUID]VoiceUser, len(users))
		for id, u := range users {
			k := voiceKey{id, chID}
			power, hand := h.stage.view(k)
			cp[id] = VoiceUser{User: u.Public(), JoinedAt: h.voiceSince[k], MuteState: h.voiceMute[k], TalkPower: power, HandRaisedAt: hand}
		}
		out[chID] = cp
	}
	return out
}

// voiceRooms says since when each voice room is occupied, plus the server's
// clock so clients can count up without trusting their own.
func (h *Hub) voiceRooms(c *Client, snap *perm.Snapshot) VoiceRooms {
	h.mu.RLock()
	defer h.mu.RUnlock()
	started := make(map[uuid.UUID]time.Time, len(h.roomSince))
	for id, t := range h.roomSince {
		started[id] = t
	}
	return VoiceRooms{Started: visibleTo(c, snap, started), Now: time.Now()}
}

// voiceChannel returns chID if it exists and is a voice channel.
func (h *Hub) voiceChannel(ctx context.Context, chID uuid.UUID) (*core.ChannelInfo, bool) {
	ch, err := core.LoadChannel(ctx, h.DB, chID)
	if err != nil || ch.Type != core.ChannelTypeVoice {
		return nil, false
	}
	return ch, true
}

// applyUserUpdate keeps the hub's copies of a user's public profile current
// after a name, status or avatar change (a user_update with a User payload).
func (h *Hub) applyUserUpdate(u auth.User) {
	if u.ID != uuid.Nil {
		h.updateUserPublicProfile(u.ID, u.Public())
	}
}

func (h *Hub) updateUserPublicProfile(id uuid.UUID, pub auth.User) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.profiles[id]; ok {
		h.profiles[id] = pub
	}
	for _, users := range h.voice {
		if _, ok := users[id]; ok {
			users[id] = pub
		}
	}
}

// profileLocked is c's user as currently known: the connection's snapshot
// updated by later user_update events. Callers hold h.mu.
func (h *Hub) profileLocked(c *Client) auth.User {
	if p, ok := h.profiles[c.User.ID]; ok {
		return p
	}
	return c.User.Public()
}

// leaveCurrentVoice is an explicit leave of whatever room c is in.
func (h *Hub) leaveCurrentVoice(c *Client) {
	c.voiceMu.Lock()
	defer c.voiceMu.Unlock()
	if cur := c.currentVoice(); cur != nil {
		h.leaveVoiceLocked(c, *cur)
	}
}

// leaveVoiceLocked is an explicit leave: presence ends immediately unless
// another connection of the same user is still in the room. Callers hold
// c.voiceMu.
func (h *Hub) leaveVoiceLocked(c *Client, chID uuid.UUID) {
	if h.SFU != nil {
		h.SFU.RemovePeer(chID, c.peer())
	}
	h.takeGrace(voiceKey{c.User.ID, chID})
	c.setVoice(nil)
	h.removePresenceUnlessPresent(c.User.ID, chID, c)
}

// moveRoomLocked keeps h.rooms in step when c's voice room changes from one
// to another (nil: none). Only registered connections are listed, like the
// scan of h.clients the index replaces. Callers hold h.mu.
func (h *Hub) moveRoomLocked(c *Client, from, to *uuid.UUID) {
	if _, registered := h.clients[c]; !registered {
		return
	}
	if from != nil {
		h.leaveRoomLocked(c, *from)
	}
	if to == nil {
		return
	}
	if h.rooms[*to] == nil {
		h.rooms[*to] = map[*Client]struct{}{}
	}
	h.rooms[*to][c] = struct{}{}
}

// leaveRoomLocked takes c out of chID's entry in h.rooms. Callers hold h.mu.
func (h *Hub) leaveRoomLocked(c *Client, chID uuid.UUID) {
	room := h.rooms[chID]
	delete(room, c)
	if len(room) == 0 {
		delete(h.rooms, chID)
	}
}

// The Talk events that carry a profile, the layers of a share or WebRTC
// signalling; the plain ones are in internal/events/wire. Fields that were map
// keys once stay in alphabetical order: the JSON must not change.

// VoiceJoin is the voice_state_update of a member joining a Talk; action is
// always "join". Leaving is wire.VoiceLeave.
type VoiceJoin struct {
	Action    string    `json:"action" enums:"join"`
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	StartedAt time.Time `json:"started_at" format:"date-time"`
	User      VoiceUser `json:"user"`
}

func (VoiceJoin) EventName() string { return events.VoiceStateUpdate }

// VoiceSnapshot is who sits in which Talk, by channel and user ID
// (voice_snapshot): part of the greeting of a new connection.
type VoiceSnapshot map[uuid.UUID]map[uuid.UUID]VoiceUser

func (VoiceSnapshot) EventName() string { return events.VoiceSnapshot }

// VoiceRooms says since when each Talk is occupied, and the server's clock
// (voice_rooms).
type VoiceRooms struct {
	Now     time.Time               `json:"now" format:"date-time"`
	Started map[uuid.UUID]time.Time `json:"started" format:"date-time"`
}

func (VoiceRooms) EventName() string { return events.VoiceRooms }

// ScreenLayers lists the layers of a screen share (screen_layers); layers is
// [] once the share ended, never null.
type ScreenLayers struct {
	ChannelID uuid.UUID   `json:"channel_id" format:"uuid"`
	Layers    []sfu.Layer `json:"layers"`
	UserID    uuid.UUID   `json:"user_id" format:"uuid"`
}

func (ScreenLayers) EventName() string { return events.ScreenLayers }

// WebrtcOffer is the server's session description for a connection's media
// peer (webrtc_offer); WebrtcCandidate one of its ICE candidates
// (webrtc_candidate).
type (
	WebrtcOffer     webrtc.SessionDescription
	WebrtcCandidate webrtc.ICECandidateInit
)

func (WebrtcOffer) EventName() string      { return events.WebrtcOffer }
func (*WebrtcCandidate) EventName() string { return events.WebrtcCandidate }
