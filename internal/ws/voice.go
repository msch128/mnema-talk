package ws

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
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
func (h *Hub) voiceRooms(c *Client, snap *perm.Snapshot) map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	started := make(map[uuid.UUID]time.Time, len(h.roomSince))
	for id, t := range h.roomSince {
		started[id] = t
	}
	return map[string]any{"started": visibleTo(c, snap, started), "now": time.Now()}
}

// voiceChannel returns chID if it exists and is a voice channel.
func (h *Hub) voiceChannel(ctx context.Context, chID uuid.UUID) (*chat.ChannelInfo, bool) {
	ch, err := chat.LoadChannel(ctx, h.DB, chID)
	if err != nil || ch.Type != chat.ChannelTypeVoice {
		return nil, false
	}
	return ch, true
}

// applyUserUpdate keeps the hub's copies of a user's public profile current
// after a name, status or avatar change (a user_update with a User payload).
func (h *Hub) applyUserUpdate(payload any) {
	u, ok := extractUserPayload(payload)
	if !ok {
		return
	}
	h.updateUserPublicProfile(u.ID, u.Public())
}

func extractUserPayload(payload any) (auth.User, bool) {
	switch v := payload.(type) {
	case auth.User:
		return v, v.ID != uuid.Nil
	case *auth.User:
		if v == nil || v.ID == uuid.Nil {
			return auth.User{}, false
		}
		return *v, true
	default:
		return auth.User{}, false
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
